package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"sync"
	"time"
)

const mmrLeaderboard = "riftbound_mmr"

var mmrSyncLock sync.Mutex

// SET permits rating decreases. Always read the current durable rating rather
// than an old match result; dirty rows retain failed or superseded writes.
func syncMMR(ctx context.Context, db *sql.DB, nk runtime.NakamaModule, id string) error {
	mmrSyncLock.Lock()
	defer mmrSyncLock.Unlock()
	var rating, games int
	if err := db.QueryRowContext(ctx, "SELECT rating,rated_games FROM rift_mmr WHERE user_id=$1", id).Scan(&rating, &games); err != nil {
		return err
	}
	if games == 0 {
		return nil
	}
	users, err := nk.UsersGetId(ctx, []string{id}, nil)
	if err != nil {
		return err
	}
	if len(users) != 1 {
		return fmt.Errorf("MMR account missing")
	}
	name := users[0].DisplayName
	if name == "" {
		name = users[0].Username
	}
	if _, err = nk.LeaderboardRecordWrite(ctx, mmrLeaderboard, id, name, int64(rating), 0, map[string]interface{}{"rated_games": games}, nil); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "UPDATE rift_mmr SET leaderboard_synced=true WHERE user_id=$1 AND rating=$2 AND rated_games=$3", id, rating, games)
	return err
}

func reconcileMMR(parent context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT user_id FROM rift_mmr WHERE leaderboard_synced=false AND rated_games>0 LIMIT 100")
	if err != nil {
		logger.Error("MMR leaderboard reconciliation: %v", err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err = syncMMR(ctx, db, nk, id); err != nil {
			logger.Error("MMR leaderboard retry: %v", err)
		}
	}
}
