package main

import (
	"context"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"reflect"
	"sync"
	"testing"
)

func backPayload(id, request string) string {
	b, _ := json.Marshal(map[string]interface{}{"card_back_id": id, "request_id": request, "config_version": activeConfigVersion, "price": 0, "user_id": "other"})
	return string(b)
}
func TestCardBackPurchaseAtomicAndPrivate(t *testing.T) {
	configForTest(t)
	nk := inventoryFake()
	ctx := queueContext("backs")
	p, _, err := ensurePlayer(ctx, nk, "backs", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !ownsCardBack(p, "") || ownsCardBack(p, "card_back_01") {
		t.Fatal("default ownership incorrect")
	}
	before := p
	if _, err = cardBackPurchase(ctx, nil, nil, nk, backPayload("card_back_01", "buy")); err == nil {
		t.Fatal("negative wallet purchase accepted")
	}
	p, _, _ = readPlayer(ctx, nk, "backs")
	if !reflect.DeepEqual(before, p) || nk.wallets["backs"] != 0 {
		t.Fatal("failed purchase changed account")
	}
	nk.wallets["backs"] = 100
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := cardBackPurchase(ctx, nil, nil, nk, backPayload("card_back_01", "buy"))
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	p, _, _ = readPlayer(ctx, nk, "backs")
	if !ownsCardBack(p, "card_back_01") || nk.wallets["backs"] != 0 || len(p.OwnedCardBacks) != 2 {
		t.Fatal("purchase not exactly once")
	}
	if _, err = cardBackPurchase(ctx, nil, nil, nk, backPayload("card_back_02", "buy")); err == nil {
		t.Fatal("receipt reused for another back")
	}
	if _, err = characterPurchase(ctx, nil, nil, nk, purchasePayload(unownedCharacter(p), "buy")); err == nil {
		t.Fatal("receipt reused across product types")
	}
	for _, id := range []string{"unknown", ""} {
		if _, err = cardBackPurchase(ctx, nil, nil, nk, backPayload(id, "invalid")); err == nil {
			t.Fatal("unknown back accepted")
		}
	}
	if _, err = cardBackPurchase(ctx, nil, nil, nk, `{"card_back_id":"card_back_02","request_id":"stale","config_version":"old"}`); err == nil {
		t.Fatal("stale config accepted")
	}
	other, _, e := ensurePlayer(queueContext("other"), nk, "other", nil, "")
	if e != nil {
		t.Fatal(e)
	}
	if ownsCardBack(other, "card_back_01") {
		t.Fatal("ownership leaked to another account")
	}
}
func TestCardBackDeckOwnershipAndReload(t *testing.T) {
	configForTest(t)
	nk := inventoryFake()
	ctx := queueContext("deck-backs")
	p, v, err := ensurePlayer(ctx, nk, "deck-backs", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	d := p.Decks[0]
	d.CardBackID = "card_back_01"
	save := func(version string, deck savedDeck) error {
		raw, _ := json.Marshal(map[string]interface{}{"decks": []savedDeck{deck}, "selected_deck_id": deck.ID, "profile_version": version, "config_version": activeConfigVersion})
		_, e := playerDecksSave(ctx, nil, nil, nk, string(raw))
		return e
	}
	if save(v, d) == nil {
		t.Fatal("unowned cosmetic saved")
	}
	activeConfig.Economy.AllUnlocked = true
	decoded, err := decodeSavedDeck(d)
	if err != nil {
		t.Fatal(err)
	}
	if validateOwnedDeck(ctx, nk, "deck-backs", decoded) == nil {
		t.Fatal("free character mode unlocked paid cosmetics")
	}
	nk.wallets["deck-backs"] = 100
	if _, err = cardBackPurchase(ctx, nil, nil, nk, backPayload(d.CardBackID, "unlock")); err != nil {
		t.Fatal(err)
	}
	if save(v, d) == nil {
		t.Fatal("stale save overwrote purchase")
	}
	p, v, _ = readPlayer(ctx, nk, "deck-backs")
	if err = save(v, d); err != nil {
		t.Fatal(err)
	}
	p, v, err = ensurePlayer(ctx, nk, "deck-backs", nil, "")
	if err != nil || p.Decks[0].CardBackID != d.CardBackID {
		t.Fatal("reload lost chosen back", err)
	}
	decoded, err = decodeSavedDeck(p.Decks[0])
	if err != nil {
		t.Fatal(err)
	}
	room := game.New("backs", "deck-backs", 42)
	if err = room.SetDeck("deck-backs", decoded); err != nil {
		t.Fatal(err)
	}
	if room.Player("deck-backs").CardBackID != d.CardBackID {
		t.Fatal("room omitted back")
	}
	league, err := game.NewLeague([]game.Seat{{ID: "deck-backs", Deck: decoded, Rating: 1000}, {ID: "other", Deck: game.DefaultDeck(), Rating: 1000}}, 42)
	if err != nil || league.Players[0].CardBackID != d.CardBackID {
		t.Fatal("league omitted back", err)
	}
	raw, _ := json.Marshal(room.State)
	var state game.State
	if json.Unmarshal(raw, &state) != nil || state.Players[0].CardBackID != d.CardBackID {
		t.Fatal("network/replay snapshot lost back")
	}
	legacy := d
	legacy.CardBackID = ""
	parsed, err := decodeSavedDeck(legacy)
	if err != nil || parsed.CardBackID != game.DefaultCardBackID {
		t.Fatal("old deck lacks default back")
	}
}

func TestPaidBacksRestoreAfterGiveaway(t *testing.T) {
	configForTest(t)
	activeConfig.Economy.FreeCharacterCards = true
	activeConfig.Economy.FreeCardBacks = false
	nk := inventoryFake()
	ctx := context.Background()
	d := toSavedDeck("keep-deck", game.DefaultDeck())
	d.CardBackID = "card_back_19"
	paid := d
	paid.ID = "paid-deck"
	paid.CardBackID = "card_back_01"
	p := playerData{SchemaVersion: 1, Decks: []savedDeck{d, paid}, SelectedID: d.ID}
	for _, back := range game.CardBacks {
		p.OwnedCardBacks = append(p.OwnedCardBacks, back.ID)
	}
	nk.wallets["giveaway"] = 73
	if _, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{playerWrite("giveaway", p, "*"), {Collection: "riftbound_purchases", Key: "paid-before-giveaway", UserID: "giveaway", Value: `{"type":"card_back","card_back_id":"card_back_01","price":100}`, Version: "*"}}); err != nil {
		t.Fatal(err)
	}
	migrated, v, err := ensurePlayer(ctx, nk, "giveaway", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrated.Owned) != 100 || len(migrated.OwnedCardBacks) != 2 || !ownsCardBack(migrated, "card_back_01") || ownsCardBack(migrated, "card_back_19") {
		t.Fatal("paid ownership not preserved or free backs survived")
	}
	if migrated.Decks[0].CardBackID != game.DefaultCardBackID || migrated.Decks[1].CardBackID != "card_back_01" || migrated.SelectedID != d.ID || nk.wallets["giveaway"] != 73 {
		t.Fatal("migration changed deck identity, paid selection or wallet")
	}
	again, next, err := ensurePlayer(ctx, nk, "giveaway", nil, "")
	if err != nil || v != next || !reflect.DeepEqual(migrated, again) {
		t.Fatal("giveaway correction repeated")
	}
	fresh, _, err := ensurePlayer(ctx, nk, "new-paid-backs", nil, "")
	if err != nil || len(fresh.Owned) != 100 || len(fresh.OwnedCardBacks) != 1 {
		t.Fatal("new account must receive forty-two characters and only default back")
	}
}
