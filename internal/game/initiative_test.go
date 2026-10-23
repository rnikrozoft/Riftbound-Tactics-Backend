package game

import (
	"math/rand"
	"testing"
)

func TestOpeningSideAttackAndTie(t *testing.T) {
	seen := map[int]bool{}
	for seed := int64(0); seed < 100; seed++ {
		r := &Room{rng: rand.New(rand.NewSource(seed))}
		units := []CombatUnit{{Team: "A", Attack: 8}, {Team: "A", Attack: 7}, {Team: "B", Attack: 14}}
		if r.openingSide(units) != 0 {
			t.Fatal("combined A attack must start")
		}
		units[2].Attack = 16
		if r.openingSide(units) != 1 {
			t.Fatal("stronger B must start")
		}
		// Unequal totals must not consume the tie-breaking random draw.
		units[2].Attack = 15
		got := r.openingSide(units)
		if got != rand.New(rand.NewSource(seed)).Intn(2) {
			t.Fatal("unexpected random draw")
		}
		seen[got] = true
	}
	if len(seen) != 2 {
		t.Fatal("ties must allow either side")
	}
}
func TestFormationActionOrder(t *testing.T) {
	for side := 0; side < 2; side++ {
		f := &fight{}
		for slot := 0; slot < 6; slot++ {
			f.units = append(f.units, &fighter{side: side, hp: 100, unit: CombatUnit{Slot: slot}})
		}
		cursor := 0
		for i := 0; i < 12; i++ {
			u := f.nextActor(side, &cursor)
			if u == nil || u.unit.Slot != 5-i%6 {
				t.Fatal("incorrect mirrored slot cycle", side, i)
			}
		}
		f.units[0].hp = 0
		f.units[2].canAct = 10
		cursor = 0
		for i := 0; i < 12; i++ {
			u := f.nextActor(side, &cursor)
			if u == nil || u.unit.Slot == 0 || u.unit.Slot == 2 {
				t.Fatal("dead or delayed actor selected")
			}
		}
		for _, u := range f.units {
			u.hp = 0
		}
		if f.nextActor(side, &cursor) != nil {
			t.Fatal("empty team acted")
		}
	}
}
