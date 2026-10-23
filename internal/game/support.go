package game

// Support cards never enter the target/initiative lists. Their state is carried in
// the same combat snapshots as heroes so online, replay and preparation agree.
func (f *fight) support(side int, effect string) *fighter {
	for _, u := range f.units {
		if u.side == side && u.hp > 0 && supportEffect(u.unit.Kind) == effect {
			return u
		}
	}
	return nil
}
func supportPower(u *fighter, fallback int) int {
	if v := Character(u.unit.Kind).SupportValue; v > 0 {
		return v
	}
	return fallback
}
func (f *fight) consume(u *fighter, frame int) { u.hp = 0; f.record(frame, u, u, 0, "trap_consumed") }
func (f *fight) startSupports() {
	if f.supportStarted {
		return
	}
	f.supportStarted = true
	for _, u := range f.units {
		if u.hp > 0 && (supportEffect(u.unit.Kind) == "guardian" || supportEffect(u.unit.Kind) == "armor_once") {
			amount := 2
			if supportEffect(u.unit.Kind) == "armor_once" {
				amount = supportPower(u, 1)
				f.consume(u, 0)
			}
			for _, ally := range f.alive(u.side) {
				ally.armor += amount
				f.record(0, u, ally, 0, "armor")
			}
		}
	}
}
func (f *fight) endSupports(frame int) {
	for _, u := range f.units {
		if u.hp <= 0 {
			continue
		}
		switch supportEffect(u.unit.Kind) {
		case "heal":
			for _, ally := range f.alive(u.side) {
				f.heal(u, ally, 1, frame)
			}
		case "heal_once", "heal_all_once":
			var chosen *fighter
			for _, ally := range f.alive(u.side) {
				if !ally.antiHeal && ally.hp < ally.unit.MaxHP && (chosen == nil || ally.hp < chosen.hp) {
					chosen = ally
				}
			}
			if chosen != nil {
				f.consume(u, frame)
				if supportEffect(u.unit.Kind) == "heal_once" {
					f.heal(u, chosen, supportPower(u, 1), frame)
				} else {
					for _, ally := range f.alive(u.side) {
						f.heal(u, ally, supportPower(u, 1), frame)
					}
				}
			}
		case "cleanse_once":
			var chosen *fighter
			for _, ally := range f.alive(u.side) {
				if (ally.fire > 0 || ally.poison > 0 || ally.stun || ally.curse || ally.antiHeal) && (chosen == nil || ally.hp < chosen.hp) {
					chosen = ally
				}
			}
			if chosen != nil {
				f.consume(u, frame)
				chosen.fire = 0
				chosen.poison = 0
				chosen.poisonTicks = 0
				chosen.stun = false
				chosen.curse = false
				chosen.antiHeal = false
				f.record(frame, u, chosen, 0, "cleanse")
				f.heal(u, chosen, supportPower(u, 1), frame)
			}
		case "purify":
			var chosen *fighter
			for _, ally := range f.alive(u.side) {
				if (ally.fire > 0 || ally.poisonTicks > 0) && (chosen == nil || ally.hp < chosen.hp) {
					chosen = ally
				}
			}
			if chosen != nil {
				if chosen.fire > 0 {
					chosen.fire = 0
				} else {
					chosen.poison = 0
					chosen.poisonTicks = 0
				}
				f.record(frame, u, chosen, 0, "cleanse")
			}
		}
	}
}
func (f *fight) afterAttack(a, target *fighter, mode string, total, frame int) {
	if mode != "melee" && mode != "ranged" {
		return
	}
	if mode == "melee" {
		if trap := f.support(target.side, "thorns"); trap != nil {
			f.consume(trap, frame)
			f.damage(trap, a, supportPower(trap, 4), 100, frame, "thorns")
		}
	}
	if trap := f.support(target.side, "frost"); trap != nil {
		f.consume(trap, frame)
		if n := Character(trap.unit.Kind).SupportValue; n > 0 {
			f.damage(trap, a, n, 0, frame, "retaliate")
		}
		f.status(a, "stun", 1)
		f.record(frame, trap, a, 0, "status")
	}
	if mode == "melee" {
		if trap := f.support(target.side, "mine"); trap != nil {
			f.consume(trap, frame)
			if n := Character(trap.unit.Kind).SupportValue; n > 0 {
				f.damage(trap, a, n, 0, frame, "retaliate")
			}
			f.status(a, "fire", 1)
			f.record(frame, trap, a, 0, "status")
			for _, other := range f.splash(a, "sides") {
				f.status(other, "fire", 1)
				f.record(frame, trap, other, 0, "status")
			}
		}
	}
	if mode == "ranged" {
		if trap := f.support(target.side, "mirror"); trap != nil {
			f.consume(trap, frame)
			f.damage(trap, a, percent(total, supportPower(trap, 50)), 0, frame, "reflect")
		}
	}
	if a.hp > 0 {
		if trap := f.support(target.side, "retaliate"); trap != nil {
			f.consume(trap, frame)
			f.damage(trap, a, supportPower(trap, 1), 0, frame, "retaliate")
		}
	}
	if a.hp > 0 {
		if trap := f.support(target.side, "weaken"); trap != nil {
			f.consume(trap, frame)
			f.damage(trap, a, supportPower(trap, 1), 0, frame, "retaliate")
			if a.hp > 0 {
				a.curse = true
				f.record(frame, trap, a, 0, "curse")
			}
		}
	}

}
func consumedSupport(plan *Plan) map[string]bool {
	used := map[string]bool{}
	if plan != nil {
		for _, event := range plan.Events {
			for _, hit := range event.Hits {
				if hit.Kind == "trap_consumed" {
					used[hit.Source] = true
				}
			}
		}
	}
	return used
}
