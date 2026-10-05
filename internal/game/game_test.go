package game

import (
	"reflect"
	"testing"
)

func setup(t *testing.T) *Room {
	r := New("123456", "a", 42)
	if !r.Reserve("b") {
		t.Fatal("reserve")
	}
	r.Connect("a", 1000)
	r.Connect("b", 1000)
	return r
}
func action(t *testing.T, r *Room, id, kind string, token, slot int) {
	t.Helper()
	if err := r.Apply(id, Action{Type: kind, Token: token, Slot: slot, Round: r.State.Round}, 1100); err != nil {
		t.Fatal(err)
	}
}
func deploy(t *testing.T, r *Room, id string, slot int) int {
	token := r.Player(id).Offers[0].Token
	action(t, r, id, "buy", token, 0)
	action(t, r, id, "deploy", token, slot)
	return token
}
func TestRoomAndDeadline(t *testing.T) {
	r := setup(t)
	if r.State.DeadlineMS != 61000 || r.State.Phase != "preparation" {
		t.Fatal("60-second deadline")
	}
	if r.Reserve("third") {
		t.Fatal("third player accepted")
	}
	if r.ShouldStart(60999) || !r.ShouldStart(61000) {
		t.Fatal("timeout")
	}
	if err := r.Apply("a", Action{Type: "ready", Round: 1}, 61000); err == nil {
		t.Fatal("late edit accepted")
	}
	plan := r.Start(61000)
	if plan.Winner != "DRAW" || len(plan.Events) != 0 {
		t.Fatal("empty teams")
	}
}
func TestOwnershipSwapVeteran(t *testing.T) {
	r := setup(t)
	a := deploy(t, r, "a", 0)
	b := deploy(t, r, "a", 1)
	if err := r.Apply("b", Action{Type: "move", Token: a, Slot: 2, Round: 1}, 1100); err == nil {
		t.Fatal("opponent edit")
	}
	action(t, r, "a", "move", a, 1)
	if r.Player("a").Units[0].Slot != 1 || r.Player("a").Units[1].Slot != 0 {
		t.Fatal("swap")
	}
	action(t, r, "a", "return", b, 0)
	action(t, r, "a", "deploy", b, 0)
	deploy(t, r, "b", 0)
	action(t, r, "a", "ready", 0, 0)
	if r.ShouldStart(1100) {
		t.Fatal("only one ready")
	}
	if err := r.Apply("a", Action{Type: "move", Token: a, Slot: 2, Round: 1}, 1100); err != nil {
		t.Fatal("ready move must remain allowed")
	}
	action(t, r, "b", "ready", 0, 0)
	if !r.ShouldStart(1100) {
		t.Fatal("both ready")
	}
	plan := r.Start(1100)
	if len(plan.Events) == 0 || plan.Winner == "DRAW" {
		t.Fatal("combat")
	}
	r.Finish(plan.EndMS)
	action(t, r, "a", "next", 0, 0)
	action(t, r, "b", "next", 0, 0)
	if r.State.Round != 2 || len(r.Player("a").Units) != 2 {
		t.Fatal("round persistence")
	}
	if err := r.Apply("a", Action{Type: "return", Token: a, Round: 2}, 1100); err == nil {
		t.Fatal("veteran returned")
	}
	action(t, r, "a", "sell", a, 0)
	if len(r.Player("a").Units) != 1 {
		t.Fatal("veteran sale")
	}
}
func TestPlanDeterministicAndValid(t *testing.T) {
	makePlan := func() *Plan {
		r := setup(t)
		r.Player("a").Coins = 30
		r.Player("b").Coins = 30
		// Distinct fixtures isolate battle determinism from shop RNG and duplicate upgrades.
		for i := 0; i < 4; i++ {
			for _, id := range []string{"a", "b"} {
				r.serial++
				r.Player(id).Offers = []Card{{Token: r.serial, Kind: i, Price: 2}}
				deploy(t, r, id, i)
			}
		}
		return r.Start(1100)
	}
	a, b := makePlan(), makePlan()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed differs")
	}
	hp := map[string]int{}
	team := map[string]string{}
	for _, u := range a.Units {
		hp[u.ID] = 100
		team[u.ID] = u.Team
	}
	previous := "B"
	for i, e := range a.Events {
		if hp[e.Attacker] <= 0 || hp[e.Target] <= 0 || team[e.Attacker] == team[e.Target] {
			t.Fatal("invalid attacker/target")
		}
		if team[e.Attacker] == previous {
			t.Fatal("not alternating")
		}
		previous = team[e.Attacker]
		hp[e.Target] -= e.Damage
		if hp[e.Target] < 0 {
			hp[e.Target] = 0
		}
		if hp[e.Target] != e.TargetHP || e.Dead != (e.TargetHP == 0) || e.Index != i || e.AtMS != a.StartMS+int64(i)*StepMS {
			t.Fatal("invalid plan")
		}
	}
}
func TestLimitsAndReplay(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	p.Coins = 100 // Capacity checks are independent of affordability.
	p.RemainingCopies[0] = 20
	seen := map[int]bool{}
	for _, c := range p.Offers {
		if seen[c.Token] || c.Kind < 0 || c.Kind >= CardKinds {
			t.Fatal("invalid offer or duplicate token")
		}
		seen[c.Token] = true
	}
	token := p.Offers[0].Token
	action(t, r, "a", "buy", token, 0)
	if err := r.Apply("a", Action{Type: "buy", Token: token, Round: 1}, 1100); err == nil {
		t.Fatal("double buy")
	}
	if err := r.Apply("a", Action{Type: "deploy", Token: token, Slot: 6, Round: 1}, 1100); err == nil {
		t.Fatal("invalid slot")
	}
	if err := r.Apply("a", Action{Type: "deploy", Token: token, Slot: 0, Round: 0}, 1100); err == nil {
		t.Fatal("stale round")
	}
	for len(p.Hand) < 10 {
		if len(p.Offers) == 0 {
			r.serial++
			p.Offers = append(p.Offers, Card{Token: r.serial, Kind: 6 + len(p.Hand)})
		}
		action(t, r, "a", "buy", p.Offers[0].Token, 0)
	}
	r.serial++
	p.Offers = append(p.Offers, Card{Token: r.serial, Kind: 6 + len(p.Hand)})
	if err := r.Apply("a", Action{Type: "buy", Token: r.serial, Round: 1}, 1100); err == nil {
		t.Fatal("hand overflow")
	}
	for i := 0; i < 6; i++ {
		action(t, r, "a", "deploy", p.Hand[0].Token, i)
	}
	if err := r.Apply("a", Action{Type: "deploy", Token: p.Hand[0].Token, Slot: 0, Round: 1}, 1100); err == nil {
		t.Fatal("field overflow")
	}
}

func TestEconomyAffordabilityAndRefund(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	if p.Coins != StartingCoins || p.ShopLevel != 2 || len(p.Offers) != 3 {
		t.Fatal("initial economy")
	}
	token := p.Offers[0].Token
	p.Coins = 1
	revision := r.State.Revision
	if r.Apply("a", Action{Type: "buy", Token: token, Round: 1}, 1100) == nil || p.Coins != 1 || len(p.Hand) != 0 || r.State.Revision != revision {
		t.Fatal("unaffordable buy")
	}
	p.Coins = 20
	p.Offers[0].Price = 5
	action(t, r, "a", "buy", token, 0)
	action(t, r, "a", "deploy", token, 0)
	action(t, r, "a", "return", token, 0)
	action(t, r, "a", "deploy", token, 0)
	action(t, r, "a", "sell", token, 0)
	if p.Coins != 17 {
		t.Fatal("purchase price 5 must refund floor(5/2)=2")
	}
	if r.Apply("a", Action{Type: "sell", Token: token, Round: 1}, 1100) == nil || p.Coins != 17 {
		t.Fatal("double refund")
	}
	token = p.Offers[0].Token
	action(t, r, "a", "buy", token, 0)
	action(t, r, "a", "sell", token, 0)
	if p.Coins != 16 {
		t.Fatal("hand refund")
	}
}
func TestEconomyLockRerollAndIncome(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	action(t, r, "a", "buy", p.Offers[0].Token, 0)
	action(t, r, "a", "lock", 0, 0)
	saved := append([]Card(nil), p.Offers...)
	coins := p.Coins
	r.Prepare(1200)
	if !reflect.DeepEqual(saved, p.Offers[:len(saved)]) || len(p.Offers) != 3 || !p.ShopLocked || p.Coins != coins+6 {
		t.Fatal("locked refill or income")
	}
	action(t, r, "a", "reroll", 0, 0)
	if p.ShopLocked || p.Coins != coins+6-2 || len(p.Offers) != 3 || p.Offers[0].Token == saved[0].Token {
		t.Fatal("paid reroll")
	}
	p.Coins = 1
	p.ShopLocked = true
	saved = append([]Card(nil), p.Offers...)
	if r.Apply("a", Action{Type: "reroll", Round: 2}, 1300) == nil || !reflect.DeepEqual(saved, p.Offers) || p.Coins != 1 || !p.ShopLocked {
		t.Fatal("unaffordable reroll")
	}
	action(t, r, "a", "ready", 0, 0)
	if r.Apply("a", Action{Type: "upgrade", Round: 2}, 1300) != nil {
		t.Fatal("free upgrade while waiting must remain allowed")
	}
}
func TestEconomyBattleDoesNotChangeCoins(t *testing.T) {
	r := setup(t)
	deploy(t, r, "a", 0)
	a, b := r.Player("a").Coins, r.Player("b").Coins
	plan := r.Start(1100)
	if plan.Winner != "A" || r.Finish(plan.EndMS-1) {
		t.Fatal("early reward")
	}
	if !r.Finish(plan.EndMS) || r.Player("a").Coins != a || r.Player("b").Coins != b {
		t.Fatal("battle must not change either balance")
	}
	if r.Finish(plan.EndMS+1) || r.Player("a").Coins != a {
		t.Fatal("repeated finish must not change coins")
	}
	draw := setup(t)
	plan = draw.Start(1100)
	draw.Finish(plan.EndMS)
	if draw.Player("a").Coins != StartingCoins || draw.Player("b").Coins != StartingCoins {
		t.Fatal("draw reward")
	}
}
func TestShopUpgradeDiscountResetAndCap(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	if p.UpgradeCost != 2 {
		t.Fatal("initial upgrade")
	}
	r.Prepare(1200)
	if p.UpgradeCost != 0 || p.Coins != 10 {
		t.Fatal("round two free upgrade")
	}
	saved := append([]Card(nil), p.Offers...)
	action(t, r, "a", "upgrade", 0, 0)
	if p.ShopLevel != 3 || p.UpgradeCost != 8 || p.Coins != 10 || len(p.Offers) != 3 || !reflect.DeepEqual(saved, p.Offers) {
		t.Fatal("free upgrade reset or preserved offers")
	}
	r.Prepare(1400)
	if p.UpgradeCost != 6 || p.Coins != 19 {
		t.Fatal("third round upgrade discount")
	}
	action(t, r, "a", "upgrade", 0, 0)
	if p.ShopLevel != 4 || p.Coins != 13 || p.UpgradeCost != 10 {
		t.Fatal("level four")
	}
	action(t, r, "a", "upgrade", 0, 0)
	if p.ShopLevel != 5 || p.Coins != 3 || p.UpgradeCost != 14 || len(p.Offers) != 4 {
		t.Fatal("same-round upgrade cost")
	}
	before := r.State.Revision
	if r.Apply("a", Action{Type: "upgrade", Round: 3}, 1500) == nil || p.Coins != 3 || p.ShopLevel != 5 || r.State.Revision != before {
		t.Fatal("unaffordable upgrade")
	}
	p.Coins = 100
	action(t, r, "a", "upgrade", 0, 0)
	if p.ShopLevel != 6 || len(p.Offers) != 4 || p.UpgradeCost != 0 || p.Coins != 86 {
		t.Fatal("max level")
	}
	if r.Apply("a", Action{Type: "upgrade", Round: 3}, 1500) == nil || p.Coins != 86 {
		t.Fatal("upgrade beyond cap")
	}
	for i := 0; i < 10; i++ {
		r.Prepare(2000 + int64(i))
	}
	if p.UpgradeCost != 0 {
		t.Fatal("discount below zero")
	}
	incomes := []int{4, 6, 9, 12, 15, 18, 20, 20}
	for i, income := range incomes {
		if RoundIncome(i+1) != income {
			t.Fatal("income table")
		}
	}
}
func TestShopPoolEligibilityUniqueAndExhausted(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	p.Coins = 10000
	for level := 2; level <= 6; level++ {
		p.ShopLevel = level
		seenHigh := false
		for roll := 0; roll < 100; roll++ {
			r.rollShop(p)
			seen := map[int]bool{}
			for _, c := range p.Offers {
				if c.Price > level || c.Price != CardCost(c.Kind) || seen[c.Kind] {
					t.Fatal("invalid cost or duplicate kind")
				}
				seen[c.Kind] = true
				if c.Price == level {
					seenHigh = true
				}
			}
		}
		if !seenHigh {
			t.Fatal("new cost tier not available")
		}
	}
	p.ShopLevel = 2
	for i := range p.RemainingCopies {
		p.RemainingCopies[i] = 0
	}
	p.RemainingCopies[0] = 1
	r.rollShop(p)
	if len(p.Offers) != 1 || p.Offers[0].Kind != 0 {
		t.Fatal("exhausted pool draw")
	}
	token := p.Offers[0].Token
	action(t, r, "a", "buy", token, 0)
	action(t, r, "a", "sell", token, 0)
	r.rollShop(p)
	if len(p.Offers) != 0 {
		t.Fatal("sold card returned to pool")
	}
}

func TestUpgradeWaitsForReroll(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	p.Coins = 20
	saved := append([]Card(nil), p.Offers...)
	action(t, r, "a", "upgrade", 0, 0)
	if !reflect.DeepEqual(saved, p.Offers) {
		t.Fatal("upgrade changed offers")
	}
	action(t, r, "a", "reroll", 0, 0)
	if len(p.Offers) != 4 || p.Coins != 16 {
		t.Fatal("reroll did not use upgraded capacity and cost")
	}
	for _, offer := range p.Offers {
		if offer.Price > 3 {
			t.Fatal("offer above shop level")
		}
	}
}

func TestCoinCarryAcrossRoundsWithoutPurchases(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	if p.Coins != 4 {
		t.Fatal("round one income")
	}
	r.Prepare(1200)
	if p.Coins != 10 {
		t.Fatal("unspent four plus six")
	}
	r.Prepare(1400)
	if p.Coins != 19 {
		t.Fatal("unspent ten plus nine")
	}
}
func TestPlayerHealthUsesWinningShopAndSurvivingStars(t *testing.T) {
	r := setup(t)
	a, b := r.Player("a"), r.Player("b")
	if a.HP != 30 || b.HP != 30 {
		t.Fatal("initial health")
	}
	a.ShopLevel = 5
	b.ShopLevel = 2
	a.Units = []Unit{{Token: 1, Slot: 0, Stars: 1}, {Token: 2, Slot: 1, Stars: 4}}
	b.Units = []Unit{{Token: 3, Slot: 0, Stars: 2}}
	plan := r.Start(1200)
	alive := map[string]bool{}
	for _, u := range plan.Units {
		alive[u.ID] = true
	}
	for _, e := range plan.Events {
		if e.Dead {
			alive[e.Target] = false
		}
	}
	winner, loser := a, b
	if plan.Winner == "B" {
		winner, loser = b, a
	}
	damage := winner.ShopLevel
	for _, u := range plan.Units {
		if u.Team == winner.Team && alive[u.ID] {
			damage += u.Stars
		}
	}
	if plan.PlayerDamage != damage {
		t.Fatalf("damage got %d want %d", plan.PlayerDamage, damage)
	}
	if r.Finish(plan.EndMS-1) || loser.HP != 30 {
		t.Fatal("damage before battle ends")
	}
	r.Finish(plan.EndMS)
	if loser.HP != 30-damage || winner.HP != 30 || loser.LastDamage != damage {
		t.Fatal("wrong player health")
	}
	r.Finish(plan.EndMS + 1)
	if loser.HP != 30-damage {
		t.Fatal("damage applied twice")
	}
	r.Prepare(plan.EndMS + 2)
	if loser.HP != 30-damage {
		t.Fatal("round reset player health")
	}
}
func TestPlayerEliminationEndsMatch(t *testing.T) {
	r := setup(t)
	a, b := r.Player("a"), r.Player("b")
	a.ShopLevel = 6
	a.Units = []Unit{{Token: 1, Slot: 0, Stars: 4}, {Token: 2, Slot: 1, Stars: 2}}
	b.HP = 3
	plan := r.Start(1200)
	if plan.PlayerDamage != 12 {
		t.Fatal("shop six plus surviving six stars")
	}
	r.Finish(plan.EndMS)
	if b.HP != 0 || a.HP != 30 || r.State.Phase != "game_over" || r.State.Winner != "A" {
		t.Fatal("terminal state")
	}
	revision, round, coins := r.State.Revision, r.State.Round, a.Coins
	for _, kind := range []string{"next", "ready", "buy", "upgrade", "reroll", "lock"} {
		if r.Apply("a", Action{Type: kind, Round: round}, plan.EndMS+1) == nil {
			t.Fatal("action after match end")
		}
	}
	r.Prepare(plan.EndMS + 2)
	if r.State.Revision != revision || r.State.Round != round || a.Coins != coins {
		t.Fatal("game over must not grant next income")
	}
}
func TestDrawDoesNotDamagePlayers(t *testing.T) {
	r := setup(t)
	plan := r.Start(1200)
	r.Finish(plan.EndMS)
	if plan.Winner != "DRAW" || plan.PlayerDamage != 0 || r.Player("a").HP != 30 || r.Player("b").HP != 30 {
		t.Fatal("draw damage")
	}
}

func TestWinningPlayerBUsesOwnShopAndDefaultStar(t *testing.T) {
	r := setup(t)
	a, b := r.Player("a"), r.Player("b")
	a.ShopLevel = 6
	b.ShopLevel = 3
	b.Units = []Unit{{Token: 1, Slot: 0}}
	plan := r.Start(1200)
	if plan.Winner != "B" || plan.PlayerDamage != 4 {
		t.Fatal("B shop plus default one star")
	}
	r.Finish(plan.EndMS)
	if a.HP != 26 || b.HP != 30 {
		t.Fatal("wrong losing side")
	}
}

func TestDuplicatePurchasesUpgradeHandAndDeployedCharacter(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	p.Coins = 100
	p.Offers = []Card{{Token: 101, Kind: 0, Price: 2}, {Token: 102, Kind: 0, Price: 2}, {Token: 103, Kind: 0, Price: 2}, {Token: 104, Kind: 0, Price: 2}}
	action(t, r, "a", "buy", 101, 0)
	action(t, r, "a", "buy", 102, 0)
	if len(p.Hand) != 1 || p.Hand[0].Stars != 2 || p.Hand[0].Token != 101 || p.Hand[0].Paid != 4 {
		t.Fatal("hand merge")
	}
	action(t, r, "a", "deploy", 101, 2)
	action(t, r, "a", "buy", 103, 0)
	action(t, r, "a", "buy", 104, 0)
	if len(p.Units) != 0 || len(p.Hand) != 1 || p.Hand[0].Stars != 4 || p.Hand[0].Paid != 8 || p.Coins != 92 {
		t.Fatal("field merge")
	}
	p.Offers = append(p.Offers, Card{Token: 105, Kind: 0, Price: 2})
	before := r.State.Revision
	if r.Apply("a", Action{Type: "buy", Token: 105, Round: 1}, 1100) == nil || p.Coins != 92 || r.State.Revision != before {
		t.Fatal("four-star cap must not spend or mutate")
	}
	action(t, r, "a", "deploy", 101, 2)
	action(t, r, "a", "return", 101, 0)
	if p.Hand[0].Stars != 4 || p.Hand[0].Paid != 8 || p.Hand[0].Price != 2 {
		t.Fatal("return lost upgrade or price")
	}
	action(t, r, "a", "sell", 101, 0)
	if p.Coins != 96 {
		t.Fatal("refund half total purchase investment")
	}
}
func TestUpgradeAllowedWithFullHandAndField(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	p.Coins = 100
	for i := 0; i < 10; i++ {
		p.Hand = append(p.Hand, Card{Token: 100 + i, Kind: i, Stars: 1, Price: 2, Paid: 2})
	}
	p.Offers = []Card{{Token: 201, Kind: 0, Price: 2}}
	action(t, r, "a", "buy", 201, 0)
	if len(p.Hand) != 10 || p.Hand[0].Stars != 2 {
		t.Fatal("full hand upgrade")
	}
	p.Hand = nil
	for i := 0; i < 6; i++ {
		p.Units = append(p.Units, Unit{Token: 300 + i, Kind: i, Stars: 1, Slot: i, PurchasePrice: 2, Veteran: true})
	}
	p.Offers = []Card{{Token: 202, Kind: 0, Price: 2}}
	action(t, r, "a", "buy", 202, 0)
	if len(p.Units) != 5 || len(p.Hand) != 1 || p.Hand[0].Stars != 2 || !p.Hand[0].Veteran {
		t.Fatal("full field veteran upgrade")
	}
}
func TestUpgradedCombatStatsAndDamage(t *testing.T) {
	r := setup(t)
	a, b := r.Player("a"), r.Player("b")
	a.Units = []Unit{{Token: 1, Slot: 0, Stars: 4}}
	b.Units = []Unit{{Token: 2, Slot: 0, Stars: 1}}
	plan := r.Start(1200)
	if plan.Units[0].MaxHP != 400 || plan.Units[0].Attack != 120 || plan.Units[0].Speed != 16 {
		t.Fatal("four-star stats")
	}
	first := plan.Events[0]
	if first.Damage < 100 || first.Damage > 160 {
		t.Fatal("attack upgrade did not affect damage")
	}
	if len(plan.Events) > 1 && plan.Events[1].AtMS-first.AtMS != 1500 {
		t.Fatal("speed did not affect turn time")
	}
	if plan.Winner == "A" && plan.PlayerDamage != a.ShopLevel+4 {
		t.Fatal("upgraded survivor stars in player damage")
	}
}

func TestReadyPlayerCanEditFieldUntilBattle(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	p.Coins = 100
	for i := 0; i < FieldLimit; i++ {
		p.Hand = append(p.Hand, Card{Token: 100 + i, Kind: i, Price: 2, Stars: 1})
	}
	for slot := 0; slot < FieldLimit; slot++ {
		action(t, r, "a", "deploy", 100+slot, slot)
	}
	if len(p.Units) != 6 {
		t.Fatal("field must fit six")
	}
	action(t, r, "a", "ready", 0, 0)
	if r.ShouldStart(1100) {
		t.Fatal("opponent not ready")
	}
	revision := r.State.Revision
	if r.Apply("a", Action{Type: "return", Token: 100, Round: 1}, 1100) == nil || len(p.Hand) != 0 || r.State.Revision != revision {
		t.Fatal("ready player returned unit")
	}
	action(t, r, "a", "move", 100, 5)
	if p.Units[0].Slot != 5 || p.Units[5].Slot != 0 {
		t.Fatal("ready swap")
	}
	action(t, r, "a", "sell", 100, 0)
	if len(p.Units) != 5 || !p.Ready {
		t.Fatal("ready sale")
	}
	p.Offers = []Card{{Token: 200, Kind: 6, Price: 3}}
	action(t, r, "a", "buy", 200, 0)
	action(t, r, "a", "deploy", 200, 5)
	if len(p.Units) != 6 || !p.Ready {
		t.Fatal("ready buy/deploy vacant sixth slot")
	}
	if r.Apply("a", Action{Type: "return", Token: 200, Round: 1}, 1100) == nil {
		t.Fatal("newly deployed unit returned after ready")
	}
	action(t, r, "b", "ready", 0, 0)
	if !r.ShouldStart(1100) {
		t.Fatal("both ready")
	}
	r.Start(1200)
	for _, kind := range []string{"move", "sell", "deploy", "return", "buy"} {
		if r.Apply("a", Action{Type: kind, Token: 200, Slot: 1, Round: 1}, 1300) == nil {
			t.Fatal("battle edit allowed")
		}
	}
}

func TestFieldUpgradeReturnsAutomaticallyAfterReadyButRejectsFullHand(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	p.Coins = 100
	p.Units = []Unit{{Token: 100, Kind: 0, Slot: 5, Stars: 1, Veteran: true, PurchasePrice: 2}}
	p.Offers = []Card{{Token: 200, Kind: 0, Price: 2}}
	for i := 0; i < 10; i++ {
		p.Hand = append(p.Hand, Card{Token: 300 + i, Kind: i + 1, Stars: 1})
	}
	p.Ready = true
	revision, remaining := r.State.Revision, p.RemainingCopies[0]
	if r.Apply("a", Action{Type: "buy", Token: 200, Round: 1}, 1100) == nil || p.Coins != 100 || r.State.Revision != revision || p.RemainingCopies[0] != remaining || len(p.Units) != 1 {
		t.Fatal("full hand upgrade must not mutate")
	}
	p.Hand = p.Hand[:9]
	action(t, r, "a", "buy", 200, 0)
	card := p.Hand[9]
	if len(p.Units) != 0 || len(p.Hand) != 10 || card.Token != 100 || card.Stars != 2 || !card.Veteran || !p.Ready {
		t.Fatal("upgrade must auto-return even after ready")
	}
	action(t, r, "a", "deploy", 100, 5)
	if p.Units[0].Stars != 2 || !p.Units[0].Veteran {
		t.Fatal("redeploy must preserve veteran")
	}
	if r.Apply("a", Action{Type: "return", Token: 100, Round: 1}, 1100) == nil {
		t.Fatal("manual return still forbidden")
	}
}

func TestEmptyLockedShopUnlocksOnNextRound(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	p.Coins = 6
	action(t, r, "a", "lock", 0, 0)
	for len(p.Offers) > 0 {
		action(t, r, "a", "buy", p.Offers[0].Token, 0)
	}
	if !p.ShopLocked {
		t.Fatal("empty shop unlocked before next round")
	}
	coins := p.Coins
	r.Prepare(1200)
	if p.ShopLocked || len(p.Offers) != 3 || p.Coins != coins+RoundIncome(2) {
		t.Fatal("empty locked shop must unlock and refill without charging on next round")
	}
}

func TestLockedShopOnlyPreservesOriginallyLockedOffers(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	action(t, r, "a", "buy", p.Offers[0].Token, 0)
	action(t, r, "a", "lock", 0, 0)
	locked := append([]Card(nil), p.Offers...)
	r.Prepare(1200)
	added := p.Offers[2]
	r.Prepare(1400)
	if !p.ShopLocked || len(p.Offers) != 3 || !reflect.DeepEqual(p.Offers[:2], locked) || p.Offers[2].Token == added.Token || p.Offers[2].Kind == added.Kind {
		t.Fatal("original offers must remain locked while filled offer changes each round")
	}
	p.Coins = 20
	for _, card := range locked {
		action(t, r, "a", "buy", card.Token, 0)
	}
	r.Prepare(1600)
	if p.ShopLocked || len(p.Offers) != 3 {
		t.Fatal("shop must unlock when all original locked offers are gone")
	}
}
