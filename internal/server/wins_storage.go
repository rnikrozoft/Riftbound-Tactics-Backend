package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/heroiclabs/nakama-common/runtime"
)

const winsCollection = "riftbound_wins"
const winReceiptCollection = "riftbound_win_receipts"

type winsAccount struct {
	Wins   int64 `json:"wins"`
	Synced bool  `json:"leaderboard_synced"`
}

func readWins(ctx context.Context, nk runtime.NakamaModule, id string) (winsAccount, string, error) {
	var account winsAccount
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: winsCollection, Key: scoreAccountKey, UserID: id}})
	if err != nil {
		return account, "", err
	}
	if len(objects) == 0 {
		return account, "*", nil
	}
	err = json.Unmarshal([]byte(objects[0].Value), &account)
	return account, objects[0].Version, err
}

func recordWin(ctx context.Context, nk runtime.NakamaModule, matchID, id string) error {
	for attempt := 0; attempt < storageAttempts; attempt++ {
		account, version, err := readWins(ctx, nk, id)
		if err != nil {
			return err
		}
		receipts, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: winReceiptCollection, Key: matchID, UserID: systemStorageOwner}})
		if err != nil {
			return err
		}
		if len(receipts) > 0 {
			return nil
		}
		account.Wins++
		account.Synced = false
		raw, err := json.Marshal(account)
		if err != nil {
			return err
		}
		_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{
			{Collection: winsCollection, Key: scoreAccountKey, UserID: id, Value: string(raw), Version: version, PermissionRead: 0, PermissionWrite: 0},
			{Collection: winReceiptCollection, Key: matchID, UserID: systemStorageOwner, Value: `{}`, Version: "*", PermissionRead: 0, PermissionWrite: 0},
		})
		if errors.Is(err, runtime.ErrStorageRejectedVersion) {
			continue
		}
		return err
	}
	return runtime.NewError("Wins changed; retry", 10)
}
