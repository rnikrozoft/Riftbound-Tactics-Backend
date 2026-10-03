package game

import (
	"errors"
	"fmt"
	"math/rand"
)

const ShopLimit = 5
const StartingCoins = 4
const CardPrice = 2
const CardKinds = 60
const MaxShopLevel = 6
const MaxPlayerHP = 30
const RerollCost = 2

func RoundIncome(round int) int {
	incomes := [...]int{4, 6, 9, 12, 15, 18, 20}
	if round < 1 {
		return 0
	}
	if round > len(incomes) {
		return 20
	}
	return incomes[round-1]
}
func CardCost(kind int) int {
	if kind < 30 {
		return 2 + kind/6
	}
	if kind < 40 {
		return 2 + (kind-30)/2
	}
	return 2 + (kind-40)/4
}
func ShopSlots(level int) int {
	switch level {
	case 2:
		return 3
	case 3, 4:
		return 4
	case 5:
		return 5
	default:
		return ShopLimit
	}
}
func UpgradeBaseCost(level int) int {
	switch level {
	case 2:
		return 2
	case 3:
		return 8
	case 4:
		return 10
	case 5:
		return 14
	default:
		return 0
	}
}

const HandLimit = 10
const FieldLimit = 6
const PreparationMS int64 = 60000
const StepMS int64 = 2400

type Card struct {
	Veteran bool `json:"veteran"`
	Stars   int  `json:"stars"`
	Paid    int  `json:"paid"`
	Token   int  `json:"token"`
	Kind    int  `json:"kind"`
	Price   int  `json:"price"`
}
type Unit struct {
	Stars         int  `json:"stars"`
	Token         int  `json:"token"`
	Kind          int  `json:"kind"`
	Slot          int  `json:"slot"`
	Veteran       bool `json:"veteran"`
	PurchasePrice int  `json:"purchase_price"`
}
type Player struct {
	DeckName        string         `json:"deck_name"`
	HP              int            `json:"hp"`
	LastDamage      int            `json:"last_damage"`
	UserID          string         `json:"user_id"`
	Team            string         `json:"team"`
	Connected       bool           `json:"connected"`
	Ready           bool           `json:"ready"`
	Coins           int            `json:"coins"`
	ShopLocked      bool           `json:"shop_locked"`
	ShopLevel       int            `json:"shop_level"`
	UpgradeCost     int            `json:"upgrade_cost"`
	RemainingCopies [CardKinds]int `json:"-"`
	Offers          []Card         `json:"offers"`
	Hand            []Card         `json:"hand"`
	HandCount       int            `json:"hand_count"`
	Units           []Unit         `json:"units"`
}
type Action struct {
	Type     string `json:"type"`
	Token    int    `json:"token"`
	Slot     int    `json:"slot"`
	Round    int    `json:"round"`
	Sequence int64  `json:"sequence"`
}
type CombatUnit struct {
	Stars  int    `json:"stars"`
	ID     string `json:"id"`
	Team   string `json:"team"`
	Token  int    `json:"token"`
	Kind   int    `json:"kind"`
	Slot   int    `json:"slot"`
	MaxHP  int    `json:"max_hp"`
	Attack int    `json:"attack"`
	Speed  int    `json:"speed"`
}
type CombatEvent struct {
	Index    int    `json:"index"`
	Attacker string `json:"attacker"`
	Target   string `json:"target"`
	Damage   int    `json:"damage"`
	TargetHP int    `json:"target_hp"`
	Dead     bool   `json:"dead"`
	AtMS     int64  `json:"at_ms"`
}
type Plan struct {
	PlayerDamage int           `json:"player_damage"`
	Round        int           `json:"round"`
	StartMS      int64         `json:"start_ms"`
	EndMS        int64         `json:"end_ms"`
	Winner       string        `json:"winner"`
	Units        []CombatUnit  `json:"units"`
	Events       []CombatEvent `json:"events"`
}
type State struct {
	Winner     string     `json:"winner"`
	Code       string     `json:"code"`
	Phase      string     `json:"phase"`
	Round      int        `json:"round"`
	DeadlineMS int64      `json:"deadline_ms"`
	ServerMS   int64      `json:"server_ms"`
	Revision   int64      `json:"revision"`
	Players    [2]*Player `json:"players"`
	Battle     *Plan      `json:"battle,omitempty"`
}
type Room struct {
	State  State
	rng    *rand.Rand
	serial int
}

func New(code, creator string, seed int64) *Room {
	return &Room{State: State{Code: code, Phase: "waiting", Players: [2]*Player{newPlayer(creator, "A"), nil}}, rng: rand.New(rand.NewSource(seed))}
}
func newPlayer(id, team string) *Player {
	p := &Player{UserID: id, Team: team, HP: MaxPlayerHP, ShopLevel: 2, UpgradeCost: 2, Offers: []Card{}, Hand: []Card{}, Units: []Unit{}}
	deck := DefaultDeck()
	p.DeckName = deck.Name
	for _, card := range deck.Cards {
		p.RemainingCopies[card.Kind] = card.Copies
	}
	return p
}
func (r *Room) Player(id string) *Player {
	for _, p := range r.State.Players {
		if p != nil && p.UserID == id {
			return p
		}
	}
	return nil
}
func (r *Room) Reserve(id string) bool {
	if r.Player(id) != nil {
		return true
	}
	if r.State.Phase != "waiting" || r.State.Players[1] != nil {
		return false
	}
	r.State.Players[1] = newPlayer(id, "B")
	return true
}
func (r *Room) Connect(id string, now int64) {
	p := r.Player(id)
	if p == nil {
		return
	}
	p.Connected = true
	if r.State.Phase == "waiting" && r.State.Players[0].Connected && r.State.Players[1] != nil && r.State.Players[1].Connected {
		r.Prepare(now)
	}
	r.State.Revision++
}
func (r *Room) Disconnect(id string) {
	if p := r.Player(id); p != nil {
		p.Connected = false
		r.State.Revision++
	}
}
func (r *Room) Prepare(now int64) {
	if r.State.Phase == "game_over" {
		return
	}
	r.State.Round++
	r.State.Phase = "preparation"
	r.State.DeadlineMS = now + PreparationMS
	r.State.Battle = nil
	for _, p := range r.State.Players {
		p.Ready = false
		p.Coins += RoundIncome(r.State.Round)
		if r.State.Round > 1 {
			p.UpgradeCost -= 2
			if p.UpgradeCost < 0 {
				p.UpgradeCost = 0
			}
		}
		if !p.ShopLocked {
			r.rollShop(p)
		} else {
			r.fillShop(p)
		}
	}
	r.State.Revision++
}
func (r *Room) rollShop(p *Player) {
	p.Offers = p.Offers[:0]
	r.fillShop(p)
}
func (r *Room) fillShop(p *Player) {
	candidates := []int{}
	for kind, remaining := range p.RemainingCopies {
		if remaining <= 0 || CardCost(kind) > p.ShopLevel {
			continue
		}
		present := false
		for _, card := range p.Offers {
			if card.Kind == kind {
				present = true
				break
			}
		}
		if !present {
			candidates = append(candidates, kind)
		}
	}
	for len(p.Offers) < ShopSlots(p.ShopLevel) && len(candidates) > 0 {
		index := r.rng.Intn(len(candidates))
		kind := candidates[index]
		candidates = append(candidates[:index], candidates[index+1:]...)
		r.serial++
		p.Offers = append(p.Offers, Card{Token: r.serial, Kind: kind, Price: CardCost(kind)})
	}
}
func price(card Card) int {
	if card.Price > 0 {
		return card.Price
	}
	return CardCost(card.Kind)
}
func cardIndex(cards []Card, token int) int {
	for i, c := range cards {
		if c.Token == token {
			return i
		}
	}
	return -1
}
func unitIndex(units []Unit, token int) int {
	for i, u := range units {
		if u.Token == token {
			return i
		}
	}
	return -1
}
func (r *Room) Apply(id string, a Action, now int64) error {
	if r.State.Phase == "game_over" {
		return errors.New("match has ended")
	}
	p := r.Player(id)
	if p == nil || !p.Connected {
		return errors.New("not a room participant")
	}
	if a.Round != r.State.Round {
		return errors.New("stale round")
	}
	if r.State.Phase == "finished" && a.Type == "next" {
		p.Ready = true
		r.State.Revision++
		if r.State.Players[0].Ready && r.State.Players[1].Ready {
			r.Prepare(now)
		}
		return nil
	}
	if r.State.Phase != "preparation" || now >= r.State.DeadlineMS {
		return errors.New("preparation is locked")
	}
	ci := cardIndex(p.Hand, a.Token)
	ui := unitIndex(p.Units, a.Token)
	switch a.Type {
	case "buy":
		i := cardIndex(p.Offers, a.Token)
		if i < 0 {
			return errors.New("offer unavailable")
		}
		card := p.Offers[i]
		mergeUnit, mergeHand := -1, -1
		for j, u := range p.Units {
			if u.Kind == card.Kind {
				if starCount(u.Stars) >= 4 {
					return errors.New("character already has four stars")
				}
				mergeUnit = j
				break
			}
		}
		if mergeUnit < 0 {
			for j, c := range p.Hand {
				if c.Kind == card.Kind {
					if starCount(c.Stars) >= 4 {
						return errors.New("character already has four stars")
					}
					mergeHand = j
					break
				}
			}
		}
		if mergeHand < 0 && len(p.Hand) >= HandLimit {
			return errors.New("hand full")
		}
		cost := price(card)
		if p.Coins < cost {
			return errors.New("not enough coins")
		}
		if card.Kind < 0 || card.Kind >= CardKinds || p.RemainingCopies[card.Kind] <= 0 {
			return errors.New("card pool exhausted")
		}
		p.Coins -= cost
		p.RemainingCopies[card.Kind]--
		if mergeUnit >= 0 {
			u := p.Units[mergeUnit]
			p.Hand = append(p.Hand, Card{Token: u.Token, Kind: u.Kind, Stars: starCount(u.Stars) + 1, Price: cost, Paid: u.PurchasePrice + cost, Veteran: u.Veteran})
			p.Units = append(p.Units[:mergeUnit], p.Units[mergeUnit+1:]...)
		} else if mergeHand >= 0 {
			c := &p.Hand[mergeHand]
			c.Paid = investment(*c) + cost
			c.Stars = starCount(c.Stars) + 1
		} else {
			card.Price = cost
			card.Paid = cost
			card.Stars = 1
			p.Hand = append(p.Hand, card)
		}
		p.Offers = append(p.Offers[:i], p.Offers[i+1:]...)

	case "deploy":
		if ci < 0 || a.Slot < 0 || a.Slot >= FieldLimit || len(p.Units) >= FieldLimit {
			return errors.New("invalid deployment")
		}
		for _, u := range p.Units {
			if u.Slot == a.Slot {
				return errors.New("slot occupied")
			}
		}
		card := p.Hand[ci]
		p.Hand = append(p.Hand[:ci], p.Hand[ci+1:]...)
		p.Units = append(p.Units, Unit{Veteran: card.Veteran, Stars: starCount(card.Stars), Token: card.Token, Kind: card.Kind, Slot: a.Slot, PurchasePrice: investment(card)})
	case "move":
		if ui < 0 || a.Slot < 0 || a.Slot >= FieldLimit {
			return errors.New("invalid move")
		}
		old := p.Units[ui].Slot
		for i := range p.Units {
			if i != ui && p.Units[i].Slot == a.Slot {
				p.Units[i].Slot = old
			}
		}
		p.Units[ui].Slot = a.Slot
	case "return":
		if ui < 0 || p.Ready || p.Units[ui].Veteran || len(p.Hand) >= HandLimit {
			return errors.New("unit cannot return")
		}
		unit := p.Units[ui]
		p.Units = append(p.Units[:ui], p.Units[ui+1:]...)
		p.Hand = append(p.Hand, Card{Veteran: unit.Veteran, Stars: starCount(unit.Stars), Paid: unit.PurchasePrice, Token: unit.Token, Kind: unit.Kind, Price: CardCost(unit.Kind)})
	case "sell":
		if ci >= 0 {
			p.Coins += investment(p.Hand[ci]) / 2
			p.Hand = append(p.Hand[:ci], p.Hand[ci+1:]...)
		} else if ui >= 0 {
			p.Coins += p.Units[ui].PurchasePrice / 2
			p.Units = append(p.Units[:ui], p.Units[ui+1:]...)
		} else {
			return errors.New("card not owned")
		}
	case "upgrade":
		if p.ShopLevel >= MaxShopLevel {
			return errors.New("shop is already at maximum level")
		}
		if p.Coins < p.UpgradeCost {
			return errors.New("not enough coins")
		}
		p.Coins -= p.UpgradeCost
		p.ShopLevel++
		p.UpgradeCost = UpgradeBaseCost(p.ShopLevel)
	case "reroll":
		if p.ShopLocked {
			return errors.New("unlock shop before rerolling")
		}
		if p.Coins < RerollCost {
			return errors.New("not enough coins")
		}
		p.Coins -= RerollCost
		r.rollShop(p)
	case "lock":
		p.ShopLocked = !p.ShopLocked
	case "ready":
		p.Ready = true
	default:
		return errors.New("unknown action")
	}
	r.State.Revision++
	return nil
}
func (r *Room) ShouldStart(now int64) bool {
	return r.State.Phase == "preparation" && (now >= r.State.DeadlineMS || (r.State.Players[0].Ready && r.State.Players[1].Ready))
}
func (r *Room) Start(now int64) *Plan {
	plan := &Plan{Round: r.State.Round, StartMS: now + 1500, Units: []CombatUnit{}, Events: []CombatEvent{}}
	var alive [2][]int
	hp := map[int]int{}
	for side, p := range r.State.Players {
		for i := range p.Units {
			u := &p.Units[i]
			u.Veteran = true
			index := len(plan.Units)
			stars := u.Stars
			if stars < 1 {
				stars = 1
			}
			plan.Units = append(plan.Units, CombatUnit{Stars: stars, ID: fmt.Sprintf("%s:%d", p.Team, u.Token), Team: p.Team, Token: u.Token, Kind: u.Kind, Slot: u.Slot, MaxHP: 100 * stars, Attack: 30 * stars, Speed: 10 + 2*(stars-1)})
			alive[side] = append(alive[side], index)
			hp[index] = 100 * stars
		}
	}
	side := 0
	nextEventMS := plan.StartMS
	for len(alive[0]) > 0 && len(alive[1]) > 0 {
		attacker := alive[side][r.rng.Intn(len(alive[side]))]
		targetIndex := r.rng.Intn(len(alive[1-side]))
		target := alive[1-side][targetIndex]
		damage := (25 + r.rng.Intn(16)) * plan.Units[attacker].Stars
		hp[target] -= damage
		if hp[target] < 0 {
			hp[target] = 0
		}
		dead := hp[target] == 0
		plan.Events = append(plan.Events, CombatEvent{Index: len(plan.Events), Attacker: plan.Units[attacker].ID, Target: plan.Units[target].ID, Damage: damage, TargetHP: hp[target], Dead: dead, AtMS: nextEventMS})
		nextEventMS += StepMS * 10 / int64(plan.Units[attacker].Speed)
		if dead {
			alive[1-side] = append(alive[1-side][:targetIndex], alive[1-side][targetIndex+1:]...)
		}
		side = 1 - side
	}
	plan.Winner = "DRAW"
	if len(alive[0]) > 0 {
		plan.Winner = "A"
	} else if len(alive[1]) > 0 {
		plan.Winner = "B"
	}
	if plan.Winner != "DRAW" {
		winnerSide := 0
		if plan.Winner == "B" {
			winnerSide = 1
		}
		plan.PlayerDamage = r.State.Players[winnerSide].ShopLevel
		for _, index := range alive[winnerSide] {
			plan.PlayerDamage += plan.Units[index].Stars
		}
	}
	plan.EndMS = nextEventMS
	r.State.Phase = "battle"
	r.State.DeadlineMS = 0
	r.State.Battle = plan
	r.State.Revision++
	return plan
}
func (r *Room) Finish(now int64) bool {
	if r.State.Phase == "battle" && r.State.Battle != nil && now >= r.State.Battle.EndMS {
		r.State.Phase = "finished"
		for _, p := range r.State.Players {
			p.LastDamage = 0
		}
		plan := r.State.Battle
		if plan.Winner != "DRAW" {
			for _, p := range r.State.Players {
				if p.Team == plan.Winner {
					continue
				}
				p.LastDamage = plan.PlayerDamage
				p.HP -= plan.PlayerDamage
				if p.HP <= 0 {
					p.HP = 0
					r.State.Phase = "game_over"
					r.State.Winner = plan.Winner
				}
			}
		}
		for _, p := range r.State.Players {
			p.Ready = false
		}
		r.State.Revision++
		return true
	}
	return false
}

func starCount(stars int) int {
	if stars < 1 {
		return 1
	}
	if stars > 4 {
		return 4
	}
	return stars
}
func investment(c Card) int {
	if c.Paid > 0 {
		return c.Paid
	}
	return price(c)
}

// Leave time for the final replay event, then advance without client acknowledgements.
func (r *Room) AutoAdvance(now int64) bool {
	if r.State.Phase != "finished" || r.State.Battle == nil || now < r.State.Battle.EndMS+2000 {
		return false
	}
	r.Prepare(now)
	return true
}
