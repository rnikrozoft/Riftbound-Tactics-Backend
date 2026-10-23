package server

import (
	"context"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
	"time"
)

const roomDirectoryCollection = "riftbound_rooms"
const systemStorageOwner = "00000000-0000-0000-0000-000000000000"

type roomDirectoryEntry struct {
	Creator   string `json:"creator"`
	MatchID   string `json:"match_id"`
	ExpiresMs int64  `json:"expires_ms"`
}

func deleteRoomDirectory(ctx context.Context, nk runtime.NakamaModule, code string) {
	if nk == nil {
		return
	}
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: roomDirectoryCollection, Key: code, UserID: systemStorageOwner}})
	if err != nil || len(objects) != 1 {
		return
	}
	var entry roomDirectoryEntry
	if json.Unmarshal([]byte(objects[0].Value), &entry) != nil {
		return
	}
	matchID, _ := ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string)
	if matchID == "" || entry.MatchID != matchID {
		return
	}
	nk.StorageDelete(ctx, []*runtime.StorageDelete{{Collection: roomDirectoryCollection, Key: code, Version: objects[0].Version}})
}
func reclaimExpiredRoom(ctx context.Context, nk runtime.NakamaModule, code string) {
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: roomDirectoryCollection, Key: code, UserID: systemStorageOwner}})
	if err != nil || len(objects) != 1 {
		return
	}
	var entry roomDirectoryEntry
	if json.Unmarshal([]byte(objects[0].Value), &entry) != nil || entry.ExpiresMs > time.Now().UnixMilli() {
		return
	}
	if entry.MatchID != "" {
		match, err := nk.MatchGet(ctx, entry.MatchID)
		if err != nil || match != nil {
			return
		}
	}
	nk.StorageDelete(ctx, []*runtime.StorageDelete{{Collection: roomDirectoryCollection, Key: code, Version: objects[0].Version}})
}
