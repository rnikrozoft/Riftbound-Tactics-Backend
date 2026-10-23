package game

import "testing"

func TestBinaryMergesAndFlatSell(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	kind := p.Offers[0].Kind
	p.Coins = 100
	p.RemainingCopies[kind] = 8
	for i := 1; i <= 8; i++ {
		token := 100 + i
		p.Offers = []Card{{Token: token, Kind: kind, Stars: 1, Price: 2}}
		action(t, r, "a", "buy", token, 0)
		if p.Units == nil {
			t.Fatal("purchase must serialize empty field as an array")
		}
		copies := 0
		for _, c := range p.Hand {
			copies += 1 << (starCount(c.Stars) - 1)
		}
		if copies != i {
			t.Fatalf("purchase %d lost copies: %v", i, p.Hand)
		}
		if i == 3 && (len(p.Hand) != 2 || p.Hand[0].Stars != 2 || p.Hand[1].Stars != 1) {
			t.Fatalf("third copy upgraded early: %v", p.Hand)
		}
		if i == 4 && (len(p.Hand) != 1 || p.Hand[0].Stars != 3) {
			t.Fatal("four copies must yield three stars")
		}
		if i == 8 && (len(p.Hand) != 1 || p.Hand[0].Stars != 4) {
			t.Fatal("eight copies must yield four stars")
		}
	}
	before := p.Coins
	action(t, r, "a", "sell", p.Hand[0].Token, 0)
	if p.Coins != before+1 {
		t.Fatal("upgraded sale is not one coin")
	}
	p.Units = []Unit{{Token: 999, Kind: kind, Stars: 4, PurchasePrice: 48}}
	before = p.Coins
	action(t, r, "a", "sell", 999, 0)
	if p.Coins != before+1 {
		t.Fatal("field sale is not one coin")
	}
}

func TestBinaryMergeCapacityAndVeteran(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	kind := p.Offers[0].Kind
	p.Coins = 100
	p.RemainingCopies[kind] = 8
	p.Units = []Unit{{Token: 500, Kind: kind, Stars: 2, PurchasePrice: 4, Veteran: true}}
	for i := 0; i < HandLimit; i++ {
		p.Hand = append(p.Hand, Card{Token: 600 + i, Kind: kind + 1, Stars: 1, Price: 2})
	}
	p.Offers = []Card{{Token: 900, Kind: kind, Stars: 1, Price: 2}}
	revision := r.State.Revision
	if r.Apply("a", Action{Type: "buy", Token: 900, Round: r.State.Round}, 1100) == nil || p.Coins != 100 || p.RemainingCopies[kind] != 8 || r.State.Revision != revision || len(p.Units) != 1 {
		t.Fatal("rejected purchase mutated state")
	}
	p.Hand[0] = Card{Token: 501, Kind: kind, Stars: 1, Price: 2, Paid: 2}
	action(t, r, "a", "buy", 900, 0)
	if len(p.Units) != 0 || len(p.Hand) != HandLimit {
		t.Fatal("carry must free capacity before returning unit")
	}
	c := p.Hand[cardIndex(p.Hand, 500)]
	if c.Stars != 3 || !c.Veteran || c.Paid != 8 {
		t.Fatal("carry lost tier, investment or veteran flag")
	}
}
