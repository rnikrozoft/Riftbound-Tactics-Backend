package server

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

func loadMMR(ctx context.Context, nk runtime.NakamaModule, id string) (MMRAccount, error) {
	account, _, err := readRating(ctx, nk, id)
	return account.MMRAccount, err
}
func selfMMR(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	a, err := loadMMR(ctx, nk, id)
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

// Workers receive values only. The match goroutine alone owns results and revision.
func settleMMR(logger runtime.Logger, nk runtime.NakamaModule, s *leagueState, matchID string, now int64) {
	if s.mmrResults == nil {
		s.mmrResults = map[string]MMRResult{}
	}
	if s.mmrSaves == nil {
		s.mmrSaves = map[string]*asyncTask[MMRResult]{}
	}
	for id, task := range s.mmrSaves {
		if result, err, done := task.poll(); done {
			delete(s.mmrSaves, id)
			if err != nil {
				logger.Error("MMR settlement: %v", err)
				s.nextMMRRetry = now + 1000
			} else {
				s.mmrResults[id] = result
				s.league.Revision++
			}
		}
	}
	if now < s.nextMMRRetry {
		return
	}
	for i, p := range s.league.Players {
		if s.league.Seats[i].Bot || s.league.Place[i] == 0 {
			continue
		}
		if _, done := s.mmrResults[p.UserID]; done {
			continue
		}
		if s.mmrSaves[p.UserID] != nil {
			continue
		}
		delta, rated, err := placementMMR(s.league, i)
		if err != nil {
			logger.Error("MMR settlement: %v", err)
			continue
		}
		id, place := p.UserID, s.league.Place[i]
		s.mmrSaves[id] = startTask(func(ctx context.Context) (MMRResult, error) {
			result, err := persistMMR(ctx, nk, matchID, id, place, delta, rated)
			if err == nil {
				if syncErr := syncMMR(ctx, nk, id); syncErr != nil {
					logger.Error("MMR leaderboard (will reconcile): %v", syncErr)
				}
			}
			return result, err
		})
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
