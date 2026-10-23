package game

import "testing"

func TestMandatoryTotemPurchaseAndSupportSlots(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	kind := SupportKind()
	if kind < 0 || p.RemainingCopies[kind] != 1 {
		t.Fatal("Every player must own the mandatory neutral pool")
	}
	var token int
	for _, c := range p.Offers {
		if c.Kind == kind {
			token = c.Token
		}
	}
	if token == 0 {
		t.Fatal("Totem must be offered for the trial")
	}
	coins := p.Coins
	action(t, r, "a", "buy", token, 0)
	if p.Coins != coins-2 || cardIndex(p.Hand, token) < 0 {
		t.Fatal("Purchase did not enter hand or charge coins")
	}
	if r.Apply("a", Action{Type: "deploy", Token: token, Slot: 0, Round: r.State.Round}, 1100) == nil {
		t.Fatal("Support accepted in hero slot")
	}
	action(t, r, "a", "deploy", token, 8)
	if len(p.Units) != 1 || p.Units[0].Slot != 8 {
		t.Fatal("Support placement failed")
	}
	if r.Apply("a", Action{Type: "move", Token: token, Slot: 1, Round: r.State.Round}, 1100) == nil {
		t.Fatal("Support moved into hero row")
	}
	action(t, r, "a", "move", token, 10)
	r.rollShop(p)
	for _, c := range p.Offers {
		if c.Kind == kind {
			t.Fatal("Purchased unique support offered again")
		}
	}
}

func TestTotemHealsAlliesAfterEitherTeamsTurn(t *testing.T) {
	f := fixtureFight(basic("ranged"), basic("ranged"))
	a, b := f.units[0], f.units[1]
	support := &fighter{unit: CombatUnit{ID: "A:totem", Team: "A", Kind: SupportKind(), Slot: 8, MaxHP: 1}, hp: 1, side: 0}
	ally := &fighter{unit: CombatUnit{ID: "A:ally", Team: "A", Kind: 0, Slot: 4, MaxHP: 20}, hp: 10, side: 0, rules: basic("ranged")}
	f.units = append(f.units, support, ally)
	a.hp = 50
	b.hp = 80
	a.stun = true
	f.action(a, b, 0)
	if a.hp != 51 || ally.hp != 11 || b.hp != 80 {
		t.Fatalf("Wrong allied healing: %d %d %d", a.hp, ally.hp, b.hp)
	}
	b.stun = true
	f.action(b, a, 1)
	if a.hp != 52 || ally.hp != 12 {
		t.Fatal("Totem did not heal during enemy turn")
	}
	ally.hp = 20
	a.antiHeal = true
	b.stun = true
	f.action(b, a, 2)
	if a.hp != 52 || ally.hp != 20 {
		t.Fatal("Healing must respect max HP and anti-heal")
	}
	for _, u := range f.alive(0) {
		if IsSupport(u.unit.Kind) {
			t.Fatal("Totem must not be an attack target or count as survivor")
		}
	}
	if support.hp != 1 {
		t.Fatal("Totem took damage")
	}
}

func TestRankedLeagueMandatoryNeutralPool(t *testing.T) {
	l, err := NewLeague([]Seat{{ID: "a", Deck: DefaultDeck()}, {ID: "b", Deck: DefaultDeck()}}, 19)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range l.Players {
		if p.RemainingCopies[SupportKind()] != 1 {
			t.Fatal("League reset mandatory support pool")
		}
	}
}

func TestSupportAloneCannotFightOrWin(t *testing.T) {
	r := setup(t)
	r.Player("a").Units = []Unit{{Kind: SupportKind(), Token: 900, Slot: 6, Stars: 1}}
	r.Player("b").Units = []Unit{}
	plan := r.startEffects(0)
	if len(plan.Events) != 0 || plan.Winner != "DRAW" {
		t.Fatal("Support-only board must not attack or win")
	}
}
