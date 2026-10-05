package game

import (
	"fmt"
	"math"
	"strings"
)

// Rules are shipped in the catalog, so combat and the details UI describe the same abilities.
type AttackRule struct {
	Animation    string `json:"animation"`
	Mode         string `json:"mode"`
	Hits         int    `json:"hits"`
	Power        int    `json:"power"`
	Frames       int    `json:"frames"`
	HitFrames    []int  `json:"hit_frames"`
	Pierce       int    `json:"pierce"`
	Splash       string `json:"splash"`
	SplashPower  int    `json:"splash_power"`
	MinStars     int    `json:"min_stars"`
	Status       string `json:"status"`
	SplashStatus string `json:"splash_status"`
	Lifesteal    int    `json:"lifesteal"`
	TeamHeal     int    `json:"team_heal"`
	Heal         int    `json:"heal"`
	SwapBack     bool   `json:"swap_back"`
	TargetBack   bool   `json:"target_back"`
	AntiHeal     bool   `json:"anti_heal"`
}
type CombatRules struct {
	Actions           []AttackRule `json:"actions"`
	First             *AttackRule  `json:"first,omitempty"`
	Finisher          string       `json:"finisher"`
	FinisherHits      int          `json:"finisher_hits"`
	FinisherFrames    int          `json:"finisher_frames"`
	FinisherHitFrames []int        `json:"finisher_hit_frames"`
	BlockFirst        bool         `json:"block_first"`
	Bodyguard         bool         `json:"bodyguard"`
	FirstBonus        int          `json:"first_bonus"`
	FirstHeal         int          `json:"first_heal"`
	LowArmor          int          `json:"low_armor"`
	DeathArmor        int          `json:"death_armor"`
	KillStatus        string       `json:"kill_status"`
	KillSplash        string       `json:"kill_splash"`
	Suicide           bool         `json:"suicide"`
	KillNoCounter     bool         `json:"kill_no_counter"`
	Execute           int          `json:"execute"`
}

func (c *CombatRules) Validate() error {
	if len(c.Actions) < 1 || len(c.Actions) > 4 {
		return fmt.Errorf("combat requires 1–4 actions")
	}
	actions := append([]AttackRule{}, c.Actions...)
	if c.First != nil {
		actions = append(actions, *c.First)
	}
	valid := func(v string, options ...string) bool {
		for _, x := range options {
			if v == x {
				return true
			}
		}
		return false
	}
	for _, a := range actions {
		if !valid(a.Mode, "melee", "ranged", "heal", "curse", "summon", "revive") || a.Hits < 0 || a.Hits > 4 || a.Power < 0 || a.Power > 200 || a.Pierce < 0 || a.Pierce > 100 || a.SplashPower < 0 || a.SplashPower > 100 || !valid(a.Splash, "", "sides", "back", "around", "column") || !valid(a.Status, "", "fire", "stun", "poison") || !valid(a.SplashStatus, "", "fire", "stun", "poison") || a.Frames < 1 || len(a.HitFrames) != a.Hits {
			return fmt.Errorf("invalid attack rule")
		}
		prior := -1
		for _, f := range a.HitFrames {
			if f <= prior || f >= a.Frames {
				return fmt.Errorf("invalid impact frames")
			}
			prior = f
		}
	}
	return nil
}

type CombatChange struct {
	ID       string `json:"id"`
	HP       int    `json:"hp"`
	Armor    int    `json:"armor"`
	Slot     int    `json:"slot"`
	Fire     int    `json:"fire"`
	Stun     bool   `json:"stun"`
	Poison   int    `json:"poison"`
	Curse    bool   `json:"curse"`
	AntiHeal bool   `json:"anti_heal"`
}
type CombatHit struct {
	Frame   int            `json:"frame"`
	Source  string         `json:"source"`
	Target  string         `json:"target"`
	Damage  int            `json:"damage"`
	Kind    string         `json:"kind"`
	Changes []CombatChange `json:"changes"`
}
type fighter struct {
	unit                                                                                                             CombatUnit
	hp, armor, side, turn, fire, poison, poisonTicks, canAct                                                         int
	stun, grace, curse, antiHeal, blockUsed, guardUsed, healUsed, lowUsed, killUsed, executeUsed, revived, deathUsed bool
	summons                                                                                                          int
	rules                                                                                                            *CombatRules
}
type fight struct {
	r     *Room
	plan  *Plan
	units []*fighter
	step  int
	event *CombatEvent
	block map[string]bool
}

func scaled(v, stars int) int {
	return int(math.Round(float64(v) * []float64{1, 1.5, 2, 2.5}[starCount(stars)-1]))
}
func percent(v, p int) int { return (v*p + 50) / 100 }
func (f *fight) alive(side int) []*fighter {
	var out []*fighter
	for _, u := range f.units {
		if u.side == side && u.hp > 0 {
			out = append(out, u)
		}
	}
	return out
}
func (f *fight) pick(list []*fighter) *fighter {
	if len(list) == 0 {
		return nil
	}
	return list[f.r.rng.Intn(len(list))]
}
func (f *fight) snapshot() []CombatChange {
	out := make([]CombatChange, 0, len(f.units))
	for _, u := range f.units {
		out = append(out, CombatChange{u.unit.ID, u.hp, u.armor, u.unit.Slot, u.fire, u.stun, u.poison, u.curse, u.antiHeal})
	}
	return out
}
func (f *fight) record(frame int, source, target *fighter, damage int, kind string) {
	f.event.Hits = append(f.event.Hits, CombatHit{frame, source.unit.ID, target.unit.ID, damage, kind, f.snapshot()})
}
func (f *fight) status(u *fighter, status string, stars int) {
	if u.hp <= 0 {
		return
	}
	switch status {
	case "fire":
		u.fire = scaled(3, stars)
	case "stun":
		if !u.grace {
			u.stun = true
		}
	case "poison":
		u.poison = scaled(2, stars)
		u.poisonTicks = 2
	}
}
func (f *fight) damage(source, u *fighter, n, pierce, frame int, kind string) int {
	if u.hp <= 0 {
		return 0
	}
	before := u.hp
	if kind != "poison" && kind != "execute" && u.rules != nil && u.rules.BlockFirst && !u.blockUsed {
		if _, ok := f.block[u.unit.ID]; !ok {
			f.block[u.unit.ID] = true
			u.blockUsed = true
		}
	}
	if kind != "poison" && kind != "execute" && f.block[u.unit.ID] {
		n = (n + 1) / 2
	}
	direct := n * pierce / 100
	rest := n - direct
	absorbed := min(u.armor, rest)
	u.armor -= absorbed
	u.hp = max(0, u.hp-direct-(rest-absorbed))
	lost := before - u.hp
	f.record(frame, source, u, n, kind)
	if u.hp > 0 && u.rules != nil {
		if before > u.hp && u.rules.FirstHeal > 0 && !u.healUsed {
			u.healUsed = true
			f.heal(source, u, scaled(u.rules.FirstHeal, u.unit.Stars), frame)
		}
		if u.hp*2 < u.unit.MaxHP && u.rules.LowArmor > 0 && !u.lowUsed {
			u.lowUsed = true
			u.armor += scaled(u.rules.LowArmor, u.unit.Stars)
			f.record(frame, u, u, 0, "armor")
		}
	}
	if u.hp == 0 && u.rules != nil && u.rules.DeathArmor > 0 && !u.deathUsed {
		u.deathUsed = true
		if friend := f.pick(f.alive(u.side)); friend != nil {
			friend.armor += scaled(u.rules.DeathArmor, u.unit.Stars)
			f.record(frame, u, friend, 0, "armor")
		}
	}
	return lost
}
func (f *fight) heal(source, u *fighter, n, frame int) {
	if u.hp <= 0 || u.antiHeal {
		return
	}
	before := u.hp
	u.hp = min(u.unit.MaxHP, u.hp+n)
	if before != u.hp {
		f.record(frame, source, u, 0, "heal")
	}
}
func neighbors(a, b int, shape string) bool {
	// Slots 0..2 are the back column, 3..5 the front column; row = slot % 3.
	ac, ar, bc, br := a/3, a%3, b/3, b%3
	switch shape {
	case "sides":
		return ac == bc && abs(ar-br) == 1
	case "back":
		return ac == 1 && bc == 0 && ar == br
	case "column":
		return ar == br && ac != bc
	case "around":
		return a != b && abs(ac-bc) <= 1 && abs(ar-br) <= 1
	}
	return false
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func (f *fight) splash(target *fighter, shape string) []*fighter {
	var out []*fighter
	for _, u := range f.alive(target.side) {
		if u != target && neighbors(target.unit.Slot, u.unit.Slot, shape) {
			out = append(out, u)
		}
	}
	return out
}
func (f *fight) empty(side int) int {
	used := [6]bool{}
	for _, u := range f.alive(side) {
		used[u.unit.Slot] = true
	}
	for i := 0; i < 6; i++ {
		if !used[i] {
			return i
		}
	}
	return -1
}
func (f *fight) skeletonKind() int {
	for _, c := range catalog.Characters {
		if strings.HasSuffix(c.ScenePath, "/skeleton.tscn") {
			return c.Kind
		}
	}
	return -1
}
func (f *fight) action(a, target *fighter, at int64) {
	rule := AttackRule{Animation: "attack01", Mode: "melee", Hits: 1, Power: 100, Frames: 8, HitFrames: []int{2}}
	if a.rules != nil {
		rule = a.rules.Actions[a.turn%len(a.rules.Actions)]
		if a.turn == 0 && a.rules.First != nil {
			rule = *a.rules.First
		}
	}
	a.turn++
	if rule.MinStars > a.unit.Stars {
		rule.Animation = "attack01"
	}
	f.block = map[string]bool{}
	event := CombatEvent{Index: len(f.plan.Events), Attacker: a.unit.ID, Target: target.unit.ID, AtMS: at, Animation: rule.Animation, Mode: rule.Mode}
	f.event = &event
	if a.stun {
		a.stun = false
		a.grace = true
		event.Animation = "hit"
		event.Mode = "stun"
		f.record(0, a, a, 0, "stun_skip")
		f.endAction(a, 0)
		f.record(0, a, a, 0, "action_end")
		event.TargetHP = target.hp
		event.Dead = target.hp == 0
		f.plan.Events = append(f.plan.Events, event)
		return
	}
	grace := a.grace
	curAnti := a.antiHeal
	slot := f.empty(a.side)
	switch rule.Mode {
	case "summon":
		if slot >= 0 && a.summons < 2 {
			kind := f.skeletonKind()
			if kind >= 0 {
				a.summons++
				stats := StatsFor(kind, a.unit.Stars)
				hp := scaled(5, a.unit.Stars)
				u := &fighter{unit: CombatUnit{ID: fmt.Sprintf("%s:summon:%d:%d", a.unit.Team, a.unit.Token, a.summons), Team: a.unit.Team, Kind: kind, Stars: a.unit.Stars, Slot: slot, MaxHP: hp, InitialHP: 0, Speed: stats.Speed, Summoned: true}, hp: hp, side: a.side, canAct: f.step + 2}
				f.units = append(f.units, u)
				f.plan.Units = append(f.plan.Units, u.unit)
				event.Target = u.unit.ID
				target = u
				f.record(2, a, u, 0, "summon")
				goto end
			}
		}
		rule.Mode = "melee"
		event.Mode = "melee"
		rule.Hits = 1
		rule.Power = 100
		rule.HitFrames = []int{2}
	case "revive":
		var dead *fighter
		for _, u := range f.units {
			if u.side == a.side && u.hp == 0 && !u.unit.Summoned && !u.revived && (dead == nil || u.unit.MaxHP > dead.unit.MaxHP) {
				dead = u
			}
		}
		if slot >= 0 && dead != nil && a.summons == 0 {
			a.summons = 1
			dead.revived = true
			dead.hp = max(1, percent(dead.unit.MaxHP, 40))
			dead.armor = 0
			dead.unit.Slot = slot
			dead.fire = 0
			dead.stun = false
			dead.poison = 0
			dead.poisonTicks = 0
			dead.curse = false
			dead.antiHeal = false
			dead.canAct = f.step + 2
			event.Target = dead.unit.ID
			target = dead
			f.record(2, a, dead, 0, "revive")
			goto end
		}
		rule.Mode = "melee"
		event.Mode = "melee"
		rule.Hits = 1
		rule.Power = 100
		rule.HitFrames = []int{2}
	case "heal":
		friends := f.alive(a.side)
		largest := -1
		var choices []*fighter
		for _, u := range friends {
			lost := u.unit.MaxHP - u.hp
			if lost > largest {
				largest = lost
				choices = []*fighter{u}
			} else if lost == largest {
				choices = append(choices, u)
			}
		}
		u := f.pick(choices)
		event.Target = u.unit.ID
		target = u
		f.heal(a, u, scaled(rule.Heal, a.unit.Stars), 2)
		goto end
	case "curse":
		target.curse = true
		f.record(2, a, target, 0, "curse")
		goto end
	}
	if rule.SwapBack || rule.TargetBack {
		backs := []*fighter{}
		for _, u := range f.alive(1 - a.side) {
			if u.unit.Slot < 3 {
				backs = append(backs, u)
			}
		}
		if back := f.pick(backs); back != nil {
			if rule.TargetBack {
				target = back
				event.Target = back.unit.ID
			} else {
				frontSlot := back.unit.Slot + 3
				for _, u := range f.alive(1 - a.side) {
					if u.unit.Slot == frontSlot {
						u.unit.Slot = back.unit.Slot
						break
					}
				}
				back.unit.Slot = frontSlot
				target = back
				event.Target = back.unit.ID
				f.record(0, a, back, 0, "move")
			}
		}
	}
	if rule.Mode == "melee" || rule.Mode == "ranged" {
		for _, u := range f.alive(target.side) {
			if u != target && u.rules != nil && u.rules.Bodyguard && !u.guardUsed && neighbors(u.unit.Slot, target.unit.Slot, "back") {
				u.guardUsed = true
				target = u
				event.Target = u.unit.ID
				f.record(0, u, u, 0, "guard")
				break
			}
		}
	}
	{
		base := a.hp
		counter := target.hp
		power := rule.Power
		if a.curse {
			power = power / 2
			a.curse = false
		}
		total := percent(base, power)
		if a.turn == 1 && a.rules != nil {
			total += scaled(a.rules.FirstBonus, a.unit.Stars)
		}
		execute := a.rules != nil && a.rules.Execute > 0 && !a.executeUsed && target.hp*100 <= target.unit.MaxHP*a.rules.Execute
		if execute {
			a.executeUsed = true
			total = target.hp
			rule.Pierce = 100
			rule.Hits = 1
			rule.HitFrames = []int{2}
			event.Animation = "attack03"
			event.Mode = "execute"
		}
		if !execute && a.rules != nil && a.rules.Finisher != "" {
			effective := total - min(target.armor, total*(100-rule.Pierce)/100)
			if effective >= target.hp {
				event.Animation = a.rules.Finisher
				if a.rules.FinisherFrames > 0 {
					rule.Frames = a.rules.FinisherFrames
				}
				if len(a.rules.FinisherHitFrames) > 0 {
					rule.HitFrames = a.rules.FinisherHitFrames
				}
				if a.rules.FinisherHits > 1 {
					rule.Hits = a.rules.FinisherHits
					total = percent(base, 120)
					if len(rule.HitFrames) != rule.Hits {
						rule.HitFrames = []int{3, 7}
					}
					rule.Frames = max(rule.Frames, rule.HitFrames[len(rule.HitFrames)-1]+1)
				}
			}
		}
		if target.fire > 0 {
			total += target.fire
			target.fire = 0
		}
		extra := f.splash(target, rule.Splash)
		if a.unit.Stars < rule.MinStars {
			extra = nil
		}
		stolen := 0
		hits := max(1, rule.Hits)
		lastFrame := 2
		for h := 0; h < hits && target.hp > 0; h++ {
			frame := 2 + h*3
			if h < len(rule.HitFrames) {
				frame = rule.HitFrames[h]
			}
			lastFrame = frame
			n := total / hits
			if h < total%hits {
				n++
			}
			kind := "damage"
			if execute {
				kind = "execute"
			}
			stolen += f.damage(a, target, n, rule.Pierce, frame, kind)
			event.Damage += n
			if h == 0 {
				for _, u := range extra {
					f.damage(a, u, percent(base, rule.SplashPower), rule.Pierce, frame, "splash")
					f.status(u, rule.SplashStatus, a.unit.Stars)
				}
			}
		}
		if rule.Mode == "melee" {
			avoid := a.rules != nil && a.rules.KillNoCounter && !a.killUsed && target.hp == 0
			if avoid {
				a.killUsed = true
			} else {
				if a.fire > 0 {
					counter += a.fire
					a.fire = 0
				}
				f.damage(target, a, counter, 0, lastFrame, "counter")
			}
		}
		f.status(target, rule.Status, a.unit.Stars)
		if rule.AntiHeal && target.hp > 0 {
			target.antiHeal = true
		}
		f.record(lastFrame, a, target, 0, "status")
		if a.hp > 0 {
			if rule.Lifesteal > 0 {
				f.heal(a, a, min(stolen, scaled(rule.Lifesteal, a.unit.Stars)), lastFrame)
			}
			if rule.TeamHeal > 0 {
				for _, u := range f.alive(a.side) {
					f.heal(a, u, scaled(rule.TeamHeal, a.unit.Stars), lastFrame)
				}
			}
		}
		if target.hp == 0 && a.rules != nil {
			if a.rules.KillStatus != "" && !a.killUsed {
				a.killUsed = true
				for _, u := range f.splash(target, a.rules.KillSplash) {
					f.status(u, a.rules.KillStatus, a.unit.Stars)
					f.record(lastFrame, a, u, 0, "status")
				}
			}
			if a.rules.Suicide && a.hp > 0 {
				f.damage(a, a, a.hp+a.armor, 100, lastFrame, "suicide")
			}
		}
	}
end:
	f.endAction(a, max(2, rule.Frames-1))
	if grace {
		a.grace = false
	}
	if curAnti {
		a.antiHeal = false
	}
	f.record(max(2, rule.Frames-1), a, a, 0, "action_end")
	event.TargetHP = target.hp
	event.Dead = target.hp == 0
	f.plan.Events = append(f.plan.Events, event)
}
func (f *fight) endAction(a *fighter, frame int) {
	if a.hp > 0 && a.poisonTicks > 0 {
		f.damage(a, a, a.poison, 100, frame, "poison")
		a.poisonTicks--
		if a.poisonTicks == 0 {
			a.poison = 0
		}
	}
}
func (r *Room) startEffects(now int64) *Plan {
	// Keep legacy external catalogs working; the shipped catalog uses HP = attack.
	modern := false
	for _, p := range r.State.Players {
		for _, u := range p.Units {
			if Character(u.Kind).Combat != nil {
				modern = true
			}
		}
	}
	if !modern {
		return r.startLegacy(now)
	}
	f := &fight{r: r, plan: &Plan{Round: r.State.Round, StartMS: now + 1500, Units: []CombatUnit{}, Events: []CombatEvent{}}}
	for side, p := range r.State.Players {
		for i := range p.Units {
			u := &p.Units[i]
			u.Veteran = true
			s := StatsFor(u.Kind, u.Stars)
			c := CombatUnit{Stars: starCount(u.Stars), ID: fmt.Sprintf("%s:%d", p.Team, u.Token), Team: p.Team, Token: u.Token, Kind: u.Kind, Slot: u.Slot, MaxHP: s.HP, InitialHP: s.HP, Attack: s.HP, Armor: s.Armor, Speed: 10}
			f.units = append(f.units, &fighter{unit: c, hp: c.MaxHP, armor: c.Armor, side: side, rules: Character(u.Kind).Combat})
			f.plan.Units = append(f.plan.Units, c)
		}
	}
	at := f.plan.StartMS
	side := 0
	for f.step = 0; f.step < 512 && len(f.alive(0)) > 0 && len(f.alive(1)) > 0; f.step++ {
		choices := []*fighter{}
		for _, u := range f.alive(side) {
			if u.canAct <= f.step {
				choices = append(choices, u)
			}
		}
		if a := f.pick(choices); a != nil {
			target := f.pick(f.alive(1 - side))
			f.action(a, target, at)
			frames := 8
			if a.rules != nil {
				frames = a.rules.Actions[(a.turn-1)%len(a.rules.Actions)].Frames
			}
			if a.rules != nil {
				frames = max(frames, a.rules.FinisherFrames)
			}
			at += max(int64(3700), int64(frames*1000/12+2200))
		}
		side = 1 - side
	}
	f.plan.Winner = "DRAW"
	if len(f.alive(0)) > 0 && len(f.alive(1)) == 0 {
		f.plan.Winner = "A"
	} else if len(f.alive(1)) > 0 && len(f.alive(0)) == 0 {
		f.plan.Winner = "B"
	}
	if f.plan.Winner != "DRAW" {
		winner := 0
		if f.plan.Winner == "B" {
			winner = 1
		}
		f.plan.PlayerDamage = r.State.Players[winner].ShopLevel
		for _, u := range f.alive(winner) {
			if !u.unit.Summoned {
				f.plan.PlayerDamage += u.unit.Stars
			}
		}
	}
	f.plan.EndMS = at
	r.State.Phase = "battle"
	r.State.DeadlineMS = 0
	r.State.Battle = f.plan
	r.State.Revision++
	return f.plan
}
