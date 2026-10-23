package game

// Both teams use the same local formation order; rendering mirrors the opponent.
func actionSlotRank(side, slot int) int {
	return 5 - slot
}
func (r *Room) openingSide(units []CombatUnit) int {
	totals := [2]int{}
	for _, u := range units {
		if IsSupport(u.Kind) {
			continue
		}
		side := 0
		if u.Team == "B" {
			side = 1
		}
		totals[side] += u.Attack
	}
	if totals[0] > totals[1] {
		return 0
	}
	if totals[1] > totals[0] {
		return 1
	}
	return r.rng.Intn(2)
}
func (f *fight) nextActor(side int, cursor *int) *fighter {
	for offset := 0; offset < 6; offset++ {
		rank := (*cursor + offset) % 6
		for _, u := range f.alive(side) {
			if u.canAct <= f.step && actionSlotRank(side, u.unit.Slot) == rank {
				*cursor = (rank + 1) % 6
				return u
			}
		}
	}
	return nil
}
