package server

import (
	"context"
	"github.com/heroiclabs/nakama-common/api"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"testing"
)

func TestClientCannotSeedOrDeleteReservedGameStorage(t *testing.T) {
	ctx := queueContext("attacker")
	for _, collection := range []string{playerCollection, configCollection, roomDirectoryCollection, battleCollection, ratingCollection, ratingResultCollection, winsCollection, winReceiptCollection, "riftbound_purchases", "riftbound_grants"} {
		// Includes first-time creation: permissions do not exist yet.
		request := &api.WriteStorageObjectsRequest{Objects: []*api.WriteStorageObject{{Collection: collection, Key: "account", Value: `{"mmr":99999}`, Version: "*", PermissionRead: wrapperspb.Int32(1), PermissionWrite: wrapperspb.Int32(1)}}}
		if out, err := beforeStorageWrite(ctx, nil, nil, nil, request); err == nil || out != nil {
			t.Fatal("reserved write accepted", collection)
		}
		deletion := &api.DeleteStorageObjectsRequest{ObjectIds: []*api.DeleteStorageObjectId{{Collection: collection, Key: "account"}}}
		if out, err := beforeStorageDelete(ctx, nil, nil, nil, deletion); err == nil || out != nil {
			t.Fatal("reserved delete accepted", collection)
		}
	}
	request := &api.WriteStorageObjectsRequest{Objects: []*api.WriteStorageObject{{Collection: "client_settings", Key: "local"}}}
	if out, err := beforeStorageWrite(context.Background(), nil, nil, nil, request); err != nil || out != request {
		t.Fatal("unrelated collection blocked", err)
	}
}
