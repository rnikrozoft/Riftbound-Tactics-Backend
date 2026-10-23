package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"time"
)

type roomMatch struct{}
type matchState struct {
	room           *game.Room
	presences      map[string]runtime.Presence
	sequences      map[string]int64
	emptySince     int64
	createdMS      int64
	persistedRound int
	battleSave     *asyncTask[int]
	winSave        *asyncTask[struct{}]
	lastBroadcast  int64
	scoreRecorded  bool
	nextScoreRetry int64
}

func (m *roomMatch) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	var seed [8]byte
	if _, err := rand.Read(seed[:]); err != nil {
		logger.Error("Seed generation failed: %v", err)
		return nil, 10, ""
	}
	code, _ := params["code"].(string)
	creator, _ := params["creator"].(string)
	now := time.Now().UnixMilli()
	state := &matchState{room: game.New(code, creator, int64(binary.LittleEndian.Uint64(seed[:]))), presences: map[string]runtime.Presence{}, sequences: map[string]int64{}, createdMS: now, emptySince: now}
	deck := game.DefaultDeck()
	if raw, ok := params["deck"].(string); ok {
		if json.Unmarshal([]byte(raw), &deck) != nil {
			return nil, 10, ""
		}
	}
	if err := state.room.SetDeck(creator, deck); err != nil {
		return nil, 10, ""
	}
	if err := rememberActiveMatches(ctx, nk, []string{creator}, code); err != nil {
		return nil, 10, ""
	}
	label, _ := json.Marshal(map[string]string{"game": "riftbound", "code": code})
	return state, 10, string(label)
}
func (m *roomMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	s := state.(*matchState)
	deck := game.DefaultDeck()
	if raw := metadata["deck"]; raw != "" {
		if json.Unmarshal([]byte(raw), &deck) != nil {
			return s, false, "Invalid deck"
		}
	}
	if err := func() error {
		if s.room.Player(presence.GetUserId()) != nil {
			return nil
		}
		return validateOwnedDeck(ctx, nk, presence.GetUserId(), deck)
	}(); err != nil {
		return s, false, err.Error()
	}
	if existing := s.presences[presence.GetUserId()]; existing != nil && existing.GetSessionId() != presence.GetSessionId() {
		if err := dispatcher.MatchKick([]runtime.Presence{existing}); err != nil {
			return s, false, "Retry reconnect"
		}
		delete(s.presences, presence.GetUserId())
	}
	existingPlayer := s.room.Player(presence.GetUserId()) != nil
	if !s.room.Reserve(presence.GetUserId()) {
		return s, false, "Room is full (2 players)"
	}
	if !existingPlayer {
		if err := s.room.SetDeck(presence.GetUserId(), deck); err != nil {
			return s, false, err.Error()
		}
	}
	return s, true, ""
}
func (m *roomMatch) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	s := state.(*matchState)
	now := time.Now().UnixMilli()
	for _, p := range presences {
		previous := s.presences[p.GetUserId()]
		if s.sequences == nil {
			s.sequences = map[string]int64{}
		}
		if previous == nil || previous.GetSessionId() != p.GetSessionId() {
			s.sequences[p.GetUserId()] = 0
		}
		s.presences[p.GetUserId()] = p
		s.room.Connect(p.GetUserId(), now)
		if err := rememberActiveMatches(ctx, nk, []string{p.GetUserId()}, s.room.State.Code); err != nil && logger != nil {
			logger.Error("Active match discovery: %v", err)
		}
	}
	s.emptySince = 0
	broadcast(s, dispatcher, now)
	return s
}
func (m *roomMatch) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	s := state.(*matchState)
	now := time.Now().UnixMilli()
	for _, p := range presences {
		if current := s.presences[p.GetUserId()]; current != nil && current.GetSessionId() == p.GetSessionId() {
			delete(s.presences, p.GetUserId())
			s.room.Disconnect(p.GetUserId())
		}
	}
	if len(s.presences) == 0 {
		s.emptySince = now
	}
	broadcast(s, dispatcher, now)
	return s
}
func (m *roomMatch) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, messages []runtime.MatchData) interface{} {
	s := state.(*matchState)
	now := time.Now().UnixMilli()
	revision := s.room.State.Revision
	// Deadline is applied before messages, so late ready/edit packets cannot extend preparation.
	if s.room.ShouldStart(now) {
		s.room.Start(now)
	}
	for _, message := range messages {
		if message.GetOpCode() != opAction || len(message.GetData()) > 2048 {
			continue
		}
		id := message.GetUserId()
		presence := s.presences[id]
		if presence == nil || presence.GetSessionId() != message.GetSessionId() {
			continue
		}
		var action game.Action
		if json.Unmarshal(message.GetData(), &action) != nil {
			sendError(dispatcher, presence, "Invalid action", 0)
			continue
		}
		if action.Sequence <= s.sequences[id] {
			sendError(dispatcher, presence, "Duplicate/stale action", action.Sequence)
			continue
		}
		s.sequences[id] = action.Sequence
		if err := s.room.Apply(id, action, now); err != nil {
			sendError(dispatcher, presence, err.Error(), action.Sequence)
		}
	}
	if s.room.ShouldStart(now) {
		s.room.Start(now)
	}
	if s.room.State.Phase == "battle" && s.persistedRound != s.room.State.Round && s.battleSave == nil {
		plan := s.room.State.Battle
		payload, _ := json.Marshal(plan)
		matchID, _ := ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string)
		if s.battleSave == nil {
			code, round := s.room.State.Code, plan.Round
			s.battleSave = startTask(func(ctx context.Context) (int, error) {
				return round, persistBattle(ctx, nk, matchID, round, code, payload)
			})
		}
	}
	if s.battleSave != nil {
		if round, err, done := s.battleSave.poll(); done {
			s.battleSave = nil
			if err != nil {
				logger.Error("Unable to persist battle: %v", err)
			} else {
				s.persistedRound = round
			}
		}
	}
	s.room.Finish(now)
	s.room.AutoAdvance(now)
	if s.room.State.Phase == "game_over" && !s.scoreRecorded && now >= s.nextScoreRetry {
		s.nextScoreRetry = now + 3000
		matchID, _ := ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string)
		winner := matchWinnerID(s.room.State)
		if winner != "" && s.winSave == nil {
			s.winSave = startTask(func(ctx context.Context) (struct{}, error) {
				if err := recordWin(ctx, nk, matchID, winner); err != nil {
					return struct{}{}, err
				}
				return struct{}{}, syncWinner(ctx, nk, winner)
			})
		}
	}
	if s.winSave != nil {
		if _, err, done := s.winSave.poll(); done {
			s.winSave = nil
			if err != nil {
				logger.Error("Unable to sync leaderboard (will retry): %v", err)
			} else {
				s.scoreRecorded = true
			}
		}
	}
	if s.room.State.Revision != revision || now-s.lastBroadcast >= 1000 {
		broadcast(s, dispatcher, now)
	}
	// A disconnected client may reopen while a started match is still running.
	if (s.room.State.Phase == "game_over" && s.emptySince > 0 && now-s.emptySince > 60000) || (s.room.State.Phase == "waiting" && now-s.createdMS > 600000) {
		deleteRoomDirectory(ctx, nk, s.room.State.Code)
		return nil
	}
	return s
}
func broadcast(s *matchState, dispatcher runtime.MatchDispatcher, now int64) {
	s.room.State.ServerMS = now
	s.lastBroadcast = now
	for id, presence := range s.presences {
		view := recipientState(s.room.State, id)
		bytes, _ := json.Marshal(struct {
			game.State
			AckSequence int64 `json:"ack_sequence"`
		}{view, s.sequences[id]})
		dispatcher.BroadcastMessage(opSnapshot, bytes, []runtime.Presence{presence}, nil, true)
	}
}
func sendError(dispatcher runtime.MatchDispatcher, presence runtime.Presence, message string, sequence int64) {
	bytes, _ := json.Marshal(map[string]interface{}{"message": message, "sequence": sequence})
	dispatcher.BroadcastMessage(opError, bytes, []runtime.Presence{presence}, nil, true)
}
func (m *roomMatch) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, graceSeconds int) interface{} {
	s := state.(*matchState)
	deleteRoomDirectory(ctx, nk, s.room.State.Code)
	for _, p := range s.presences {
		sendError(dispatcher, p, "Server shutting down", 0)
	}
	return nil
}
func (m *roomMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
	if id, ok := resumeUser(data); ok {
		room := state.(*matchState).room
		p := room.Player(id)
		if p != nil && p.HP > 0 && room.State.Phase != "game_over" {
			return state, "available"
		}
		return state, "unavailable"
	}
	return state, fmt.Sprintf("riftbound:%s", state.(*matchState).room.State.Code)
}

// Project a copy so private formations never enter an opponent's preparation snapshot.
func recipientState(state game.State, id string) game.State {
	view := state
	for i, p := range view.Players {
		if p != nil && p.UserID != id {
			other := *p
			other.HandCount = 0
			if view.Phase == "battle" || view.Phase == "finished" || view.Phase == "game_over" {
				other.HandCount = len(p.Hand)
			}
			other.Hand = []game.Card{}
			other.Offers = []game.Card{}
			if view.Phase == "preparation" || view.Phase == "waiting" {
				other.Units = []game.Unit{}
			}
			view.Players[i] = &other
		}
	}
	return view
}
