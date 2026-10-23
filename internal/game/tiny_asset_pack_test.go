package game

import "testing"

func TestTinyPackCardsFitExistingDecks(t *testing.T) {
	for _, id := range []string{"roster_bat", "roster_necromancer"} {
		found := false
		for _, card := range Catalog().Characters {
			if card.ID != id {
				continue
			}
			found = true
			if !card.Enabled || card.Combat == nil {
				t.Fatal("new card unavailable", id)
			}
			d := deckForGroups("Tiny pack", []int{45, 47, 48})
			replaced := false
			for i, entry := range d.Cards {
				if CardGroup(entry.Kind) == card.Group && CardCost(entry.Kind) == card.Cost {
					d.Cards[i].Kind = card.Kind
					replaced = true
					break
				}
			}
			if !replaced {
				t.Fatal("missing replacement slot", id)
			}
			if err := ValidateDeck(d); err != nil {
				t.Fatal(id, err)
			}
			r := New("tiny-pack", "player", 42)
			if err := r.SetDeck("player", d); err != nil {
				t.Fatal(id, err)
			}
			if r.Player("player").RemainingCopies[card.Kind] < 1 {
				t.Fatal("new card missing from shop pool", id)
			}
		}
		if !found {
			t.Fatal("missing card", id)
		}
	}
}
