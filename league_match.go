package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"time"
)

type leagueMatch struct{}
type leagueState struct {
	league                                         *game.League
	presences                                      map[string]runtime.Presence
	sequences                                      map[string]int64
	created, lastBroadcast, emptySince, nextReward int64
	rewarded                                       bool
	persisted                                      int
	mmrResults                                     map[string]MMRResult
	nextMMRRetry                                   int64
}

func (m *leagueMatch) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	raw, _ := params["seats"].(string)
	var seats []game.Seat
	if json.Unmarshal([]byte(raw), &seats) != nil {
		return nil, 10, ""
	}
	now := time.Now().UnixMilli()
	l, err := game.NewLeague(seats, now)
	if err != nil {
		return nil, 10, ""
	}
	return &leagueState{league: l, presences: map[string]runtime.Presence{}, sequences: map[string]int64{}, created: now}, 10, `{"game":"riftbound","seats":6}`
}
func (m *leagueMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, p runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	s := state.(*leagueState)
	i := s.league.Index(p.GetUserId())
	if i < 0 || s.league.Seats[i].Bot {
		return s, false, "Not reserved for this match"
	}
	if s.league.Players[i].HP <= 0 {
		return s, false, "Player eliminated; this match cannot be rejoined"
	}
	if old := s.presences[p.GetUserId()]; old != nil && old.GetSessionId() != p.GetSessionId() {
		if err := d.MatchKick([]runtime.Presence{old}); err != nil {
			return s, false, "Retry reconnect"
		}
		delete(s.presences, p.GetUserId())
	}
	return s, true, ""
}
func (m *leagueMatch) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, ps []runtime.Presence) interface{} {
	s := state.(*leagueState)
	for _, p := range ps {
		previous := s.presences[p.GetUserId()]
		if s.sequences == nil {
			s.sequences = map[string]int64{}
		}
		if previous == nil || previous.GetSessionId() != p.GetSessionId() {
			s.sequences[p.GetUserId()] = 0
		}
		s.presences[p.GetUserId()] = p
		s.league.Connected(p.GetUserId(), true)
	}
	s.emptySince = 0
	return s
}
func (m *leagueMatch) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, ps []runtime.Presence) interface{} {
	s := state.(*leagueState)
	for _, p := range ps {
		if old := s.presences[p.GetUserId()]; old != nil && old.GetSessionId() == p.GetSessionId() {
			delete(s.presences, p.GetUserId())
			s.league.Connected(p.GetUserId(), false)
		}
	}
	if len(s.presences) == 0 {
		s.emptySince = time.Now().UnixMilli()
	}
	return s
}
func (m *leagueMatch) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, messages []runtime.MatchData) interface{} {
	s := state.(*leagueState)
	l := s.league
	now := time.Now().UnixMilli()
	revision := l.Revision
	if l.Phase == "waiting" {
		connected := true
		for i, p := range l.Players {
			if !l.Seats[i].Bot && !p.Connected {
				connected = false
			}
		}
		if connected || now-s.created >= 5000 {
			l.Prepare(now)
		}
	}
	l.Tick(now)
	for _, msg := range messages {
		p := s.presences[msg.GetUserId()]
		if p == nil || p.GetSessionId() != msg.GetSessionId() || msg.GetOpCode() != opAction || len(msg.GetData()) > 2048 {
			continue
		}
		var a game.Action
		if json.Unmarshal(msg.GetData(), &a) != nil {
			sendError(d, p, "Invalid action", 0)
			continue
		}
		if a.Sequence <= s.sequences[p.GetUserId()] {
			sendError(d, p, "Duplicate/stale action", a.Sequence)
			continue
		}
		s.sequences[p.GetUserId()] = a.Sequence
		if err := l.Apply(p.GetUserId(), a, now); err != nil {
			sendError(d, p, err.Error(), a.Sequence)
		}
	}
	l.PlayBots(now)
	l.Tick(now)
	matchID, _ := ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string)
	settleMMR(ctx, db, logger, nk, s, matchID, now)
	if l.Phase == "battle" && s.persisted != l.Round {
		plans := []*game.Plan{}
		for _, r := range l.Duels {
			plans = append(plans, r.State.Battle)
		}
		raw, _ := json.Marshal(plans)
		if _, err := db.ExecContext(ctx, "INSERT INTO rift_battles(match_id,round,code,result) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT DO NOTHING", matchID, l.Round, "MATCHMAKING", string(raw)); err != nil {
			logger.Error("Persist league: %v", err)
		} else {
			s.persisted = l.Round
		}
	}
	if l.Phase == "game_over" && !s.rewarded && now >= s.nextReward {
		s.nextReward = now + 3000
		i := l.Index(l.Winner)
		if i < 0 || l.Seats[i].Bot {
			s.rewarded = true
		} else if _, err := db.ExecContext(ctx, "INSERT INTO rift_match_wins(match_id,winner) VALUES($1,$2) ON CONFLICT DO NOTHING", matchID, l.Winner); err != nil {
			logger.Error("League reward: %v", err)
		} else if err := syncWinner(ctx, db, nk, l.Winner); err != nil {
			logger.Error("League leaderboard: %v", err)
		} else {
			s.rewarded = true
		}
	}
	if l.Phase != "waiting" && (l.Revision != revision || now-s.lastBroadcast >= 1000) {
		s.lastBroadcast = now
		for id, p := range s.presences {
			view := recipientState(l.View(id, now), id)
			roster := l.Roster()
			for i, p := range roster {
				if result, ok := s.mmrResults[p.UserID]; ok {
					roster[i].Rating = result.After
				}
			}
			delta := 0
			pending := false
			if result, ok := s.mmrResults[id]; ok {
				delta = result.Delta
			} else {
				index := l.Index(id)
				pending = index >= 0 && l.Place[index] > 0
			}
			raw, _ := json.Marshal(struct {
				game.State
				Roster     []game.Standing `json:"roster"`
				WinnerID   string          `json:"winner_id"`
				Ack        int64           `json:"ack_sequence"`
				MMRDelta   int             `json:"mmr_delta"`
				MMRPending bool            `json:"mmr_pending"`
			}{view, roster, l.Winner, s.sequences[id], delta, pending})
			d.BroadcastMessage(opSnapshot, raw, []runtime.Presence{p}, nil, true)
		}
	}
	// Send the final HP/placement snapshot before removing the losing connection.
	kickEliminated(s, d, logger, now)
	if !mmrPending(s) && (s.emptySince > 0 && now-s.emptySince > 60000 || len(s.presences) == 0 && now-s.created > 60000) {
		return nil
	}
	return s
}
func (m *leagueMatch) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, grace int) interface{} {
	s := state.(*leagueState)
	for _, p := range s.presences {
		sendError(d, p, "Server shutting down", 0)
	}
	return nil
}
func (m *leagueMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
	return state, "riftbound-six"
}

// Remove only the match presence. The authenticated socket/account remains usable
// for lobby APIs; eliminated formations stay in League for ghost opponents.
func kickEliminated(s *leagueState, d runtime.MatchDispatcher, logger runtime.Logger, now int64) {
	for id, p := range s.presences {
		i := s.league.Index(id)
		if i < 0 || s.league.Players[i].HP > 0 {
			continue
		}
		if err := d.MatchKick([]runtime.Presence{p}); err != nil {
			if logger != nil {
				logger.Error("Elimination kick: %v", err)
			}
			continue
		}
		delete(s.presences, id)
		s.league.Connected(id, false)
	}
	if len(s.presences) == 0 && s.emptySince == 0 {
		s.emptySince = now
	}
}
