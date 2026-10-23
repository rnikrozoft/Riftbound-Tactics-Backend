package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"hash/fnv"
	"sync"
)

const mmrLeaderboard = "riftbound_mmr"

// The configured single Nakama node serializes writes per owner, not globally.
// Storage CAS remains the authority for concurrent matches and retries.
var leaderboardLocks [64]sync.Mutex

func ownerLock(id string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return &leaderboardLocks[h.Sum32()%uint32(len(leaderboardLocks))]
}

func leaderboardName(ctx context.Context, nk runtime.NakamaModule, id string) (string, error) {
	users, err := nk.UsersGetId(ctx, []string{id}, nil)
	if err != nil {
		return "", err
	}
	if len(users) != 1 {
		return "", fmt.Errorf("leaderboard account missing")
	}
	if users[0].DisplayName != "" {
		return users[0].DisplayName, nil
	}
	return users[0].Username, nil
}

func syncMMR(ctx context.Context, nk runtime.NakamaModule, id string) error {
	lock := ownerLock(id)
	lock.Lock()
	defer lock.Unlock()
	account, version, err := readRating(ctx, nk, id)
	if err != nil || version == "*" || account.Synced || account.Games == 0 {
		return err
	}
	name, err := leaderboardName(ctx, nk, id)
	if err != nil {
		return err
	}
	if _, err = nk.LeaderboardRecordWrite(ctx, mmrLeaderboard, id, name, int64(account.Rating), 0, map[string]interface{}{"rated_games": account.Games}, nil); err != nil {
		return err
	}
	account.Synced = true
	raw, err := json.Marshal(account)
	if err != nil {
		return err
	}
	_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: ratingCollection, Key: scoreAccountKey, UserID: id, Value: string(raw), Version: version, PermissionRead: 0, PermissionWrite: 0}})
	// A newer rating stays dirty and will be reconciled, never overwritten.
	if errors.Is(err, runtime.ErrStorageRejectedVersion) {
		return nil
	}
	return err
}

func syncWinner(ctx context.Context, nk runtime.NakamaModule, id string) error {
	lock := ownerLock(id)
	lock.Lock()
	defer lock.Unlock()
	account, version, err := readWins(ctx, nk, id)
	if err != nil || version == "*" || account.Synced {
		return err
	}
	name, err := leaderboardName(ctx, nk, id)
	if err != nil {
		return err
	}
	if _, err = nk.LeaderboardRecordWrite(ctx, winLeaderboard, id, name, account.Wins, 0, map[string]interface{}{"wins": account.Wins}, nil); err != nil {
		return err
	}
	account.Synced = true
	raw, err := json.Marshal(account)
	if err != nil {
		return err
	}
	_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: winsCollection, Key: scoreAccountKey, UserID: id, Value: string(raw), Version: version, PermissionRead: 0, PermissionWrite: 0}})
	if errors.Is(err, runtime.ErrStorageRejectedVersion) {
		return nil
	}
	return err
}
