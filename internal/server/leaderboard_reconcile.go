package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
	"time"
)

// Scan storage itself (not an eventually-consistent index). One bounded page per
// pass avoids starving owners behind the first page, including after restart.
func reconcilePage(ctx context.Context, logger runtime.Logger, nk runtime.NakamaModule, collection, cursor string, syncOwner func(context.Context, runtime.NakamaModule, string) error) string {
	objects, next, err := nk.StorageList(ctx, "", "", collection, 100, cursor)
	if err != nil {
		logger.Error("Leaderboard scan %s: %v", collection, err)
		return ""
	}
	for _, object := range objects {
		var status struct {
			Synced bool `json:"leaderboard_synced"`
		}
		if err := json.Unmarshal([]byte(object.Value), &status); err != nil {
			logger.Error("Leaderboard data %s/%s: %v", collection, object.UserId, err)
			continue
		}
		if !status.Synced {
			if err := syncOwner(ctx, nk, object.UserId); err != nil {
				logger.Error("Leaderboard retry %s/%s: %v", collection, object.UserId, err)
			}
		}
	}
	return next
}

func startLeaderboardReconciler(initializer runtime.Initializer, logger runtime.Logger, nk runtime.NakamaModule) error {
	parent, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	if err := initializer.RegisterShutdown(func(ctx context.Context, _ runtime.Logger, _ *sql.DB, _ runtime.NakamaModule) {
		cancel()
		defer stopPersistence(ctx)
		select {
		case <-done:
		case <-ctx.Done():
		}
	}); err != nil {
		cancel()
		return err
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		winsCursor, ratingCursor := "", ""
		for {
			ctx, stop := context.WithTimeout(parent, 10*time.Second)
			winsCursor = reconcilePage(ctx, logger, nk, winsCollection, winsCursor, syncWinner)
			ratingCursor = reconcilePage(ctx, logger, nk, ratingCollection, ratingCursor, syncMMR)
			stop()
			select {
			case <-parent.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}
