package game

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultDeckAndCatalog(t *testing.T) {
	d := DefaultDeck()
	if err := ValidateDeck(d); err != nil {
		t.Fatal(err)
	}
	if len(d.Cards) != 40 {
		t.Fatal("forty distinct slots")
	}
	counts := map[[2]int]int{}
	for k := 0; k < CardKinds; k++ {
		counts[[2]int{CardGroup(k), CardCost(k)}]++
	}
	for g := 0; g <= 4; g++ {
		for cost := 2; cost <= 6; cost++ {
			want := 2
			if g == 4 {
				want = 4
			}
			if counts[[2]int{g, cost}] != want {
				t.Fatal("catalog grouping", g, cost)
			}
		}
	}
}
func TestInvalidDecksRejected(t *testing.T) {
	cases := []func(*Deck){func(d *Deck) { d.Cards = d.Cards[:39] }, func(d *Deck) { d.Heroes[1] = d.Heroes[0] }, func(d *Deck) { d.Cards[0].Kind = d.Cards[1].Kind }, func(d *Deck) { d.Cards[0].Copies = 0 }, func(d *Deck) { d.Cards[0].Copies = 5 }, func(d *Deck) { d.Cards[0].Kind = 60 }, func(d *Deck) { d.Cards[0].Kind = 30 }, func(d *Deck) { d.Name = "" }, func(d *Deck) { d.Name = strings.Repeat("x", 41) }, func(d *Deck) { d.Cards[0].Kind = 42; d.Cards[1].Kind = 43 }}
	for i, change := range cases {
		d := DefaultDeck()
		change(&d)
		if ValidateDeck(d) == nil {
			t.Fatal("invalid accepted", i)
		}
	}
}
func TestSelectedDeckPoolAndCopyExhaustion(t *testing.T) {
	r := New("123456", "a", 42)
	d := DefaultDeck()
	d.Name = "Guardian solo copies"
	d.Heroes[0] = 3
	cards := []DeckCard{}
	for _, c := range d.Cards {
		if CardGroup(c.Kind) != 0 {
			c.Copies = 1
			cards = append(cards, c)
		}
	}
	for k := 30; k < 40; k++ {
		cards = append(cards, DeckCard{Kind: k, Copies: 1})
	}
	d.Cards = cards
	if err := r.SetDeck("a", d); err != nil {
		t.Fatal(err)
	}
	r.Reserve("b")
	r.Connect("a", 1000)
	r.Connect("b", 1000)
	p := r.Player("a")
	p.Coins = 100
	p.ShopLevel = 6
	if err := r.SetDeck("a", DefaultDeck()); err == nil {
		t.Fatal("changed deck during preparation")
	}
	seen := map[int]bool{}
	for _, c := range d.Cards {
		seen[c.Kind] = true
		if p.RemainingCopies[c.Kind] != 1 {
			t.Fatal("wrong copies")
		}
	}
	for k, n := range p.RemainingCopies {
		if !seen[k] && n != 0 {
			t.Fatal("unselected kind stocked")
		}
	}
	r.rollShop(p)
	card := p.Offers[0]
	action(t, r, "a", "buy", card.Token, 0)
	if p.RemainingCopies[card.Kind] != 0 {
		t.Fatal("purchase did not consume last copy")
	}
	for i := 0; i < 20; i++ {
		r.rollShop(p)
		for _, c := range p.Offers {
			if !seen[c.Kind] || c.Kind == card.Kind {
				t.Fatal("unselected or exhausted card offered")
			}
		}
	}
	data, _ := json.Marshal(p)
	if strings.Contains(string(data), "remaining") {
		t.Fatal("private pool leaked")
	}
}
