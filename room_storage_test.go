package main

import (
	"context"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"testing"
	"time"
)

type roomStorageFake struct {
	runtime.NakamaModule
	object  *api.StorageObject
	writes  []*runtime.StorageWrite
	deleted *runtime.StorageDelete
}

func (n *roomStorageFake) StorageWrite(ctx context.Context, w []*runtime.StorageWrite) ([]*api.StorageObjectAck, error) {
	n.writes = append(n.writes, w...)
	n.object = &api.StorageObject{Value: w[0].Value, Version: "v2"}
	return []*api.StorageObjectAck{{Version: "v2"}}, nil
}
func (n *roomStorageFake) StorageRead(ctx context.Context, r []*runtime.StorageRead) ([]*api.StorageObject, error) {
	if r[0].UserID != systemStorageOwner {
		panic("incorrect system owner")
	}
	return []*api.StorageObject{n.object}, nil
}
func (n *roomStorageFake) StorageDelete(ctx context.Context, d []*runtime.StorageDelete) error {
	n.deleted = d[0]
	return nil
}
func (n *roomStorageFake) MatchCreate(context.Context, string, map[string]interface{}) (string, error) {
	return "match", nil
}
func TestRoomDirectoryPrivateAndVersioned(t *testing.T) {
	n := &roomStorageFake{}
	raw, _ := json.Marshal(map[string]interface{}{"deck": game.DefaultDeck()})
	if _, err := createRoom(queueContext("player"), nil, nil, n, string(raw)); err != nil {
		t.Fatal(err)
	}
	if len(n.writes) != 2 || n.writes[0].Version != "*" || n.writes[1].Version != "v2" {
		t.Fatal("missing reservation CAS")
	}
	for _, w := range n.writes {
		if w.PermissionRead != 0 || w.PermissionWrite != 0 || w.UserID != "" {
			t.Fatal("directory exposed to clients")
		}
	}
	ctx := context.WithValue(context.Background(), runtime.RUNTIME_CTX_MATCH_ID, "different-match")
	deleteRoomDirectory(ctx, n, n.writes[0].Key)
	if n.deleted != nil {
		t.Fatal("stale match deleted newer room")
	}
	ctx = context.WithValue(ctx, runtime.RUNTIME_CTX_MATCH_ID, "match")
	deleteRoomDirectory(ctx, n, n.writes[0].Key)
	if n.deleted == nil || n.deleted.Version != "v2" {
		t.Fatal("cleanup must be versioned")
	}
	var e roomDirectoryEntry
	json.Unmarshal([]byte(n.object.Value), &e)
	if e.Creator != "player" || e.ExpiresMs <= time.Now().UnixMilli() {
		t.Fatal("invalid directory record")
	}
}
