package game

import "testing"

func TestFourCopyDeckLimit(t *testing.T) {
	d := DefaultDeck()
	for _, c := range Catalog().Characters {
		if c.MaxCopies > 4 {
			t.Fatal("catalog exceeds four copies", c.ID)
		}
	}
	d.Cards[0].Copies = 5
	if ValidateDeck(d) == nil {
		t.Fatal("five-copy deck accepted")
	}
	d.Cards[0].Copies = 4
	if err := ValidateDeck(d); err != nil {
		t.Fatal(err)
	}
}
