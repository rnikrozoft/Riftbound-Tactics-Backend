package game

import (
	"errors"
	"fmt"
	"math/rand"
)

const LeagueSeats = 6
const MaxLeagueBattleMS int64 = 30_000

// Immutable content is prepared once at runtime startup; only player state is
// allocated per match. Policies use normal action validation and no extra income.
var botDeckTemplates = func() []Deck {
	decks := []Deck{}
	for excluded := 0; excluded < 4; excluded++ {
		d := Deck{Name: "Bot expedition"}
		groups := map[int]bool{4: true}
		for g := 0; g < 4; g++ {
			if g != excluded {
				d.Heroes = append(d.Heroes, g)
				groups[g] = true
			}
		}
		for k := 0; k < CardKinds; k++ {
			if groups[CardGroup(k)] && (k < 40 || (k-40)%4 < 2) {
				d.Cards = append(d.Cards, DeckCard{Kind: k, Copies: 4})
			}
		}
		decks = append(decks, d)
	}
	return decks
}()

type Seat struct {
	ID     string `json:"id"`
	Deck   Deck   `json:"deck"`
	Bot    bool   `json:"bot"`
	Rating int    `json:"rating"`
}
type Standing struct {
	UserID string `json:"user_id"`
	HP     int    `json:"hp"`
	Bot    bool   `json:"bot"`
	Rating int    `json:"rating"`
	Place  int    `json:"place"`
	Ready  bool   `json:"ready"`
}
type League struct {
	Players    [LeagueSeats]*Player
	Seats      [LeagueSeats]Seat
	Place      [LeagueSeats]int
	Phase      string
	Round      int
	DeadlineMS int64
	Revision   int64
	Winner     string
	Duels      []*Room
	Ghosts     []Player
	seen       [LeagueSeats][LeagueSeats]int
	last       [LeagueSeats]int
	ghostRound [LeagueSeats]int
	rng        *rand.Rand
	serial     int
	advanceMS  int64
}

func NewLeague(seats []Seat, seed int64) (*League, error) {
	if len(seats) < 1 || len(seats) > LeagueSeats {
		return nil, errors.New("invalid participant count")
	}
	l := &League{Phase: "waiting", rng: rand.New(rand.NewSource(seed))}
	ids := map[string]bool{}
	for i := 0; i < LeagueSeats; i++ {
		s := Seat{ID: fmt.Sprintf("bot:%d:%d", seed, i+1), Deck: botDeckTemplates[i%len(botDeckTemplates)], Bot: true, Rating: 1000}
		if i < len(seats) {
			s = seats[i]
		} else {
			s.Rating = 0
			for _, human := range seats {
				s.Rating += human.Rating
			}
			s.Rating /= len(seats)
		}
		if s.ID == "" || ids[s.ID] {
			return nil, errors.New("duplicate participant")
		}
		ids[s.ID] = true
		if err := ValidateDeck(s.Deck); err != nil {
			return nil, err
		}
		p := newPlayer(s.ID, "A")
		p.Connected = s.Bot
		p.DeckName = s.Deck.Name
		p.RemainingCopies = [CardKinds]int{}
		for _, c := range s.Deck.Cards {
			p.RemainingCopies[c.Kind] = c.Copies
		}
		l.Players[i] = p
		l.Seats[i] = s
		l.last[i] = -1
	}
	return l, nil
}
func clonePlayer(p *Player) Player {
	q := *p
	q.Units = append([]Unit{}, p.Units...)
	q.Hand = append([]Card{}, p.Hand...)
	q.Offers = append([]Card{}, p.Offers...)
	return q
}
func (l *League) Index(id string) int {
	for i, p := range l.Players {
		if p.UserID == id {
			return i
		}
	}
	return -1
}
func (l *League) Roster() []Standing {
	out := []Standing{}
	for i, p := range l.Players {
		out = append(out, Standing{p.UserID, p.HP, l.Seats[i].Bot, l.Seats[i].Rating, l.Place[i], p.Ready})
	}
	return out
}
func (l *League) Connected(id string, on bool) {
	if i := l.Index(id); i >= 0 {
		l.Players[i].Connected = on
		l.Revision++
	}
}
func (l *League) Alive() []int {
	out := []int{}
	for i, p := range l.Players {
		if p.HP > 0 {
			out = append(out, i)
		}
	}
	return out
}

// Enumerate at most 15 perfect matchings. Strongly avoid consecutive opponents,
// then minimize repeat encounters. Random tie-breaking prevents seat bias.
func (l *League) pairs(alive []int) [][2]int {
	best := int(^uint(0) >> 1)
	options := [][][2]int{}
	var visit func([]int, [][2]int, int)
	visit = func(left []int, pairs [][2]int, cost int) {
		if len(left) == 0 {
			if cost < best {
				best = cost
				options = nil
			}
			if cost == best {
				options = append(options, append([][2]int{}, pairs...))
			}
			return
		}
		a := left[0]
		for j := 1; j < len(left); j++ {
			b := left[j]
			c := l.seen[a][b] * 10
			if l.last[a] == b {
				c += 1000
			}
			rest := append([]int{}, left[1:j]...)
			rest = append(rest, left[j+1:]...)
			visit(rest, append(pairs, [2]int{a, b}), cost+c)
		}
	}
	visit(alive, nil, 0)
	if len(options) == 0 {
		return nil
	}
	return options[l.rng.Intn(len(options))]
}
func (l *League) Prepare(now int64) {
	alive := l.Alive()
	if len(alive) <= 1 {
		l.Phase = "game_over"
		if len(alive) == 1 {
			l.Winner = l.Players[alive[0]].UserID
			l.Place[alive[0]] = 1
		}
		l.Revision++
		return
	}
	l.Round++
	l.Phase = "preparation"
	l.DeadlineMS = now + PreparationMS
	l.Duels = nil
	ghostSeat := -1
	if len(alive)%2 == 1 {
		oldest := int(^uint(0) >> 1)
		candidates := []int{}
		for _, i := range alive {
			if l.ghostRound[i] < oldest {
				oldest = l.ghostRound[i]
				candidates = nil
			}
			if l.ghostRound[i] == oldest {
				candidates = append(candidates, i)
			}
		}
		ghostSeat = candidates[l.rng.Intn(len(candidates))]
		l.ghostRound[ghostSeat] = l.Round
		next := []int{}
		for _, i := range alive {
			if i != ghostSeat {
				next = append(next, i)
			}
		}
		alive = next
	}
	for _, pair := range l.pairs(alive) {
		a, b := pair[0], pair[1]
		l.seen[a][b]++
		l.seen[b][a]++
		l.last[a] = b
		l.last[b] = a
		l.addDuel(l.Players[a], l.Players[b], now)
	}
	if ghostSeat >= 0 {
		ghost := clonePlayer(&l.Ghosts[l.rng.Intn(len(l.Ghosts))])
		ghost.UserID = "ghost:" + ghost.UserID
		ghost.Connected = true
		ghost.HP = MaxPlayerHP
		ghost.Team = "B"
		l.addDuel(l.Players[ghostSeat], &ghost, now)
	}
	l.Revision++
}
func (l *League) addDuel(a, b *Player, now int64) {
	a.Team = "A"
	b.Team = "B"
	r := &Room{rng: l.rng, serial: l.serial, State: State{Code: "MATCHMAKING", Phase: "finished", Round: l.Round - 1, Players: [2]*Player{a, b}}}
	// Ghost has a frozen formation. Income/shop changes affect only this private copy.
	r.Prepare(now)
	l.serial = r.serial
	l.Duels = append(l.Duels, r)
}
func (l *League) Duel(id string) *Room {
	for _, r := range l.Duels {
		if r.Player(id) != nil {
			return r
		}
	}
	return nil
}
func (l *League) Apply(id string, a Action, now int64) error {
	i := l.Index(id)
	if i < 0 || l.Players[i].HP <= 0 {
		return errors.New("player eliminated")
	}
	if l.Phase != "preparation" {
		return errors.New("preparation is locked")
	}
	if a.Type == "next" {
		return errors.New("rounds advance automatically")
	}
	r := l.Duel(id)
	if r == nil {
		return errors.New("no active duel")
	}
	r.serial = l.serial
	if err := r.Apply(id, a, now); err != nil {
		return err
	}
	l.serial = r.serial
	l.Revision++
	return nil
}

// Bots use the same economy/action validation as humans; each preparation is bounded.
func (l *League) PlayBots(now int64) {
	if l.Phase != "preparation" {
		return
	}
	for i, p := range l.Players {
		if p.HP <= 0 || (!l.Seats[i].Bot && p.Connected) || p.Ready {
			continue
		}
		connected := p.Connected
		p.Connected = true
		apply := func(t string, token, slot int) bool {
			return l.Apply(p.UserID, Action{Type: t, Token: token, Slot: slot, Round: l.Round}, now) == nil
		}
		deploy := func() {
			for len(p.Hand) > 0 && len(p.Units) < FieldLimit {
				slot := 0
				for ; slot < FieldLimit; slot++ {
					used := false
					for _, u := range p.Units {
						if u.Slot == slot {
							used = true
						}
					}
					if !used {
						break
					}
				}
				if !apply("deploy", p.Hand[0].Token, slot) {
					break
				}
			}
		}
		deploy()
		if l.Round > 1 && p.ShopLevel < MaxShopLevel && p.Coins >= p.UpgradeCost+2 {
			apply("upgrade", 0, 0)
		}
		for step := 0; step < 24; step++ {
			token := 0
			for _, c := range p.Offers {
				if c.Price > p.Coins {
					continue
				}
				if len(p.Units) < FieldLimit {
					token = c.Token
					break
				}
				for _, u := range p.Units {
					if u.Kind == c.Kind && starCount(u.Stars) < 4 {
						token = c.Token
						break
					}
				}
				if token != 0 {
					break
				}
			}
			if token != 0 && apply("buy", token, 0) {
				deploy()
				continue
			}
			if p.Coins >= RerollCost+2 {
				if p.ShopLocked {
					apply("lock", 0, 0)
				}
				if apply("reroll", 0, 0) {
					continue
				}
			}
			break
		}
		apply("ready", 0, 0)
		p.Connected = connected
	}
}
func (l *League) Tick(now int64) {
	if l.Phase == "preparation" {
		ready := true
		for _, i := range l.Alive() {
			if !l.Players[i].Ready {
				ready = false
			}
		}
		if ready || now >= l.DeadlineMS {
			l.DeadlineMS = now + MaxLeagueBattleMS
			for _, r := range l.Duels {
				r.Start(now)
				// The complete result stays intact; only presentation is capped.
				if r.State.Battle.EndMS > l.DeadlineMS {
					r.State.Battle.EndMS = l.DeadlineMS
				}
			}
			l.Phase = "battle"
			l.Revision++
		}
	}
	if l.Phase == "battle" {
		done := true
		end := int64(0)
		for _, r := range l.Duels {
			if r.State.Battle.EndMS > end {
				end = r.State.Battle.EndMS
			}
			if now < r.State.Battle.EndMS {
				done = false
			}
		}
		if done {
			before := len(l.Alive())
			for _, r := range l.Duels {
				r.Finish(now)
			}
			eliminated := []int{}
			for i, p := range l.Players {
				if p.HP == 0 && l.Place[i] == 0 {
					eliminated = append(eliminated, i)
					l.Ghosts = append(l.Ghosts, clonePlayer(p))
				}
			}
			// Simultaneous eliminations share a placement (no arbitrary seat advantage).
			for _, i := range eliminated {
				l.Place[i] = before - len(eliminated) + 1
			}
			l.Phase = "finished"
			l.advanceMS = end + 2000
			l.Revision++
			if len(l.Alive()) <= 1 {
				l.Prepare(now)
			}
		}
	}
	if l.Phase == "finished" && now >= l.advanceMS {
		l.Prepare(now)
	}
}
func (l *League) View(id string, now int64) State {
	i := l.Index(id)
	r := l.Duel(id)
	view := State{Code: "MATCHMAKING", Phase: l.Phase, Round: l.Round, DeadlineMS: l.DeadlineMS, ServerMS: now, Revision: l.Revision}
	if r != nil {
		view.Players = r.State.Players
		view.Battle = r.State.Battle
	} else if i >= 0 {
		view.Players[0] = l.Players[i]
	}
	if i >= 0 && l.Players[i].HP <= 0 && l.Phase != "game_over" {
		view.Phase = "eliminated"
		view.Battle = nil
	}
	if l.Phase == "game_over" {
		view.Winner = "B"
		if i >= 0 && l.Players[i].Team == "B" {
			view.Winner = "A"
		}
		if i >= 0 && l.Winner == id {
			view.Winner = l.Players[i].Team
		}
		if l.Winner == "" {
			view.Winner = "DRAW"
		}
	}
	return view
}
