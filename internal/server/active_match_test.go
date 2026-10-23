package server

import (
	"context"
	"errors"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"strings"
	"testing"
)

type activeMatchFake struct {
	roomStorageFake
	live      bool
	signal    string
	signalErr error
	key       string
}

func (n *activeMatchFake) StorageRead(ctx context.Context, r []*runtime.StorageRead) ([]*api.StorageObject, error) {
	n.key = r[0].Key
	return n.roomStorageFake.StorageRead(ctx, r)
}
func (n *activeMatchFake) MatchGet(context.Context, string) (*api.Match, error) {
	if n.live {
		return &api.Match{MatchId: "match"}, nil
	}
	return nil, nil
}
func (n *activeMatchFake) MatchSignal(_ context.Context, _ string, data string) (string, error) {
	n.signal = data
	if n.signalErr != nil {
		return "", n.signalErr
	}
	return "available", nil
}
func TestActiveMatchDiscoveryUsesAuthenticatedAccount(t *testing.T) {
	n := &activeMatchFake{live: true}
	ctx := context.WithValue(queueContext("player"), runtime.RUNTIME_CTX_MATCH_ID, "match")
	if err := rememberActiveMatches(ctx, n, []string{"player"}, "CODE"); err != nil {
		t.Fatal(err)
	}
	w := n.writes[0]
	if w.PermissionRead != 0 || w.PermissionWrite != 0 || w.UserID != systemStorageOwner {
		t.Fatal("active match must be private")
	}
	result, err := activeMatch(queueContext("player"), nil, nil, n, `{"user_id":"other"}`)
	if err != nil || !strings.Contains(result, "match") || n.key != "player" || n.signal != "resume:player" {
		t.Fatalf("wrong discovery: %s %v", result, err)
	}
	if n.deleted != nil {
		t.Fatal("live match deleted")
	}
}
func TestActiveMatchStaleCleanupAndTransientFailure(t *testing.T) {
	n := &activeMatchFake{roomStorageFake: roomStorageFake{object: &api.StorageObject{Value: `{"match_id":"match","code":"CODE"}`, Version: "version"}}}
	if _, err := activeMatch(queueContext("player"), nil, nil, n, ""); err != nil {
		t.Fatal(err)
	}
	if n.deleted == nil || n.deleted.Version != "version" {
		t.Fatal("stale pointer requires versioned cleanup")
	}
	n.deleted = nil
	n.live = true
	n.signalErr = errors.New("temporary failure")
	if _, err := activeMatch(queueContext("player"), nil, nil, n, ""); err == nil || n.deleted != nil {
		t.Fatal("temporary failure must retain pointer")
	}
}
func TestForgetActiveMatchDoesNotClearNewerMatch(t *testing.T) {
	n := &activeMatchFake{roomStorageFake: roomStorageFake{object: &api.StorageObject{Value: `{"match_id":"new"}`, Version: "version"}}}
	if _, err := forgetActiveMatch(queueContext("player"), nil, nil, n, `{"match_id":"old"}`); err != nil {
		t.Fatal(err)
	}
	if n.deleted != nil {
		t.Fatal("old session cleared newer match")
	}
	if _, err := forgetActiveMatch(queueContext("player"), nil, nil, n, `{"match_id":"new"}`); err != nil {
		t.Fatal(err)
	}
	if n.deleted == nil || n.deleted.Version != "version" {
		t.Fatal("missing versioned deletion")
	}
}
