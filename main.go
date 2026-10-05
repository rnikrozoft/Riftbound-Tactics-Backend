package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
)

const opAction int64 = 1
const opSnapshot int64 = 2
const opError int64 = 3

func InitModule(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, initializer runtime.Initializer) error {
	path := os.Getenv("RIFTBOUND_CHARACTERS_FILE")
	if path == "" {
		path = "/nakama/data/characters.json"
	}
	catalog, loadErr := game.LoadCharacterCatalog(path)
	if loadErr != nil {
		return loadErr
	}
	game.UseCatalog(catalog)
	logger.Info("Loaded %d character definitions from %s", len(catalog.Characters), path)
	if err := initializer.RegisterRpc("character_catalog", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		data, err := json.Marshal(game.Catalog())
		return string(data), err
	}); err != nil {
		return err
	}

	_, err := db.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS rift_rooms (
 code TEXT PRIMARY KEY, match_id TEXT NOT NULL DEFAULT '', creator TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '30 minutes'
 );
 CREATE TABLE IF NOT EXISTS rift_battles (
 match_id TEXT NOT NULL, round INTEGER NOT NULL, code TEXT NOT NULL,
 result JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(match_id,round)
 );
 CREATE TABLE IF NOT EXISTS rift_match_wins (
 match_id TEXT PRIMARY KEY, winner TEXT NOT NULL, synced BOOLEAN NOT NULL DEFAULT false,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
 );
 CREATE INDEX IF NOT EXISTS rift_match_wins_owner ON rift_match_wins(winner);`)
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, mmrSchema); err != nil {
		return err
	}
	if err = initializer.RegisterRpc("mmr_self", selfMMR); err != nil {
		return err
	}
	if err = initializer.RegisterMatch("riftbound_room", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) (runtime.Match, error) {
		return &roomMatch{}, nil
	}); err != nil {
		return err
	}
	if err = initializer.RegisterRpc("room_create", createRoom); err != nil {
		return err
	}
	if err = initializer.RegisterRpc("room_join", joinRoom); err != nil {
		return err
	}
	if err = initializer.RegisterRpc("server_clock", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		if _, err := userID(ctx); err != nil {
			return "", err
		}
		out, _ := json.Marshal(map[string]int64{"server_ms": time.Now().UnixMilli()})
		return string(out), nil
	}); err != nil {
		return err
	}
	if err = nk.LeaderboardCreate(ctx, winLeaderboard, true, "desc", "best", "", map[string]interface{}{"points_per_win": 1, "name": "All-time match wins"}, true); err != nil {
		return err
	}
	if err = nk.LeaderboardCreate(ctx, mmrLeaderboard, true, "desc", "set", "", map[string]interface{}{"name": "Skill MMR"}, true); err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			reconcileWins(context.Background(), logger, db, nk)
			reconcileMMR(context.Background(), logger, db, nk)
			<-ticker.C
		}
	}()
	if err := registerMatchmaking(initializer); err != nil {
		return err
	}
	logger.Info("Riftbound Tactics authoritative Go runtime loaded")
	return nil
}
func userID(ctx context.Context) (string, error) {
	id, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok || id == "" {
		return "", runtime.NewError("Authentication required", 16)
	}
	return id, nil
}
func roomResponse(code, id string) (string, error) {
	out, _ := json.Marshal(map[string]string{"code": code, "match_id": id})
	return string(out), nil
}
func createRoom(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	var request struct {
		Deck *game.Deck `json:"deck"`
	}
	if payload != "" && json.Unmarshal([]byte(payload), &request) != nil {
		return "", runtime.NewError("Invalid deck request", 3)
	}
	deck := game.DefaultDeck()
	if request.Deck != nil {
		deck = *request.Deck
	}
	if err := game.ValidateDeck(deck); err != nil {
		return "", runtime.NewError(err.Error(), 3)
	}
	deckBytes, _ := json.Marshal(deck)
	if _, err = db.ExecContext(ctx, "DELETE FROM rift_rooms WHERE expires_at<now()"); err != nil {
		return "", runtime.NewError("Room storage unavailable", 14)
	}
	for attempt := 0; attempt < 20; attempt++ {
		number, err := rand.Int(rand.Reader, big.NewInt(900000))
		if err != nil {
			return "", err
		}
		code := strconv.FormatInt(100000+number.Int64(), 10)
		result, err := db.ExecContext(ctx, "INSERT INTO rift_rooms(code,creator) VALUES($1,$2) ON CONFLICT DO NOTHING", code, id)
		if err != nil {
			return "", runtime.NewError("Room storage unavailable", 14)
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			continue
		}
		matchID, err := nk.MatchCreate(ctx, "riftbound_room", map[string]interface{}{"code": code, "creator": id, "deck": string(deckBytes)})
		if err != nil {
			db.ExecContext(ctx, "DELETE FROM rift_rooms WHERE code=$1", code)
			logger.Error("MatchCreate: %v", err)
			return "", runtime.NewError("Unable to create room", 14)
		}
		if _, err = db.ExecContext(ctx, "UPDATE rift_rooms SET match_id=$2 WHERE code=$1", code, matchID); err != nil {
			return "", runtime.NewError("Room storage unavailable", 14)
		}
		return roomResponse(code, matchID)
	}
	return "", runtime.NewError("Unable to allocate room code", 14)
}
func joinRoom(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	if _, err := userID(ctx); err != nil {
		return "", err
	}
	var request struct {
		Code string `json:"code"`
	}
	if json.Unmarshal([]byte(payload), &request) != nil {
		return "", runtime.NewError("Invalid request", 3)
	}
	code := strings.TrimSpace(request.Code)
	if len(code) != 6 {
		return "", runtime.NewError("Enter a 6-digit room code", 3)
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return "", runtime.NewError("Enter a 6-digit room code", 3)
		}
	}
	var matchID string
	if err := db.QueryRowContext(ctx, "SELECT match_id FROM rift_rooms WHERE code=$1 AND expires_at>now()", code).Scan(&matchID); err != nil || matchID == "" {
		return "", runtime.NewError("Room not found", 5)
	}
	match, err := nk.MatchGet(ctx, matchID)
	if err != nil || match == nil {
		return "", runtime.NewError("Room closed", 5)
	}
	return roomResponse(code, matchID)
}

type roomMatch struct{}
type matchState struct {
	room           *game.Room
	presences      map[string]runtime.Presence
	sequences      map[string]int64
	emptySince     int64
	createdMS      int64
	persistedRound int
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
	label, _ := json.Marshal(map[string]string{"game": "riftbound", "code": code})
	return state, 10, string(label)
}
func (m *roomMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	s := state.(*matchState)
	if existing := s.presences[presence.GetUserId()]; existing != nil && existing.GetSessionId() != presence.GetSessionId() {
		return s, false, "User already connected"
	}
	deck := game.DefaultDeck()
	if raw := metadata["deck"]; raw != "" {
		if json.Unmarshal([]byte(raw), &deck) != nil {
			return s, false, "Invalid deck"
		}
	}
	if err := game.ValidateDeck(deck); err != nil {
		return s, false, err.Error()
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
		s.presences[p.GetUserId()] = p
		s.room.Connect(p.GetUserId(), now)
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
	if s.room.State.Phase == "battle" && s.persistedRound != s.room.State.Round {
		plan := s.room.State.Battle
		payload, _ := json.Marshal(plan)
		matchID, _ := ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string)
		if _, err := db.ExecContext(ctx, "INSERT INTO rift_battles(match_id,round,code,result) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT DO NOTHING", matchID, plan.Round, s.room.State.Code, string(payload)); err != nil {
			logger.Error("Unable to persist battle: %v", err)
		} else {
			s.persistedRound = plan.Round
		}
	}
	s.room.Finish(now)
	s.room.AutoAdvance(now)
	if s.room.State.Phase == "game_over" && !s.scoreRecorded && now >= s.nextScoreRetry {
		s.nextScoreRetry = now + 3000
		matchID, _ := ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string)
		winner := matchWinnerID(s.room.State)
		if winner != "" {
			if _, err := db.ExecContext(ctx, "INSERT INTO rift_match_wins(match_id,winner) VALUES($1,$2) ON CONFLICT DO NOTHING", matchID, winner); err != nil {
				logger.Error("Unable to record match win: %v", err)
			} else {
				if err := syncWinner(ctx, db, nk, winner); err != nil {
					logger.Error("Unable to sync leaderboard (will retry): %v", err)
				} else {
					s.scoreRecorded = true
				}
			}
		}
	}
	if s.room.State.Revision != revision || now-s.lastBroadcast >= 1000 {
		broadcast(s, dispatcher, now)
	}
	if (s.emptySince > 0 && now-s.emptySince > 60000) || (s.room.State.Phase == "waiting" && now-s.createdMS > 600000) {
		db.ExecContext(ctx, "DELETE FROM rift_rooms WHERE code=$1", s.room.State.Code)
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
	db.ExecContext(ctx, "DELETE FROM rift_rooms WHERE code=$1", s.room.State.Code)
	for _, p := range s.presences {
		sendError(dispatcher, p, "Server shutting down", 0)
	}
	return nil
}
func (m *roomMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
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
