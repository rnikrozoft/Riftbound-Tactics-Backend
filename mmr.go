package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"math"
)

const initialMMR = 1000
const mmrK = 32.0
const mmrSchema = `
CREATE TABLE IF NOT EXISTS rift_mmr (
 user_id TEXT PRIMARY KEY, rating INTEGER NOT NULL DEFAULT 1000 CHECK(rating>=0),
 rated_games INTEGER NOT NULL DEFAULT 0, updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS rift_mmr_results (
 match_id TEXT NOT NULL, user_id TEXT NOT NULL, placement INTEGER NOT NULL,
 rating_before INTEGER NOT NULL, rating_after INTEGER NOT NULL, delta INTEGER NOT NULL,
 rated BOOLEAN NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(match_id,user_id)
 );
ALTER TABLE rift_mmr ADD COLUMN IF NOT EXISTS leaderboard_synced BOOLEAN NOT NULL DEFAULT false;`

type MMRAccount struct {
	Rating int `json:"mmr"`
	Games  int `json:"rated_games"`
}
type MMRResult struct {
	Before int  `json:"before"`
	After  int  `json:"after"`
	Delta  int  `json:"delta"`
	Rated  bool `json:"rated"`
}

func loadMMR(ctx context.Context, db *sql.DB, id string) (MMRAccount, error) {
	a := MMRAccount{Rating: initialMMR}
	err := db.QueryRowContext(ctx, "SELECT rating,rated_games FROM rift_mmr WHERE user_id=$1", id).Scan(&a.Rating, &a.Games)
	if err == sql.ErrNoRows {
		return MMRAccount{Rating: initialMMR}, nil
	}
	return a, err
}
func selfMMR(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	a, err := loadMMR(ctx, db, id)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(a)
	return string(raw), nil
}

// Placement Elo compares each human to every other human once. Surviving players
// necessarily outrank an eliminated player, so their later exact place is not needed.
// Pre-match ratings remain fixed even as earlier eliminations are persisted.
func placementMMR(l *game.League, index int) (int, bool, error) {
	if index < 0 || index >= game.LeagueSeats || l.Seats[index].Bot || l.Place[index] == 0 {
		return 0, false, fmt.Errorf("placement not resolved")
	}
	total := 0.0
	opponents := 0
	for j, s := range l.Seats {
		if j == index || s.Bot {
			continue
		}
		opponents++
		actual := 0.0
		if l.Place[j] > 0 {
			if l.Place[index] < l.Place[j] {
				actual = 1
			} else if l.Place[index] == l.Place[j] {
				actual = 0.5
			}
		}
		expected := 1 / (1 + math.Pow(10, float64(s.Rating-l.Seats[index].Rating)/400))
		total += actual - expected
	}
	if opponents == 0 {
		return 0, false, nil
	}
	return int(math.Round(mmrK * total / float64(opponents))), true, nil
}

// The account row lock serializes concurrent games; the per-match result ledger
// makes retries safe. Updating rating, game count and the ledger is one transaction.
func persistMMR(ctx context.Context, db *sql.DB, matchID, id string, place, delta int, rated bool) (MMRResult, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return MMRResult{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO rift_mmr(user_id) VALUES($1) ON CONFLICT DO NOTHING", id); err != nil {
		return MMRResult{}, err
	}
	var before int
	if err = tx.QueryRowContext(ctx, "SELECT rating FROM rift_mmr WHERE user_id=$1 FOR UPDATE", id).Scan(&before); err != nil {
		return MMRResult{}, err
	}
	var old MMRResult
	err = tx.QueryRowContext(ctx, "SELECT rating_before,rating_after,delta,rated FROM rift_mmr_results WHERE match_id=$1 AND user_id=$2", matchID, id).Scan(&old.Before, &old.After, &old.Delta, &old.Rated)
	if err == nil {
		return old, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return MMRResult{}, err
	}
	after := before + delta
	if after < 0 {
		after = 0
	}
	out := MMRResult{before, after, after - before, rated}
	games := 0
	if rated {
		games = 1
	}
	if _, err = tx.ExecContext(ctx, "UPDATE rift_mmr SET rating=$2,rated_games=rated_games+$3,updated_at=now(),leaderboard_synced=false WHERE user_id=$1", id, after, games); err != nil {
		return MMRResult{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO rift_mmr_results(match_id,user_id,placement,rating_before,rating_after,delta,rated) VALUES($1,$2,$3,$4,$5,$6,$7)", matchID, id, place, out.Before, out.After, out.Delta, rated); err != nil {
		return MMRResult{}, err
	}
	return out, tx.Commit()
}
func settleMMR(ctx context.Context, db *sql.DB, logger runtime.Logger, nk runtime.NakamaModule, s *leagueState, matchID string, now int64) {
	if s.mmrResults == nil {
		s.mmrResults = map[string]MMRResult{}
	}
	if now < s.nextMMRRetry {
		return
	}
	failed := false
	for i, p := range s.league.Players {
		if s.league.Seats[i].Bot || s.league.Place[i] == 0 {
			continue
		}
		if _, done := s.mmrResults[p.UserID]; done {
			continue
		}
		delta, rated, err := placementMMR(s.league, i)
		if err == nil {
			var result MMRResult
			result, err = persistMMR(ctx, db, matchID, p.UserID, s.league.Place[i], delta, rated)
			if err == nil {
				if syncErr := syncMMR(ctx, db, nk, p.UserID); syncErr != nil {
					logger.Error("MMR leaderboard: %v", syncErr)
				}
				s.mmrResults[p.UserID] = result
				s.league.Revision++
			}
		}
		if err != nil {
			failed = true
			logger.Error("MMR settlement: %v", err)
		}
	}
	if failed {
		s.nextMMRRetry = now + 1000
	}
}
func mmrPending(s *leagueState) bool {
	for i, p := range s.league.Players {
		if !s.league.Seats[i].Bot && s.league.Place[i] > 0 {
			if _, ok := s.mmrResults[p.UserID]; !ok {
				return true
			}
		}
	}
	return false
}
