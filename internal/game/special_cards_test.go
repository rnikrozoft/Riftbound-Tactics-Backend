package game

import "testing"

func addSpecial(f *fight, side int, effect string) *fighter {
	for _, kind := range SupportKinds() {
		if supportEffect(kind) == effect {
			u := &fighter{unit: CombatUnit{ID: effect, Kind: kind, Slot: 6, MaxHP: 1}, hp: 1, side: side}
			f.units = append(f.units, u)
			return u
		}
	}
	panic(effect)
}
func TestSpecialAbilities(t *testing.T) {
	t.Run("guardian", func(t *testing.T) {
		f := fixtureFight(basic("ranged"), basic("ranged"))
		addSpecial(f, 0, "guardian")
		f.event = &CombatEvent{}
		f.startSupports()
		f.startSupports()
		if f.units[0].armor != 2 {
			t.Fatal("armor must grant exactly once")
		}
	})
	t.Run("banner", func(t *testing.T) {
		f := fixtureFight(basic("ranged"), basic("ranged"))
		addSpecial(f, 0, "banner")
		f.event = &CombatEvent{}
		f.block = map[string]bool{}
		f.damage(f.units[0], f.units[1], 20, 0, 0, "damage")
		if f.units[1].hp != 78 {
			t.Fatal("banner damage")
		}
	})
	t.Run("purify", func(t *testing.T) {
		f := fixtureFight(basic("ranged"), basic("ranged"))
		addSpecial(f, 0, "purify")
		f.event = &CombatEvent{}
		f.units[0].fire = 2
		f.units[0].poison = 2
		f.units[0].poisonTicks = 2
		f.endSupports(0)
		if f.units[0].fire != 0 || f.units[0].poison != 2 {
			t.Fatal("cleanse one status")
		}
		f.endSupports(0)
		if f.units[0].poison != 0 {
			t.Fatal("cleanse poison")
		}
	})
	for _, effect := range []string{"thorns", "frost", "mine", "mirror"} {
		t.Run(effect, func(t *testing.T) {
			f := fixtureFight(basic("ranged"), basic("ranged"))
			trap := addSpecial(f, 1, effect)
			f.event = &CombatEvent{}
			f.block = map[string]bool{}
			mode := "melee"
			if effect == "mirror" {
				mode = "ranged"
			}
			f.afterAttack(f.units[0], f.units[1], mode, 20, 2)
			if trap.hp != 0 {
				t.Fatal("trap not consumed")
			}
			switch effect {
			case "thorns":
				if f.units[0].hp != 96 {
					t.Fatal("thorn damage")
				}
			case "frost":
				if !f.units[0].stun {
					t.Fatal("stun")
				}
			case "mine":
				if f.units[0].fire != 2 {
					t.Fatal("burn")
				}
			case "mirror":
				if f.units[0].hp != 90 {
					t.Fatal("reflect")
				}
			}
			n := len(f.event.Hits)
			f.afterAttack(f.units[0], f.units[1], mode, 20, 2)
			if len(f.event.Hits) != n {
				t.Fatal("trap reused")
			}
		})
	}
	t.Run("ward", func(t *testing.T) {
		f := fixtureFight(basic("ranged"), basic("ranged"))
		trap := addSpecial(f, 1, "ward")
		f.event = &CombatEvent{}
		f.block = map[string]bool{}
		f.damage(f.units[0], f.units[1], 200, 100, 0, "damage")
		if trap.hp != 0 || f.units[1].hp != 100 {
			t.Fatal("lethal prevention")
		}
		f.damage(f.units[0], f.units[1], 200, 100, 0, "damage")
		if f.units[1].hp != 0 {
			t.Fatal("ward reused")
		}
	})
	t.Run("seal", func(t *testing.T) {
		rule := basic("ranged")
		rule.Actions[0].Hits = 4
		rule.Actions[0].Power = 40
		f := fixtureFight(rule, basic("ranged"))
		trap := addSpecial(f, 1, "seal")
		f.action(f.units[0], f.units[1], 0)
		if trap.hp != 0 || f.units[1].hp != 90 {
			t.Fatalf("seal hp=%d", f.units[1].hp)
		}
	})
}
func TestEverySpecialPurchasableAndDeployable(t *testing.T) {
	for _, kind := range SupportKinds() {
		r := setup(t)
		p := r.Player("a")
		p.Coins = 20
		r.serial++
		token := r.serial
		p.Offers = []Card{{Token: token, Kind: kind, Price: CardCost(kind)}}
		action(t, r, "a", "buy", token, 0)
		action(t, r, "a", "deploy", token, 6)
		if len(p.Units) != 1 || p.Units[0].Kind != kind || p.Coins != 20-CardCost(kind) {
			t.Fatalf("purchase/deploy %d", kind)
		}
	}
}

func TestConsumedSpecialRemovedBeforeNextRound(t *testing.T) {
	r := setup(t)
	p := r.Player("a")
	var kind int
	for _, k := range SupportKinds() {
		if supportEffect(k) == "thorns" {
			kind = k
		}
	}
	p.Units = []Unit{{Kind: kind, Token: 777, Stars: 1, Slot: 6}, {Kind: SupportKind(), Token: 778, Stars: 1, Slot: 7}}
	r.State.Phase = "battle"
	r.State.Battle = &Plan{Winner: "DRAW", EndMS: 1, Events: []CombatEvent{{Hits: []CombatHit{{Kind: "trap_consumed", Source: p.Team + ":777"}}}}}
	r.Finish(2)
	if len(p.Units) != 1 || p.Units[0].Token != 778 {
		t.Fatal("consumed trap persisted or permanent support removed")
	}
	if len(r.State.Battle.Events[0].Hits) != 1 {
		t.Fatal("replay lost consumption event")
	}
}

func TestSpecialCardsCannotMerge(t *testing.T) {
	for _, kind := range SupportKinds() {
		r := setup(t)
		p := r.Player("a")
		p.Coins = 20
		p.Hand = []Card{{Kind: kind, Token: 9000, Stars: 1}}
		r.serial++
		token := r.serial
		p.Offers = []Card{{Kind: kind, Token: token, Price: CardCost(kind)}}
		action(t, r, "a", "buy", token, 0)
		if len(p.Hand) != 2 {
			t.Fatal("Special merged", kind)
		}
		for _, c := range p.Hand {
			if c.Stars != 1 {
				t.Fatal("Special upgraded", kind)
			}
		}
	}
}
