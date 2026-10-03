package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"time"
)

const winLeaderboard = "riftbound_match_wins"

func matchWinnerID(state game.State) string {
	if state.Phase != "game_over" {
		return ""
	}
	for _, p := range state.Players {
		if p != nil && p.Team == state.Winner && p.HP > 0 {
			return p.UserID
		}
	}
	return ""
}

// The durable match ledger deduplicates rewards. Absolute BEST scores make retries
// and concurrent matches safe: a delayed lower total never overwrites a newer total.
func syncWinner(ctx context.Context, db *sql.DB, nk runtime.NakamaModule, winner string) error {
	rows, err := db.QueryContext(ctx, "SELECT match_id FROM rift_match_wins WHERE winner=$1 AND synced=false", winner)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	var wins int64
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM rift_match_wins WHERE winner=$1", winner).Scan(&wins); err != nil {
		return err
	}
	users, err := nk.UsersGetId(ctx, []string{winner}, nil)
	if err != nil {
		return err
	}
	if len(users) != 1 {
		return fmt.Errorf("winning account missing")
	}
	name := users[0].DisplayName
	if name == "" {
		name = users[0].Username
	}
	if _, err = nk.LeaderboardRecordWrite(ctx, winLeaderboard, winner, name, wins, 0, map[string]interface{}{"wins": wins}, nil); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = db.ExecContext(ctx, "UPDATE rift_match_wins SET synced=true WHERE match_id=$1", id); err != nil {
			return err
		}
	}
	return nil
}
func reconcileWins(parent context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT DISTINCT winner FROM rift_match_wins WHERE synced=false LIMIT 100")
	if err != nil {
		logger.Error("Leaderboard reconciliation: %v", err)
		return
	}
	owners := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		owners = append(owners, id)
	}
	rows.Close()
	for _, id := range owners {
		if err = syncWinner(ctx, db, nk, id); err != nil {
			logger.Error("Leaderboard retry: %v", err)
		}
	}
}
