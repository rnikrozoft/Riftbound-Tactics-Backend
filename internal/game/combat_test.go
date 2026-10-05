package game

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func fixtureFight(a, b *CombatRules) *fight {
	f := &fight{r: &Room{rng: rand.New(rand.NewSource(19))}, plan: &Plan{Units: []CombatUnit{}, Events: []CombatEvent{}}}
	for i, r := range []*CombatRules{a, b} {
		team := []string{"A", "B"}[i]
		u := CombatUnit{ID: team + ":1", Team: team, Kind: 0, Stars: 1, Slot: 3, MaxHP: 100, InitialHP: 100}
		f.units = append(f.units, &fighter{unit: u, hp: 100, side: i, rules: r})
		f.plan.Units = append(f.plan.Units, u)
	}
	return f
}
func basic(mode string) *CombatRules {
	return &CombatRules{Actions: []AttackRule{{Animation: "attack01", Mode: mode, Hits: 1, Power: 100, Frames: 8, HitFrames: []int{2}}}}
}
func bySlug(slug string) CharacterDefinition {
	for _, d := range Catalog().Characters {
		if strings.HasSuffix(d.ScenePath, "/"+slug+".tscn") {
			return d
		}
	}
	panic(slug)
}
func TestCombatArmorAndSimultaneousCounter(t *testing.T) {
	f := fixtureFight(basic("melee"), basic("melee"))
	a, b := f.units[0], f.units[1]
	a.hp = 15
	b.hp = 10
	b.armor = 8
	f.action(a, b, 0)
	if a.hp != 5 || b.hp != 3 || b.armor != 0 {
		t.Fatalf("got a=%d b=%d armor=%d", a.hp, b.hp, b.armor)
	}
	f = fixtureFight(basic("melee"), basic("melee"))
	a, b = f.units[0], f.units[1]
	a.hp = 15
	b.hp = 10
	f.action(a, b, 0)
	if a.hp != 5 || b.hp != 0 {
		t.Fatal("dead target must still counter from its pre-action HP")
	}
}
func TestCombatRangedMultiHitsAndEarlyDeath(t *testing.T) {
	for _, n := range []int{2, 4} {
		r := basic("ranged")
		r.Actions[0].Hits = n
		r.Actions[0].Power = 120
		if n == 4 {
			r.Actions[0].Power = 140
		}
		r.Actions[0].HitFrames = make([]int, n)
		for i := range r.Actions[0].HitFrames {
			r.Actions[0].HitFrames[i] = i + 1
		}
		f := fixtureFight(r, basic("melee"))
		a, b := f.units[0], f.units[1]
		a.hp = 10
		b.hp = 50
		b.armor = 3
		f.action(a, b, 0)
		count, total := 0, 0
		for _, h := range f.plan.Events[0].Hits {
			if h.Kind == "damage" {
				count++
				total += h.Damage
			}
		}
		if count != n || total != percent(10, r.Actions[0].Power) || a.hp != 10 || b.armor != 0 || b.hp != 53-total {
			t.Fatal("multi-hit totals/armor/ranged counter incorrect")
		}
		f = fixtureFight(r, basic("melee"))
		f.units[0].hp = 10
		f.units[1].hp = 1
		f.action(f.units[0], f.units[1], 0)
		count = 0
		for _, h := range f.plan.Events[0].Hits {
			if h.Kind == "damage" {
				count++
			}
		}
		if count != 1 {
			t.Fatal("hits continue after death")
		}
	}
}
func TestCombatFireStunGraceAndPoison(t *testing.T) {
	f := fixtureFight(basic("ranged"), basic("ranged"))
	a, b := f.units[0], f.units[1]
	a.hp = 10
	b.fire = 3
	f.action(a, b, 0)
	if b.hp != 87 || b.fire != 0 {
		t.Fatal("fire must apply and expire once")
	}
	b.stun = true
	f.action(b, a, 1)
	if a.hp != 10 || b.stun || !b.grace {
		t.Fatal("stun skip")
	}
	f.status(b, "stun", 1)
	if b.stun {
		t.Fatal("chain stun")
	}
	b.hp = 1
	f.action(b, a, 2)
	if b.grace {
		t.Fatal("grace never expires")
	}
	f = fixtureFight(basic("ranged"), basic("ranged"))
	a, b = f.units[0], f.units[1]
	a.poison = 2
	a.poisonTicks = 2
	a.armor = 20
	f.action(a, b, 0)
	if a.hp != 98 || a.armor != 20 || a.poisonTicks != 1 {
		t.Fatal("poison should bypass armor")
	}
}
func TestCombatHealLifestealAndAntiHeal(t *testing.T) {
	r := basic("ranged")
	r.Actions[0].Lifesteal = 3
	f := fixtureFight(r, basic("melee"))
	a, b := f.units[0], f.units[1]
	a.hp = 10
	b.armor = 100
	f.action(a, b, 0)
	if a.hp != 10 {
		t.Fatal("armor damage grants lifesteal")
	}
	b.armor = 0
	f.action(a, b, 1)
	if a.hp != 13 {
		t.Fatal("lifesteal missing")
	}
	a.antiHeal = true
	f.action(a, b, 2)
	if a.hp != 13 || a.antiHeal {
		t.Fatal("anti-heal duration")
	}
	r = basic("ranged")
	r.Actions[0].Mode = "heal"
	r.Actions[0].Heal = 4
	f = fixtureFight(r, basic("melee"))
	f.units[0].hp = 99
	f.action(f.units[0], f.units[1], 0)
	if f.units[0].hp != 100 || f.units[1].hp != 100 {
		t.Fatal("support heal must replace damage and cap HP")
	}
}
func TestCombatSummonReviveAndNoPersistentMutation(t *testing.T) {
	r := basic("melee")
	r.Actions[0].Mode = "summon"
	f := fixtureFight(r, basic("ranged"))
	a := f.units[0]
	f.action(a, f.units[1], 0)
	if len(f.units) != 3 || f.units[2].hp != 5 || f.units[2].canAct != 2 || !f.units[2].unit.Summoned {
		t.Fatal("summon")
	}
	f.action(a, f.units[1], 1)
	f.action(a, f.units[1], 2)
	if len(f.units) != 4 {
		t.Fatal("summon cap")
	}
	r = basic("melee")
	r.Actions[0].Mode = "revive"
	f = fixtureFight(r, basic("ranged"))
	a = f.units[0]
	dead := &fighter{unit: CombatUnit{ID: "A:dead", Team: "A", MaxHP: 20, Slot: 2, Stars: 1}, hp: 0, side: 0}
	f.units = append(f.units, dead)
	f.action(a, f.units[1], 0)
	if dead.hp != 8 || dead.armor != 0 || !dead.revived {
		t.Fatal("revive")
	}
	dead.hp = 0
	f.action(a, f.units[1], 1)
	if dead.hp != 0 {
		t.Fatal("revive loop")
	}
	room := setup(t)
	room.Player("a").Units = []Unit{{Kind: bySlug("warlock").Kind, Token: 1, Stars: 1, Slot: 3}}
	room.Player("b").Units = []Unit{{Kind: bySlug("knight").Kind, Token: 2, Stars: 4, Slot: 3}}
	room.Start(0)
	if len(room.Player("a").Units) != 1 {
		t.Fatal("summon leaked to preparation")
	}
}
func TestCombatEveryShippedProfileIsValidAndReplays(t *testing.T) {
	if err := ValidateCharacterCatalog(Catalog()); err != nil {
		t.Fatal(err)
	}
	for _, d := range Catalog().Characters {
		if d.Combat == nil {
			t.Fatal("missing rules", d.ID)
		}
	}
	for _, slug := range []string{"black_knight_a", "black_knight_b", "black_knight_c", "demoness_a", "ghostfire", "warlock", "wizard", "swordsman", "knight_templar", "lancer", "werewolf", "hellbat"} {
		t.Run(slug, func(t *testing.T) {
			for seed := int64(0); seed < 12; seed++ {
				room := setup(t)
				room.rng = rand.New(rand.NewSource(seed))
				for side, p := range room.State.Players {
					for i := 0; i < 6; i++ {
						kind := bySlug(slug).Kind
						if side == 1 {
							kind = bySlug("armored_orc").Kind
						}
						p.Units = append(p.Units, Unit{Kind: kind, Token: side*10 + i + 1, Slot: i, Stars: 1 + i%4})
					}
				}
				plan := room.Start(0)
				if len(plan.Events) > 512 {
					t.Fatal("unbounded battle")
				}
				hp := map[string]int{}
				for _, u := range plan.Units {
					hp[u.ID] = u.InitialHP
				}
				for i, e := range plan.Events {
					if hp[e.Attacker] <= 0 || e.Index != i {
						t.Fatal("dead actor")
					}
					for _, h := range e.Hits {
						for _, c := range h.Changes {
							if c.HP < 0 || c.Armor < 0 || c.Slot < 0 || c.Slot > 5 {
								t.Fatal("invalid snapshot")
							}
							hp[c.ID] = c.HP
						}
					}
				}
				raw, _ := json.Marshal(plan)
				var copy Plan
				if err := json.Unmarshal(raw, &copy); err != nil || !reflect.DeepEqual(plan, &copy) {
					t.Fatal("replay protocol round-trip")
				}
				winnerSide := ""
				if plan.Winner != "DRAW" {
					winnerSide = plan.Winner
				}
				damage := 0
				for _, u := range plan.Units {
					if u.Team == winnerSide && hp[u.ID] > 0 && !u.Summoned {
						damage += u.Stars
					}
				}
				if winnerSide != "" {
					damage += 2
				}
				if plan.PlayerDamage != damage {
					t.Fatal("survivor damage includes dead/summoned units")
				}
			}
		})
	}
}

func TestExecuteBypassesArmorAndFirstBlock(t *testing.T) {
	aRule := basic("melee")
	aRule.Execute = 25
	bRule := basic("melee")
	bRule.BlockFirst = true
	f := fixtureFight(aRule, bRule)
	a, b := f.units[0], f.units[1]
	a.hp = 10
	b.hp = 20
	b.armor = 99
	f.action(a, b, 0)
	if b.hp != 0 || f.plan.Events[0].Mode != "execute" || !a.executeUsed {
		t.Fatal("execute must kill through armor and block")
	}
}
func TestLifestealCountsDamageBeforeReactiveHeal(t *testing.T) {
	r := basic("ranged")
	r.Actions[0].Lifesteal = 3
	defender := basic("ranged")
	defender.FirstHeal = 2
	f := fixtureFight(r, defender)
	f.units[0].hp = 1
	f.units[1].hp = 50
	f.action(f.units[0], f.units[1], 0)
	if f.units[0].hp != 2 || f.units[1].hp != 51 {
		t.Fatal("reactive healing made lifesteal negative")
	}
}
func TestBodyguardSplashAndBackTargeting(t *testing.T) {
	r := basic("melee")
	r.Actions[0].Splash = "back"
	r.Actions[0].SplashPower = 40
	f := fixtureFight(r, basic("ranged"))
	a, b := f.units[0], f.units[1]
	a.hp = 10
	b.hp = 50
	back := &fighter{unit: CombatUnit{ID: "B:back", Team: "B", MaxHP: 50, Slot: 0, Stars: 1}, hp: 50, side: 1}
	f.units = append(f.units, back)
	f.action(a, b, 0)
	if back.hp != 46 || a.hp != 0 {
		t.Fatal("splash or single primary retaliation wrong")
	}
	f = fixtureFight(basic("ranged"), basic("ranged"))
	a, b = f.units[0], f.units[1]
	a.hp = 10
	b.unit.Slot = 0
	guard := &fighter{unit: CombatUnit{ID: "B:guard", Team: "B", MaxHP: 50, Slot: 3, Stars: 1}, hp: 50, side: 1, rules: &CombatRules{Bodyguard: true}}
	f.units = append(f.units, guard)
	f.action(a, b, 0)
	if b.hp != 100 || guard.hp != 40 || !guard.guardUsed {
		t.Fatal("bodyguard did not intercept rear target")
	}
	f.action(a, b, 1)
	if b.hp != 90 || guard.hp != 40 {
		t.Fatal("bodyguard intercepted more than once")
	}
}
func TestSummonBlockedOnFullFieldAndPoisonIgnoresBlock(t *testing.T) {
	r := basic("melee")
	r.Actions[0].Mode = "summon"
	f := fixtureFight(r, basic("ranged"))
	f.units[0].unit.Slot = 0
	for slot := 1; slot < 6; slot++ {
		f.units = append(f.units, &fighter{unit: CombatUnit{ID: fmt.Sprintf("A:%d", slot+1), Slot: slot, MaxHP: 10}, hp: 10, side: 0})
	}
	f.action(f.units[0], f.units[1], 0)
	if len(f.units) != 7 || f.units[0].summons != 0 {
		t.Fatal("summoned into full formation")
	}
	r = basic("ranged")
	r.BlockFirst = true
	f = fixtureFight(r, basic("ranged"))
	a := f.units[0]
	a.poison = 2
	a.poisonTicks = 2
	f.action(a, f.units[1], 0)
	if a.hp != 98 || a.blockUsed {
		t.Fatal("poison consumed or was reduced by first block")
	}
}
