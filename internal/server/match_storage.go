package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
)

const battleCollection = "riftbound_battles"

// One immutable object per round; repeated delivery never overwrites a result.
func persistBattle(ctx context.Context, nk runtime.NakamaModule, matchID string, round int, code string, result json.RawMessage) error {
	raw, err := json.Marshal(struct {
		MatchID string          `json:"match_id"`
		Round   int             `json:"round"`
		Code    string          `json:"code"`
		Result  json.RawMessage `json:"result"`
	}{matchID, round, code, result})
	if err != nil {
		return err
	}
	_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: battleCollection, Key: fmt.Sprintf("%s:%d", matchID, round), Value: string(raw), Version: "*", PermissionRead: 0, PermissionWrite: 0}})
	if errors.Is(err, runtime.ErrStorageRejectedVersion) {
		return nil
	}
	return err
}
