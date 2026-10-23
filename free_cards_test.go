package main

import (
	"context"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"reflect"
	"testing"
)

func TestFreeCardsGrantedToNewAndExistingPlayers(t *testing.T) {
	configForTest(t)
	activeConfig.Economy.FreeCharacterCards = true
	activeConfig.Economy.FreeCardBacks = true
	activeConfigVersion = configurationHash(*activeConfig)
	nk := inventoryFake()
	ctx := context.Background()
	legacy := playerData{SchemaVersion: 1, Owned: []string{game.Character(460).ID}, OwnedCardBacks: []string{game.DefaultCardBackID}, Decks: []savedDeck{}, SelectedID: ""}
	if _, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{playerWrite("existing", legacy, "*")}); err != nil {
		t.Fatal(err)
	}
	nk.wallets["existing"] = 123
	for _, id := range []string{"existing", "new"} {
		p, v, err := ensurePlayer(ctx, nk, id, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Owned) != 100 || len(p.OwnedCardBacks) != 20 {
			t.Fatal("incomplete free distribution", id)
		}
		again, next, err := ensurePlayer(ctx, nk, id, nil, "")
		if err != nil || next != v || !reflect.DeepEqual(p, again) {
			t.Fatal("grant must persist and be idempotent")
		}
		if id == "existing" && (len(p.Decks) != 0 || p.SelectedID != "" || nk.wallets[id] != 123) {
			t.Fatal("grant changed deleted decks or wallet")
		}
	}
	p, _, _ := ensurePlayer(ctx, nk, "new", nil, "")
	d := game.DefaultDeck()
	d.CardBackID = "card_back_19"
	if !collectionAllows(p, d) {
		t.Fatal("free backs not usable")
	}
	userctx := queueContext("existing")
	for _, purchase := range []func() (string, error){
		func() (string, error) {
			return characterPurchase(userctx, nil, nil, nk, purchasePayload(game.Character(499).ID, "no-charge"))
		},
		func() (string, error) {
			return characterGroupPurchase(userctx, nil, nil, nk, `{"group":48,"request_id":"no-charge"}`)
		},
		func() (string, error) {
			raw, _ := json.Marshal(map[string]string{"card_back_id": "card_back_19", "request_id": "no-charge", "config_version": activeConfigVersion})
			return cardBackPurchase(userctx, nil, nil, nk, string(raw))
		},
	} {
		if _, err := purchase(); err == nil {
			t.Fatal("shop purchase accepted during free distribution")
		}
	}
	if nk.wallets["existing"] != 123 {
		t.Fatal("free cards charged wallet")
	}
}
