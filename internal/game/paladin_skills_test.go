package game

import (
	"strings"
	"testing"
)

func TestPaladinSkillValuesAndTriggers(t *testing.T) {
	count := 0
	seen := map[string]bool{}
	for _, card := range Catalog().Characters {
		if !strings.Contains(card.Tags, "skill_card") {
			continue
		}
		count++
		t.Run(card.ID, func(t *testing.T) {
			if seen[card.Name] {
				t.Fatal("duplicate name")
			}
			seen[card.Name] = true
			if card.Role != "trap" || card.MaxCopies != 1 {
				t.Fatal("skill tiers/ownership")
			}
			f := fixtureFight(basic("ranged"), basic("ranged"))
			a, b := f.units[0], f.units[1]
			u := &fighter{unit: CombatUnit{ID: card.ID, Kind: card.Kind, Slot: 6, MaxHP: 1}, hp: 1, side: 1}
			f.units = append(f.units, u)
			f.event = &CombatEvent{}
			f.block = map[string]bool{}
			v := card.SupportValue
			switch card.SupportEffect {
			case "armor_once":
				f.startSupports()
				if b.armor != v {
					t.Fatal("armor value")
				}
			case "heal_once", "heal_all_once":
				f.endSupports(0)
				if u.hp == 0 {
					t.Fatal("wasted heal at full HP")
				}
				b.hp = 50
				f.endSupports(0)
				if b.hp != 50+v {
					t.Fatal("heal value")
				}
			case "cleanse_once":
				b.hp = 50
				b.fire = 2
				b.poison = 2
				b.poisonTicks = 2
				b.stun = true
				b.curse = true
				b.antiHeal = true
				f.endSupports(0)
				if b.hp != 50+v || b.fire != 0 || b.poison != 0 || b.stun || b.curse || b.antiHeal {
					t.Fatal("cleanse")
				}
			case "ward":
				f.damage(a, b, 200, 100, 0, "damage")
				if b.hp != 100 || b.armor != v {
					t.Fatal("ward bonus")
				}
			case "seal":
				a.rules.Actions[0].Hits = 4
				a.rules.Actions[0].Power = 40
				f.action(a, b, 0)
				if b.hp != 100-percent(10, v) {
					t.Fatal("seal multiplier", b.hp)
				}
			case "thorns":
				f.afterAttack(a, b, "melee", 20, 0)
				if a.hp != 100-v {
					t.Fatal("thorn value")
				}
			case "mirror":
				f.afterAttack(a, b, "ranged", 20, 0)
				if a.hp != 100-percent(20, v) {
					t.Fatal("reflection value")
				}
			case "frost":
				f.afterAttack(a, b, "ranged", 20, 0)
				if !a.stun || a.hp != 100-v {
					t.Fatal("stun value")
				}
			case "mine":
				f.afterAttack(a, b, "melee", 20, 0)
				if a.fire != 2 || a.hp != 100-v {
					t.Fatal("mine value")
				}
			case "retaliate":
				a.armor = 2
				f.afterAttack(a, b, "ranged", 20, 0)
				if a.hp != 100-(v-2) {
					t.Fatal("counter must respect armor")
				}
			case "weaken":
				f.afterAttack(a, b, "ranged", 20, 0)
				if !a.curse || a.hp != 100-v {
					t.Fatal("weaken")
				}
			default:
				t.Fatal("unknown skill")
			}
			if u.hp != 0 {
				t.Fatal("skill not consumed")
			}
		})
	}
	if count != 48 {
		t.Fatal("missing skill cards", count)
	}
}
