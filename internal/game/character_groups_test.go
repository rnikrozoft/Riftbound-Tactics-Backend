package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeCharacterGroups(t *testing.T) {
	c, err := LoadCharacterCatalog("../../data/characters.json")
	if err != nil {
		t.Fatal(err)
	}
	old := Catalog()
	UseCatalog(c)
	defer UseCatalog(old)
	for kind, group := range map[int]int{60: 5, 61: 6, 62: 7} {
		if Character(kind).Group != group {
			t.Fatalf("kind %d still belongs to the wrong group", kind)
		}
	}
	neutral := 0
	for _, card := range c.Characters {
		if card.Enabled && card.Group == NeutralGroup {
			neutral++
		}
	}
	if neutral != 20 {
		t.Fatal("original Neutral options changed")
	}
	if IsHeroGroup(NeutralGroup) || len(HeroGroups()) != len(c.GroupNames)-1 {
		t.Fatal("selectable character groups are wrong")
	}
	deck := deckForGroups("Monster expedition", []int{5, 6, 7})
	if err = ValidateDeck(deck); err != nil {
		t.Fatal(err)
	}
	if len(deck.Cards) != 40 {
		t.Fatal("new characters must fill a complete deck")
	}
	r := New("monsters", "player", 42)
	if err = r.SetDeck("player", deck); err != nil {
		t.Fatal(err)
	}
	if len(r.Player("player").RemainingCopies) != len(c.Characters) {
		t.Fatal("pool does not include new kinds")
	}
	for _, bot := range botDeckTemplates {
		if err = ValidateDeck(bot); err != nil {
			t.Fatal(err)
		}
	}
	n := len(HeroGroups())
	if len(botDeckTemplates) != n*(n-1)*(n-2)/6 {
		t.Fatal("bots cannot select every character combination")
	}
	deck.Heroes[0] = NeutralGroup
	if ValidateDeck(deck) == nil {
		t.Fatal("Neutral must not be selectable as a character")
	}
	deck = DefaultDeck()
	deck.Cards[0].Kind = 60
	if ValidateDeck(deck) == nil {
		t.Fatal("Orc cards cannot be used without selecting Orc")
	}
}

func TestJSONCatalogRejectsIncompleteNewCharacterGroup(t *testing.T) {
	c, err := LoadCharacterCatalog("../../data/characters.json")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Characters {
		if c.Characters[i].Group == 5 && c.Characters[i].Cost == 2 {
			c.Characters[i].Enabled = false
		}
	}
	data, _ := json.Marshal(c)
	path := filepath.Join(t.TempDir(), "characters.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadCharacterCatalog(path); err == nil {
		t.Fatal("incomplete character group accepted")
	}
}
