package game

import "testing"

func TestCardBackCatalogAndDeckValidation(t *testing.T) {
	if len(CardBacks) != 20 {
		t.Fatal("expected twenty existing designs")
	}
	seen := map[string]bool{}
	for i, b := range CardBacks {
		if seen[b.ID] || b.Width != 96 || b.Height != 128 || (i > 0 && b.Price != 100) {
			t.Fatal("invalid back", b.ID)
		}
		seen[b.ID] = true
	}
	d := DefaultDeck()
	d.CardBackID = "forged"
	if ValidateDeck(d) == nil {
		t.Fatal("unknown cosmetic accepted")
	}
	d.CardBackID = "card_back_19"
	if err := ValidateDeck(d); err != nil {
		t.Fatal(err)
	}
}
