package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
)

type inventoryNakama struct {
	runtime.NakamaModule
	sync.Mutex
	objects map[string]*api.StorageObject
	wallets map[string]int64
	serial  int
}

func inventoryFake() *inventoryNakama {
	return &inventoryNakama{objects: map[string]*api.StorageObject{}, wallets: map[string]int64{}}
}
func objectKey(c, k, u string) string {
	if u == "" {
		u = systemStorageOwner
	}
	return c + ":" + k + ":" + u
}
func (n *inventoryNakama) StorageRead(ctx context.Context, reads []*runtime.StorageRead) ([]*api.StorageObject, error) {
	n.Lock()
	defer n.Unlock()
	out := []*api.StorageObject{}
	for _, r := range reads {
		if o := n.objects[objectKey(r.Collection, r.Key, r.UserID)]; o != nil {
			copy := *o
			out = append(out, &copy)
		}
	}
	return out, nil
}
func (n *inventoryNakama) AccountGetId(ctx context.Context, id string) (*api.Account, error) {
	n.Lock()
	defer n.Unlock()
	return &api.Account{User: &api.User{Id: id}, Wallet: fmt.Sprintf(`{"coins":%d}`, n.wallets[id])}, nil
}
func (n *inventoryNakama) StorageWrite(ctx context.Context, w []*runtime.StorageWrite) ([]*api.StorageObjectAck, error) {
	a, _, err := n.MultiUpdate(ctx, nil, w, nil, nil, false)
	return a, err
}
func (n *inventoryNakama) MultiUpdate(ctx context.Context, a []*runtime.AccountUpdate, w []*runtime.StorageWrite, d []*runtime.StorageDelete, updates []*runtime.WalletUpdate, ledger bool) ([]*api.StorageObjectAck, []*runtime.WalletUpdateResult, error) {
	n.Lock()
	defer n.Unlock()
	for _, write := range w {
		o := n.objects[objectKey(write.Collection, write.Key, write.UserID)]
		if write.Version == "*" && o != nil || write.Version != "" && write.Version != "*" && (o == nil || o.Version != write.Version) {
			return nil, nil, runtime.ErrStorageRejectedVersion
		}
	}
	for _, update := range updates {
		if n.wallets[update.UserID]+update.Changeset["coins"] < 0 {
			return nil, nil, &runtime.WalletNegativeError{UserID: update.UserID, Path: "coins"}
		}
	}
	acks := []*api.StorageObjectAck{}
	for _, write := range w {
		n.serial++
		v := fmt.Sprint(n.serial)
		var normalized interface{}
		json.Unmarshal([]byte(write.Value), &normalized)
		storedJSON, _ := json.Marshal(normalized)
		n.objects[objectKey(write.Collection, write.Key, write.UserID)] = &api.StorageObject{Collection: write.Collection, Key: write.Key, UserId: write.UserID, Value: string(storedJSON), Version: v, PermissionRead: int32(write.PermissionRead), PermissionWrite: int32(write.PermissionWrite)}
		acks = append(acks, &api.StorageObjectAck{Collection: write.Collection, Key: write.Key, UserId: write.UserID, Version: v})
	}
	for _, update := range updates {
		n.wallets[update.UserID] += update.Changeset["coins"]
	}
	return acks, nil, nil
}
func configForTest(t *testing.T) {
	t.Helper()
	old, v := activeConfig, activeConfigVersion
	config := defaultConfiguration(game.Catalog())
	activeConfig = &config
	activeConfigVersion = configurationHash(config)
	t.Cleanup(func() { activeConfig = old; activeConfigVersion = v })
}
func purchasePayload(id, request string) string {
	raw, _ := json.Marshal(map[string]string{"character_id": id, "request_id": request, "config_version": activeConfigVersion})
	return string(raw)
}
func unownedCharacter(p playerData) string {
	for _, c := range game.Catalog().Characters {
		if !ownsCharacter(p, c.ID) {
			return c.ID
		}
	}
	panic("no unowned character")
}
func TestCollectionPurchaseAtomicAndIdempotent(t *testing.T) {
	configForTest(t)
	nk := inventoryFake()
	ctx := queueContext("a")
	p, v, err := ensurePlayer(ctx, nk, "a", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Owned) != 40 || len(p.Decks) != 1 || v == "" {
		t.Fatal("starter collection missing")
	}
	id := unownedCharacter(p)
	price := activeConfig.Economy.Prices[id]
	if _, err = characterPurchase(ctx, nil, nil, nk, purchasePayload(id, "purchase")); err == nil {
		t.Fatal("negative wallet accepted")
	}
	p, _, _ = readPlayer(ctx, nk, "a")
	if ownsCharacter(p, id) || nk.wallets["a"] != 0 {
		t.Fatal("failed purchase partially committed")
	}
	nk.wallets["a"] = price
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := characterPurchase(ctx, nil, nil, nk, purchasePayload(id, "purchase"))
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
	p, _, _ = readPlayer(ctx, nk, "a")
	if !ownsCharacter(p, id) || nk.wallets["a"] != 0 {
		t.Fatal("purchase must grant once and charge once")
	}
	if _, err = characterPurchase(ctx, nil, nil, nk, purchasePayload(unownedCharacter(p), "purchase")); err == nil {
		t.Fatal("receipt reused for another item")
	}
	other, _, _ := ensurePlayer(queueContext("b"), nk, "b", nil, "")
	if ownsCharacter(other, id) {
		t.Fatal("ownership leaked to another account")
	}
	object := nk.objects[objectKey(playerCollection, playerKey, "a")]
	if object.PermissionRead != 1 || object.PermissionWrite != 0 {
		t.Fatal("player collection must be owner-read/server-write")
	}
}
func TestDeckSaveCASAndOwnership(t *testing.T) {
	configForTest(t)
	nk := inventoryFake()
	ctx := queueContext("a")
	p, v, _ := ensurePlayer(ctx, nk, "a", nil, "")
	d := p.Decks[0]
	d.Name = "My deck"
	raw, _ := json.Marshal(map[string]interface{}{"decks": []savedDeck{d}, "selected_deck_id": d.ID, "profile_version": v, "config_version": activeConfigVersion})
	if _, err := playerDecksSave(ctx, nil, nil, nk, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := playerDecksSave(ctx, nil, nil, nk, string(raw)); err == nil {
		t.Fatal("stale save overwrote newer deck")
	}
	activeConfig.Economy.AllUnlocked = false
	deck := game.DefaultDeck()
	if err := validateOwnedDeck(ctx, nk, "a", deck); err != nil {
		t.Fatal(err)
	}
	id := unownedCharacter(p)
	kind, _ := characterKind(id)
	group := game.CardGroup(kind)
	oldGroup := deck.Heroes[0]
	deck.Heroes[0] = group
	cards := []game.DeckCard{}
	for _, card := range deck.Cards {
		if game.CardGroup(card.Kind) != oldGroup {
			cards = append(cards, card)
		}
	}
	for cost := 2; cost <= 6; cost++ {
		count := 0
		for _, ch := range game.Catalog().Characters {
			if ch.Enabled && ch.Group == group && ch.Cost == cost && count < 2 {
				cards = append(cards, game.DeckCard{Kind: ch.Kind, Copies: ch.MaxCopies})
				count++
			}
		}
	}
	deck.Cards = cards
	if err := validateOwnedDeck(ctx, nk, "a", deck); err == nil {
		t.Fatal("locked character accepted")
	}
	activeConfig.Economy.AllUnlocked = true
	if err := validateOwnedDeck(ctx, nk, "a", deck); err != nil {
		t.Fatal("temporary all-access mode must allow unowned characters")
	}
	p, _, _ = readPlayer(ctx, nk, "a")
	if ownsCharacter(p, id) {
		t.Fatal("free-access mode permanently granted characters")
	}
}
func TestLegacyImportOnceAndAdminAuthorization(t *testing.T) {
	configForTest(t)
	nk := inventoryFake()
	ctx := queueContext("a")
	legacy := toSavedDeck("legacy", game.DefaultDeck())
	legacy.Name = "Original local deck"
	p, _, err := ensurePlayer(ctx, nk, "a", []savedDeck{legacy}, "legacy")
	if err != nil || p.Decks[0].Name != legacy.Name {
		t.Fatal("legacy migration failed", err)
	}
	legacy.Name = "Overwrite attempt"
	p, _, _ = ensurePlayer(ctx, nk, "a", []savedDeck{legacy}, "legacy")
	if p.Decks[0].Name == legacy.Name {
		t.Fatal("bootstrap overwrote existing collection")
	}
	if err = requireAdmin(ctx); err == nil {
		t.Fatal("player can administer economy")
	}
	admin := context.WithValue(context.Background(), runtime.RUNTIME_CTX_MODE, "rpc")
	raw := `{"user_id":"a","coins":250,"request_id":"grant-1"}`
	for i := 0; i < 2; i++ {
		if _, err = adminPlayerGrant(admin, nil, nil, nk, raw); err != nil {
			t.Fatal(err)
		}
	}
	if nk.wallets["a"] != 250 {
		t.Fatal("grant retry credited twice")
	}
	if _, err = adminPlayerGrant(admin, nil, nil, nk, `{"user_id":"a","coins":300,"request_id":"grant-1"}`); err == nil {
		t.Fatal("grant ID changed amount")
	}
	if err = validateConfiguration(*activeConfig); err != nil {
		t.Fatal(err)
	}
	copy := *activeConfig
	copy.Catalog = game.Catalog()
	copy.Catalog.Characters = append([]game.CharacterDefinition{}, copy.Catalog.Characters...)
	copy.Catalog.Characters[0].ID = "changed_id"
	if err = validateConfiguration(copy); err == nil {
		t.Fatal("admin can break stable character ID")
	}

}

func TestAdminPatchPublishDoesNotMutateActiveBattleConfiguration(t *testing.T) {
	configForTest(t)
	nk := inventoryFake()
	ctx := context.WithValue(context.Background(), runtime.RUNTIME_CTX_MODE, "rpc")
	raw, _ := json.Marshal(activeConfig)
	acks, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: configCollection, Key: configKey, Value: string(raw), Version: "*"}})
	if err != nil {
		t.Fatal(err)
	}
	originalHP := activeConfig.Catalog.Characters[0].Stats[0].HP
	stats := append([]game.CharacterStats{}, activeConfig.Catalog.Characters[0].Stats...)
	stats[0].HP++
	id := activeConfig.Catalog.Characters[0].ID
	patch, _ := json.Marshal(map[string]interface{}{"storage_version": acks[0].Version, "character_patches": map[string]interface{}{id: map[string]interface{}{"stats": stats}}})
	if _, err = adminConfigPublish(ctx, nil, nil, nk, string(patch)); err != nil {
		t.Fatal(err)
	}
	if activeConfig.Catalog.Characters[0].Stats[0].HP != originalHP || game.Character(0).Stats[0].HP != originalHP {
		t.Fatal("publish changed running match catalog")
	}
	objects, _ := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: configCollection, Key: configKey, UserID: systemStorageOwner}})
	var published gameConfiguration
	json.Unmarshal([]byte(objects[0].Value), &published)
	if published.Catalog.Characters[0].Stats[0].HP != originalHP+1 {
		t.Fatal("patch was not persisted")
	}
	if _, err = adminConfigPublish(ctx, nil, nil, nk, string(patch)); err == nil {
		t.Fatal("stale admin patch overwrote new publication")
	}
	patch, _ = json.Marshal(map[string]interface{}{"storage_version": objects[0].Version, "character_patches": map[string]interface{}{id: map[string]interface{}{"character_id": "hacked"}}})
	if _, err = adminConfigPublish(ctx, nil, nil, nk, string(patch)); err == nil {
		t.Fatal("identity patch accepted")
	}
}
