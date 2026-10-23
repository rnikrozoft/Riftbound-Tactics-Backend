package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
	"strings"
)

const activeMatchCollection = "riftbound_active_matches"

type activeMatchEntry struct {
	MatchID string `json:"match_id"`
	Code    string `json:"code"`
}

// Server-owned discovery survives a game-client restart. The live match remains
// authoritative; a stored pointer alone never permits admission or restoration.
func rememberActiveMatches(ctx context.Context, nk runtime.NakamaModule, ids []string, code string) error {
	matchID, _ := ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string)
	if nk == nil || matchID == "" {
		return nil
	}
	raw, _ := json.Marshal(activeMatchEntry{MatchID: matchID, Code: code})
	writes := []*runtime.StorageWrite{}
	for _, id := range ids {
		if id != "" {
			writes = append(writes, &runtime.StorageWrite{Collection: activeMatchCollection, Key: id, UserID: systemStorageOwner, Value: string(raw), PermissionRead: 0, PermissionWrite: 0})
		}
	}
	if len(writes) == 0 {
		return nil
	}
	_, err := nk.StorageWrite(ctx, writes)
	return err
}

func activeMatch(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: activeMatchCollection, Key: id, UserID: systemStorageOwner}})
	if err != nil {
		return "", err
	}
	if len(objects) == 0 {
		return roomResponse("", "")
	}
	object := objects[0]
	var entry activeMatchEntry
	if json.Unmarshal([]byte(object.Value), &entry) != nil {
		return "", runtime.NewError("Invalid active match record", 13)
	}
	if entry.MatchID == "" {
		return roomResponse("", "")
	}
	live, err := nk.MatchGet(ctx, entry.MatchID)
	if err != nil {
		return "", err
	}
	available := false
	if live != nil {
		result, signalErr := nk.MatchSignal(ctx, entry.MatchID, "resume:"+id)
		if signalErr != nil {
			return "", signalErr
		}
		available = result == "available"
	}
	if !available {
		if err := nk.StorageDelete(ctx, []*runtime.StorageDelete{{Collection: activeMatchCollection, Key: id, UserID: systemStorageOwner, Version: object.Version}}); err != nil {
			return "", err
		}
		return roomResponse("", "")
	}
	return roomResponse(entry.Code, entry.MatchID)
}

func forgetActiveMatch(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	var request activeMatchEntry
	if json.Unmarshal([]byte(payload), &request) != nil || request.MatchID == "" {
		return "", runtime.NewError("Invalid match", 3)
	}
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: activeMatchCollection, Key: id, UserID: systemStorageOwner}})
	if err != nil {
		return "", err
	}
	if len(objects) > 0 {
		var entry activeMatchEntry
		if json.Unmarshal([]byte(objects[0].Value), &entry) == nil && entry.MatchID == request.MatchID {
			err = nk.StorageDelete(ctx, []*runtime.StorageDelete{{Collection: activeMatchCollection, Key: id, UserID: systemStorageOwner, Version: objects[0].Version}})
		}
	}
	return "{}", err
}

func resumeUser(data string) (string, bool) {
	return strings.TrimPrefix(data, "resume:"), strings.HasPrefix(data, "resume:")
}
