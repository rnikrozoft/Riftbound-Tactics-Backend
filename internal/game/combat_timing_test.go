package game

import "testing"

func TestCombatPresentationBudgetsActualAction(t *testing.T) {
	rules := &CombatRules{Actions: []AttackRule{{Animation: "attack01", Frames: 7}, {Animation: "attack02", Frames: 15}}, Finisher: "attack03", FinisherFrames: 30}
	cases := []struct {
		name  string
		event CombatEvent
		want  int64
	}{
		{"short melee does not reserve unused finisher", CombatEvent{Animation: "attack01", Mode: "melee", Hits: []CombatHit{{Frame: 4, Damage: 5}}}, 1399},
		{"ranged does not reserve dash and retreat", CombatEvent{Animation: "attack01", Mode: "ranged", Hits: []CombatHit{{Frame: 4, Damage: 5}}}, 839},
		{"multi hit reserves each impact", CombatEvent{Animation: "attack02", Mode: "melee", Hits: []CombatHit{{Frame: 5, Damage: 3}, {Frame: 10, Damage: 3}}}, 2140},
		{"stun skip is brief", CombatEvent{Animation: "hit", Mode: "stun"}, 514},
		{"lethal reserves charge and zoom", CombatEvent{Attacker: "A", Target: "B", Animation: "attack03", Mode: "melee", Dead: true, Hits: []CombatHit{{Frame: 20, Damage: 5}}}, 4135},
		{"late status snapshot cannot be cut off", CombatEvent{Animation: "attack01", Mode: "ranged", Hits: []CombatHit{{Frame: 19}}}, 1847},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := combatPresentationMS(tc.event, rules); got != tc.want {
				t.Fatalf("got %dms want %dms", got, tc.want)
			}
		})
	}
}
