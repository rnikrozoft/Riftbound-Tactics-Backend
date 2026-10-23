package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/heroiclabs/nakama-common/runtime"
)

const ratingCollection = "riftbound_ratings"
const ratingResultCollection = "riftbound_rating_results"
const scoreAccountKey = "account"
const storageAttempts = 8

type ratingAccount struct {
	MMRAccount
	Synced bool `json:"leaderboard_synced"`
}

type ratingReceipt struct {
	MMRResult
	Placement int `json:"placement"`
}

func readRating(ctx context.Context, nk runtime.NakamaModule, id string) (ratingAccount, string, error) {
	account := ratingAccount{MMRAccount: MMRAccount{Rating: initialMMR}}
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: ratingCollection, Key: scoreAccountKey, UserID: id}})
	if err != nil {
		return account, "", err
	}
	if len(objects) == 0 {
		return account, "*", nil
	}
	err = json.Unmarshal([]byte(objects[0].Value), &account)
	return account, objects[0].Version, err
}

// The account CAS and create-only receipt commit in one storage transaction.
// Concurrent matches retry from the current rating; duplicate results return the receipt.
func persistMMR(ctx context.Context, nk runtime.NakamaModule, matchID, id string, place, delta int, rated bool) (MMRResult, error) {
	for attempt := 0; attempt < storageAttempts; attempt++ {
		// Read account before receipt: a receipt committed after this read will
		// invalidate the account version rather than permit a duplicate increment.
		account, version, err := readRating(ctx, nk, id)
		if err != nil {
			return MMRResult{}, err
		}
		receipts, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: ratingResultCollection, Key: matchID, UserID: id}})
		if err != nil {
			return MMRResult{}, err
		}
		if len(receipts) > 0 {
			var old ratingReceipt
			err = json.Unmarshal([]byte(receipts[0].Value), &old)
			return old.MMRResult, err
		}
		after := account.Rating + delta
		if after < 0 {
			after = 0
		}
		result := MMRResult{account.Rating, after, after - account.Rating, rated}
		account.Rating = after
		if rated {
			account.Games++
		}
		account.Synced = false
		raw, err := json.Marshal(account)
		if err != nil {
			return MMRResult{}, err
		}
		receipt, err := json.Marshal(ratingReceipt{result, place})
		if err != nil {
			return MMRResult{}, err
		}
		_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{
			{Collection: ratingCollection, Key: scoreAccountKey, UserID: id, Value: string(raw), Version: version, PermissionRead: 0, PermissionWrite: 0},
			{Collection: ratingResultCollection, Key: matchID, UserID: id, Value: string(receipt), Version: "*", PermissionRead: 0, PermissionWrite: 0},
		})
		if errors.Is(err, runtime.ErrStorageRejectedVersion) {
			continue
		}
		return result, err
	}
	return MMRResult{}, runtime.NewError("Rating changed; retry", 10)
}
