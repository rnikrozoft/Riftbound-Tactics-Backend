package game

import "testing"

func fillPurchasedHand(p *Player, kind, count int) {
	p.Hand = nil
	for i := 0; i < count; i++ {
		p.Hand = append(p.Hand, Card{Token: 10000 + i, Kind: kind, Stars: 1, Price: CardCost(kind)})
	}
}

func TestGeneratedHandOverflowAndShopCapacity(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	kind := SupportKinds()[0] // Supports never merge, isolating capacity from upgrades.
	p.Coins = 100
	fillPurchasedHand(p, kind, HandLimit-1)
	pool, coins := p.RemainingCopies[kind], p.Coins
	for i := 0; i < 16; i++ {
		if err := r.GrantGeneratedCard("a", kind); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.Hand) != 25 || purchasedHandCount(p.Hand) != 9 || p.Coins != coins || p.RemainingCopies[kind] != pool {
		t.Fatal("generation consumed capacity, coins, pool copies or merged cards")
	}
	p.RemainingCopies[kind] = 2
	r.serial++
	token := r.serial
	p.Offers = []Card{{Token: token, Kind: kind, Price: CardCost(kind)}}
	action(t, r, "a", "buy", token, 0)
	if purchasedHandCount(p.Hand) != 10 || len(p.Hand) != 26 {
		t.Fatal("generated cards blocked the tenth purchase")
	}
	r.serial++
	token = r.serial
	p.Offers = []Card{{Token: token, Kind: kind, Price: CardCost(kind)}}
	coins, revision := p.Coins, r.State.Revision
	if err := r.Apply("a", Action{Type: "buy", Token: token, Round: r.State.Round}, 1100); err == nil {
		t.Fatal("eleventh purchased card was accepted")
	}
	if p.Coins != coins || r.State.Revision != revision || len(p.Hand) != 26 {
		t.Fatal("rejected purchase mutated state")
	}
}

func TestGeneratedCardKeepsOriginAcrossDeploymentAndReturn(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	kind := DefaultDeck().Cards[0].Kind
	fillPurchasedHand(p, SupportKinds()[0], HandLimit)
	if err := r.GrantGeneratedCard("a", kind); err != nil {
		t.Fatal(err)
	}
	token := p.Hand[len(p.Hand)-1].Token
	action(t, r, "a", "deploy", token, 0)
	if !p.Units[0].Generated {
		t.Fatal("deploy lost generated origin")
	}
	action(t, r, "a", "return", token, 0)
	if len(p.Hand) != 11 || !p.Hand[len(p.Hand)-1].Generated {
		t.Fatal("generated return was capped or lost origin")
	}
	p.Units = append(p.Units, Unit{Token: 20000, Kind: kind, Stars: 1, Slot: 1})
	if err := r.Apply("a", Action{Type: "return", Token: 20000, Round: r.State.Round}, 1100); err == nil {
		t.Fatal("purchased return bypassed shop hand capacity")
	}
}

func TestPurchaseMergingGeneratedCardCountsAsPurchased(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	kind := DefaultDeck().Cards[0].Kind
	p.Coins = 100
	if err := r.GrantGeneratedCard("a", kind); err != nil {
		t.Fatal(err)
	}
	r.serial++
	token := r.serial
	p.Offers = []Card{{Token: token, Kind: kind, Price: CardCost(kind)}}
	action(t, r, "a", "buy", token, 0)
	if len(p.Hand) != 1 || p.Hand[0].Generated || p.Hand[0].Stars != 2 || purchasedHandCount(p.Hand) != 1 {
		t.Fatal("purchase upgrade bypassed capacity or changed merge rules")
	}
}

func TestGeneratedCardCannotBeRequestedByPlayerAction(t *testing.T) {
	r := setup(t)
	revision := r.State.Revision
	if err := r.Apply("a", Action{Type: "generate", Round: r.State.Round}, 1100); err == nil {
		t.Fatal("client could generate cards")
	}
	if err := r.GrantGeneratedCard("missing", 0); err == nil {
		t.Fatal("unknown recipient accepted")
	}
	if err := r.GrantGeneratedCard("a", CardKinds); err == nil {
		t.Fatal("invalid card kind accepted")
	}
	if r.State.Revision != revision {
		t.Fatal("rejected generation mutated revision")
	}
}
