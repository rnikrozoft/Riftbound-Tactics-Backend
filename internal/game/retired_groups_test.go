package game

import "testing"

func TestRetiredStarterGroups(t *testing.T) {
	c, err := LoadCharacterCatalog("../../data/characters.json")
	if err != nil {
		t.Fatal(err)
	}
	old := Catalog()
	UseCatalog(c)
	defer UseCatalog(old)
	for _, group := range []int{1, 2, 3, 4} {
		if IsHeroGroup(group) {
			t.Fatal("retired hero selectable", group)
		}
		for _, card := range c.Characters {
			if card.Group == group && card.Enabled && !IsSupport(card.Kind) {
				t.Fatal("retired card enabled")
			}
		}
	}
	if err := ValidateDeck(DefaultDeck()); err != nil {
		t.Fatal(err)
	}
	d := DefaultDeck()
	d.Heroes[0] = 1
	if ValidateDeck(d) == nil {
		t.Fatal("retired deck accepted")
	}
	for _, bot := range botDeckTemplates {
		if err := ValidateDeck(bot); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCatalogRejectsDuplicateRoleAssets(t *testing.T) {
	c, err := LoadCharacterCatalog("../../data/characters.json")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Characters {
		if c.Characters[i].Group == 1 {
			c.Characters[i].Enabled = true
		}
	}
	if ValidateCharacterCatalog(c) == nil {
		t.Fatal("duplicate role can be reintroduced")
	}
}

func TestFourGroupsHaveFortyUniqueCards(t *testing.T) {
	c := Catalog()
	assets := map[string]bool{}
	groups := map[int]int{}
	for _, card := range c.Characters {
		if !card.Enabled || IsSupport(card.Kind) {
			continue
		}
		if assets[card.ScenePath] {
			t.Fatal("duplicate card asset", card.ID)
		}
		assets[card.ScenePath] = true
		groups[card.Group]++
	}
	if len(groups) != 4 || len(assets) != 42 {
		t.Fatal("wrong roster size")
	}
	for group, count := range groups {
		want := 10
		if group == 47 || group == 48 {
			want = 11
		}
		if count != want {
			t.Fatal("incomplete group")
		}
	}
	for _, sameGroup := range []bool{true, false} {
		clone := c
		clone.Characters = append([]CharacterDefinition{}, c.Characters...)
		target := 461
		if !sameGroup {
			target = 470
		}
		clone.Characters[target].ScenePath = clone.Characters[460].ScenePath
		if ValidateCharacterCatalog(clone) == nil {
			t.Fatal("duplicate asset accepted", sameGroup)
		}
	}
}
