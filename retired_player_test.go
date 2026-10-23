package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"reflect"
	"testing"
)

func TestRetiredPlayerDeckMigration(t *testing.T) {
	old := game.Catalog()
	legacy := old
	legacy.Characters = append([]game.CharacterDefinition{}, old.Characters...)
	for i := range legacy.Characters {
		if legacy.Characters[i].Group == 1 || legacy.Characters[i].Group == 2 {
			legacy.Characters[i].Enabled = true
		}
	}
	game.UseCatalog(legacy)
	d := toSavedDeck("custom", game.DefaultDeck())
	for cost := 2; cost <= 6; cost++ {
		n := 0
		for _, c := range legacy.Characters {
			if c.Group == 4 && c.Cost == cost && n < 2 {
				d.Cards = append(d.Cards, savedCard{c.ID, 2})
				n++
			}
		}
	}
	d.Name = "My expedition"
	d.Cards[0].Copies = 1
	p := playerData{SchemaVersion: 1, Decks: []savedDeck{d}, SelectedID: d.ID}
	for _, c := range d.Cards {
		p.Owned = append(p.Owned, c.CharacterID)
	}
	game.UseCatalog(old)
	defer game.UseCatalog(old)
	nk := inventoryFake()
	ctx := context.Background()
	if _, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{playerWrite("legacy", p, "*")}); err != nil {
		t.Fatal(err)
	}
	migrated, version, err := ensurePlayer(ctx, nk, "legacy", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := decodeSavedDeck(migrated.Decks[0])
	if err != nil {
		t.Fatal(err)
	}
	if deck.Cards[0].Copies != 1 || deck.Name != d.Name || migrated.SelectedID != d.ID || len(migrated.Owned) < 30 {
		t.Fatal("migration lost saved choices")
	}
	for _, h := range deck.Heroes {
		if h >= 1 && h <= 4 {
			t.Fatal("retired hero survived")
		}
	}
	again, nextVersion, err := ensurePlayer(ctx, nk, "legacy", nil, "")
	if err != nil || version != nextVersion || !reflect.DeepEqual(again, migrated) {
		t.Fatal("migration must run only once", err)
	}
}

func TestRetirementOverridesPreviouslyPublishedGroups(t *testing.T) {
	c := defaultConfiguration(game.Catalog())
	c.Catalog.Characters = append([]game.CharacterDefinition{}, c.Catalog.Characters...)
	for i := range c.Catalog.Characters {
		if c.Catalog.Characters[i].Group == 1 || c.Catalog.Characters[i].Group == 2 {
			c.Catalog.Characters[i].Enabled = true
		}
	}
	c.Economy.AllUnlocked = true
	retireStarterGroups(&c)
	if err := validateConfiguration(c); err != nil {
		t.Fatal(err)
	}
	for _, card := range c.Catalog.Characters {
		if (card.Group == 1 || card.Group == 2) && card.Enabled {
			t.Fatal("published config revived a retired group")
		}
	}
	if !c.Economy.AllUnlocked {
		t.Fatal("retirement changed economy settings")
	}
}

func TestRetireGuardianAndNeutralPreservesPaidCosmetics(t *testing.T) {
	old := game.Catalog()
	d := savedDeck{ID: "guardian-deck", Name: "Keep my deck", Heroes: []int{0, 3, 5}, CardBackID: "card_back_19"}
	for _, g := range []int{0, 3, 5, 4} {
		for cost := 2; cost <= 6; cost++ {
			n := 0
			for _, c := range old.Characters {
				if c.Group == g && c.Cost == cost && n < 2 {
					d.Cards = append(d.Cards, savedCard{c.ID, 2})
					n++
				}
			}
		}
	}
	p := playerData{SchemaVersion: 1, Decks: []savedDeck{d}, OwnedCardBacks: []string{game.DefaultCardBackID, "card_back_19"}, SelectedID: d.ID}
	for _, c := range d.Cards {
		p.Owned = append(p.Owned, c.CharacterID)
	}
	if !repairRetiredPlayer(&p) {
		t.Fatal("old Guardian deck not migrated")
	}
	parsed, err := decodeSavedDeck(p.Decks[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Cards) != 30 || parsed.Heroes[1] != 46 || parsed.Heroes[2] != 47 || parsed.CardBackID != "card_back_19" || p.SelectedID != d.ID || !ownsCardBack(p, "card_back_19") {
		t.Fatal("migration changed saved choices")
	}
	for _, c := range parsed.Cards {
		if c.Copies != 2 {
			t.Fatal("migration lost copy counts")
		}
	}
	if repairRetiredPlayer(&p) {
		t.Fatal("migration repeated")
	}
	empty := playerData{SchemaVersion: 1, Decks: []savedDeck{}, Owned: []string{game.Character(2).ID, game.Character(0).ID}, OwnedCardBacks: []string{"card_back_19"}}
	if !repairRetiredPlayer(&empty) || len(empty.Decks) != 0 || len(empty.Owned) != 3 || !ownsCardBack(empty, "card_back_19") {
		t.Fatal("empty deck account not preserved")
	}
}

func TestLegacyRosterOwnershipAndDeckUpgrade(t *testing.T) {
	d := savedDeck{ID: "legacy-roster", Name: "Preserved deck", Heroes: []int{0, 5, 6}, CardBackID: "card_back_19"}
	for _, group := range d.Heroes {
		for cost := 2; cost <= 6; cost++ {
			count := 0
			for _, card := range game.Catalog().Characters {
				if card.Group == group && card.Cost == cost && count < 2 {
					d.Cards = append(d.Cards, savedCard{card.ID, 1 + count})
					count++
				}
			}
		}
	}
	p := playerData{Decks: []savedDeck{d}, SelectedID: d.ID, OwnedCardBacks: []string{"card_back_19"}}
	for _, card := range d.Cards {
		p.Owned = append(p.Owned, card.CharacterID)
	}
	historical := append([]string{}, p.Owned...)
	if !repairRetiredPlayer(&p) {
		t.Fatal("legacy roster not repaired")
	}
	decoded, err := decodeSavedDeck(p.Decks[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Heroes, []int{45, 46, 47}) || decoded.Name != d.Name || decoded.CardBackID != d.CardBackID || p.SelectedID != d.ID {
		t.Fatal("saved choices lost")
	}
	for i, card := range decoded.Cards {
		if card.Copies != d.Cards[i].Copies {
			t.Fatal("copy count lost")
		}
	}
	for _, id := range historical {
		found := false
		for _, owned := range p.Owned {
			if owned == id {
				found = true
			}
		}
		if !found {
			t.Fatal("paid ownership history lost")
		}
	}
	if repairRetiredPlayer(&p) {
		t.Fatal("migration is not idempotent")
	}
}

type rosterInitializer struct{ runtime.Initializer }

func (rosterInitializer) RegisterRpc(_ string, _ func(context.Context, runtime.Logger, *sql.DB, runtime.NakamaModule, string) (string, error)) error {
	return nil
}

func TestStartupUpgradesPersistedLegacyCatalogWithoutCorruptingSeed(t *testing.T) {
	old, oldConfig, oldVersion := game.Catalog(), activeConfig, activeConfigVersion
	defer func() { game.UseCatalog(old); activeConfig = oldConfig; activeConfigVersion = oldVersion }()
	legacy := defaultConfiguration(old)
	legacy.Catalog.RosterVersion = 0
	legacy.Catalog.Characters = append([]game.CharacterDefinition{}, old.Characters[:460]...)
	legacy.Catalog.GroupNames = append([]string{}, old.GroupNames[:45]...)
	for i := range legacy.Catalog.Characters {
		legacy.Catalog.Characters[i].Enabled = legacy.Catalog.Characters[i].Group == 0 || legacy.Catalog.Characters[i].Group >= 5
	}
	legacy.Economy.AllUnlocked = true
	raw, _ := json.Marshal(legacy)
	nk := inventoryFake()
	ctx := context.Background()
	if _, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: configCollection, Key: configKey, Value: string(raw), Version: "*"}}); err != nil {
		t.Fatal(err)
	}
	if err := initializePlayerStorage(ctx, nk, rosterInitializer{}, old); err != nil {
		t.Fatal(err)
	}
	if len(game.HeroGroups()) != 4 || len(game.Catalog().Characters) != 562 || !activeConfig.Economy.AllUnlocked {
		t.Fatal("startup lost roster or economy")
	}
	stored, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: configCollection, Key: configKey}})
	if err != nil {
		t.Fatal(err)
	}
	var published gameConfiguration
	if err = json.Unmarshal([]byte(stored[0].Value), &published); err != nil {
		t.Fatal(err)
	}
	if published.Catalog.RosterVersion != 14 || configurationHash(published) != activeConfigVersion {
		t.Fatal("administration and runtime catalog differ")
	}
	version := stored[0].Version
	if err := initializePlayerStorage(ctx, nk, rosterInitializer{}, game.Catalog()); err != nil {
		t.Fatal(err)
	}
	stored, _ = nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: configCollection, Key: configKey}})
	if stored[0].Version != version {
		t.Fatal("startup repeatedly rewrote roster")
	}
}

func TestPurchasedLegacyGroupRemainsUnlockedWithNoDecks(t *testing.T) {
	p := playerData{Decks: []savedDeck{}}
	for _, card := range game.Catalog().Characters {
		if card.Group == 7 {
			p.Owned = append(p.Owned, card.ID)
		}
	}
	if !repairRetiredPlayer(&p) || len(p.Decks) != 0 {
		t.Fatal("deleted decks recreated")
	}
	for _, card := range game.Catalog().Characters {
		if !card.Enabled || card.Group != 48 {
			continue
		}
		owned := false
		for _, id := range p.Owned {
			if id == card.ID {
				owned = true
			}
		}
		if !owned {
			t.Fatal("full paid group lost", card.ID)
		}
	}
	if repairRetiredPlayer(&p) {
		t.Fatal("paid group migration repeated")
	}
}

func TestStartupUpgradesVersionTwoTinyPackCatalog(t *testing.T) {
	c := defaultConfiguration(game.Catalog())
	c.Catalog.Characters = append([]game.CharacterDefinition{}, c.Catalog.Characters[:500]...)
	c.Catalog.RosterVersion = 2
	retireStarterGroups(&c)
	if c.Catalog.RosterVersion != 14 || len(c.Catalog.Characters) != 562 {
		t.Fatal("version two catalog did not receive Tiny pack additions")
	}
	if err := game.ValidateCharacterCatalog(c.Catalog); err != nil {
		t.Fatal(err)
	}
}

func TestClampLegacyEightCopyDeck(t *testing.T) {
	d := toSavedDeck("keep-id", game.DefaultDeck())
	d.Cards[0].Copies = 8
	d.Cards[1].Copies = 2
	p := playerData{Decks: []savedDeck{d}, SelectedID: d.ID}
	if !repairRetiredPlayer(&p) {
		t.Fatal("legacy deck was not repaired")
	}
	got := p.Decks[0]
	if got.ID != d.ID || got.Name != d.Name || p.SelectedID != d.ID || got.Cards[0].Copies != 4 || got.Cards[1].Copies != 2 {
		t.Fatal("copy migration lost deck data")
	}
	if _, err := decodeSavedDeck(got); err != nil {
		t.Fatal(err)
	}
	if repairRetiredPlayer(&p) {
		t.Fatal("copy migration is not idempotent")
	}
}

func TestStartupUpgradesVersionThreeRogueCatalog(t *testing.T) {
	c := defaultConfiguration(game.Catalog())
	c.Catalog.Characters = append([]game.CharacterDefinition{}, c.Catalog.Characters[:502]...)
	c.Catalog.RosterVersion = 3
	retireStarterGroups(&c)
	if c.Catalog.RosterVersion != 14 || len(c.Catalog.Characters) != 562 || c.Catalog.Characters[502].ID != "roster_burgundy_rogue" {
		t.Fatal("existing catalog did not receive Burgundy Rogue")
	}
	if err := game.ValidateCharacterCatalog(c.Catalog); err != nil {
		t.Fatal(err)
	}
}

func TestRetiredRogueDeckReplacedWithoutLosingDeck(t *testing.T) {
	configForTest(t)
	deck := toSavedDeck("rogue-retirement", game.DefaultDeck())
	replaced := false
	for i, card := range deck.Cards {
		kind, ok := characterKind(card.CharacterID)
		if ok && game.Character(kind).Group == 45 && game.Character(kind).Cost == 3 {
			deck.Cards[i].CharacterID = "roster_burgundy_rogue"
			replaced = true
			break
		}
	}
	if !replaced {
		t.Fatal("missing Knight cost 3 fixture")
	}
	p := playerData{SchemaVersion: 1, Decks: []savedDeck{deck}, SelectedID: deck.ID}
	if !repairRetiredPlayer(&p) {
		t.Fatal("Rogue deck not repaired")
	}
	if p.Decks[0].ID != deck.ID || p.SelectedID != deck.ID {
		t.Fatal("deck identity changed")
	}
	for _, card := range p.Decks[0].Cards {
		if card.CharacterID == "roster_burgundy_rogue" {
			t.Fatal("retired Rogue remains")
		}
	}
	if _, err := decodeSavedDeck(p.Decks[0]); err != nil {
		t.Fatal(err)
	}
	if repairRetiredPlayer(&p) {
		t.Fatal("retirement repeated")
	}
}

func TestMandatoryNeutralSurvivesRetirementMigration(t *testing.T) {
	c := defaultConfiguration(game.Catalog())
	retireStarterGroups(&c)
	found := false
	for _, ch := range c.Catalog.Characters {
		if ch.ID == "healing_totem" {
			found = true
			if !ch.Enabled {
				t.Fatal("Mandatory neutral retired")
			}
		}
	}
	if !found {
		t.Fatal("Mandatory neutral missing")
	}
}
