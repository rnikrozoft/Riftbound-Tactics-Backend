package game

import (
	"fmt"
	"testing"
)

func TestLeagueBattleCapAndSharedPreparation(t *testing.T) {
	seats := make([]Seat, LeagueSeats)
	for i := range seats {
		seats[i] = Seat{ID: fmt.Sprintf("human-%d", i), Deck: DefaultDeck(), Rating: 1000}
	}
	l, err := NewLeague(seats, 42)
	if err != nil {
		t.Fatal(err)
	}
	for _, seat := range seats {
		l.Connected(seat.ID, true)
	}
	l.Prepare(1000)
	l.Tick(l.DeadlineMS)
	start := l.Duels[0].State.Battle.StartMS - 1500
	if l.DeadlineMS != start+60_000 {
		t.Fatal("battle must publish a shared 60 second deadline")
	}
	// Model three presentation durations, including one exceeding the cap.
	l.Duels[0].State.Battle.EndMS = start + 20_000
	l.Duels[1].State.Battle.EndMS = start + MaxLeagueBattleMS
	l.Duels[2].State.Battle.EndMS = start + 25_000
	for _, r := range l.Duels {
		r.State.Battle.Winner = "DRAW"
	}
	l.Tick(start + 20_000)
	if l.Phase != "battle" {
		t.Fatal("early finisher must wait for other duels")
	}
	if err := l.Apply("human-0", Action{Type: "reroll", Round: l.Round}, start+20_000); err == nil {
		t.Fatal("shop must stay locked while another pair is fighting")
	}
	l.Tick(start + MaxLeagueBattleMS)
	if l.Phase != "finished" {
		t.Fatal("all duels must settle at cap")
	}
	l.Tick(start + MaxLeagueBattleMS + 2000)
	if l.Phase != "preparation" {
		t.Fatal("next preparation must open for all players")
	}
	deadline := start + MaxLeagueBattleMS + 2000 + PreparationMS
	for _, p := range l.Players {
		if l.View(p.UserID, deadline-PreparationMS).DeadlineMS != deadline {
			t.Fatal("all players must receive same full preparation deadline")
		}
	}
}

func TestLeagueBotDuelsDoNotDelayHumanNextRound(t *testing.T) {
	l := leagueForTest(t)
	l.Tick(l.DeadlineMS)
	start := l.DeadlineMS - MaxLeagueBattleMS
	human := l.Duel("human")
	for _, r := range l.Duels {
		r.State.Battle.EndMS = start + MaxLeagueBattleMS
		r.State.Battle.Winner = "A"
		r.State.Battle.PlayerDamage = 3
	}
	human.State.Battle.EndMS = start + 5000
	l.Tick(start + 4999)
	if l.Phase != "battle" {
		t.Fatal("human replay must finish first")
	}
	l.Tick(start + 5000)
	if l.Phase != "finished" {
		t.Fatal("bot replays must not delay settlement")
	}
	for _, r := range l.Duels {
		if r.State.Players[1].HP != 27 {
			t.Fatal("bot and human results must still apply once")
		}
	}
	l.Tick(start + 6999)
	if l.Phase != "finished" {
		t.Fatal("retain the two second result display")
	}
	l.Tick(start + 7000)
	if l.Phase != "preparation" || l.Round != 2 {
		t.Fatal("open the next round after human result display")
	}
}

func TestLeagueDisconnectedDuelsDoNotHoldRound(t *testing.T) {
	seats := make([]Seat, LeagueSeats)
	for i := range seats {
		seats[i] = Seat{ID: fmt.Sprintf("human-%d", i), Deck: DefaultDeck()}
	}
	l, err := NewLeague(seats, 42)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range seats {
		l.Connected(s.ID, true)
	}
	l.Prepare(1000)
	l.Tick(l.DeadlineMS)
	start := l.DeadlineMS - MaxLeagueBattleMS
	for _, r := range l.Duels {
		r.State.Battle.EndMS = start + 60_000
		r.State.Battle.Winner = "A"
		r.State.Battle.PlayerDamage = 3
	}
	l.Duels[0].State.Battle.EndMS = start + 5000
	for _, r := range l.Duels[1:] {
		for _, p := range r.State.Players {
			l.Connected(p.UserID, false)
		}
	}
	l.Tick(start + 5000)
	if l.Phase != "finished" {
		t.Fatal("disconnected human replays must not hold connected players")
	}
	for _, r := range l.Duels {
		if r.State.Players[1].HP != 27 {
			t.Fatal("all results must still settle")
		}
	}
	l.Tick(start + 7000)
	if l.Phase != "preparation" {
		t.Fatal("next round must open after result display")
	}
}

func TestLeagueEliminatedGhostDoesNotCountAsViewer(t *testing.T) {
	l := leagueForTest(t)
	l.Players[0].HP = 0
	ghost := clonePlayer(l.Players[0])
	ghost.HP = 30
	r := &Room{State: State{Players: [2]*Player{&ghost, l.Players[1]}}}
	if l.hasHumanParticipant(r) {
		t.Fatal("eliminated player's ghost must not hold replay time")
	}
}

func TestLeagueLongReplayCappedWithoutChangingResult(t *testing.T) {
	l := leagueForTest(t)
	// Durable formations make a longer computed fight than the presentation cap.
	for _, p := range l.Players {
		p.Units = []Unit{{Token: 100 + l.Index(p.UserID), Kind: 0, Stars: 4, Slot: 0}}
	}
	l.Tick(l.DeadlineMS)
	for _, r := range l.Duels {
		if r.State.Battle.EndMS > l.DeadlineMS {
			t.Fatal("replay exceeded battle deadline")
		}
	}
	r := l.Duels[0]
	r.State.Battle.Winner = "A"
	r.State.Battle.PlayerDamage = 7
	hp := r.State.Players[1].HP
	l.Tick(l.DeadlineMS)
	if r.State.Players[1].HP != hp-7 {
		t.Fatal("cutting animation must retain precomputed winner and damage")
	}
}

func leagueForTest(t *testing.T) *League {
	t.Helper()
	l, e := NewLeague([]Seat{{ID: "human", Deck: DefaultDeck(), Rating: 1000}}, 42)
	if e != nil {
		t.Fatal(e)
	}
	l.Connected("human", true)
	l.Prepare(1000)
	return l
}
func TestLeagueSeatsAndRotation(t *testing.T) {
	l := leagueForTest(t)
	if len(l.Roster()) != 6 || len(l.Duels) != 3 {
		t.Fatal("must have six participants and three pairs")
	}
	first := map[string]string{}
	for _, r := range l.Duels {
		a, b := r.State.Players[0].UserID, r.State.Players[1].UserID
		first[a] = b
		first[b] = a
	}
	l.Prepare(2000)
	for _, r := range l.Duels {
		a, b := r.State.Players[0].UserID, r.State.Players[1].UserID
		if first[a] == b {
			t.Fatal("avoidable consecutive opponent")
		}
	}
	for _, p := range l.Players {
		if p.Coins != 10 {
			t.Fatalf("income must carry over: %d", p.Coins)
		}
	}
	tokens := map[int]bool{}
	for _, p := range l.Players {
		for _, c := range p.Offers {
			if tokens[c.Token] {
				t.Fatal("tokens must remain unique across shops/rounds")
			}
			tokens[c.Token] = true
		}
	}
}
func TestLeagueGhostFrozenAndFair(t *testing.T) {
	l := leagueForTest(t)
	p := l.Players[5]
	p.Units = []Unit{{Token: 999, Kind: 0, Stars: 3, Slot: 0}}
	p.HP = 0
	l.Place[5] = 6
	l.Ghosts = append(l.Ghosts, clonePlayer(p))
	seen := map[string]bool{}
	for round := 0; round < 5; round++ {
		l.Prepare(int64(2000 + round))
		ghostCount := 0
		for _, r := range l.Duels {
			b := r.State.Players[1]
			if len(b.UserID) > 6 && b.UserID[:6] == "ghost:" {
				ghostCount++
				id := r.State.Players[0].UserID
				if seen[id] {
					t.Fatal("ghost assignment must rotate")
				}
				seen[id] = true
				if b.Units[0].Stars != 3 {
					t.Fatal("ghost must preserve eliminated formation")
				}
				b.Units[0].Stars = 1
			}
		}
		if ghostCount != 1 {
			t.Fatal("odd population requires one ghost duel")
		}
	}
	if l.Ghosts[0].Units[0].Stars != 3 || p.Units[0].Stars != 3 {
		t.Fatal("copy must not mutate stored ghost or eliminated player")
	}
}
func TestLeagueEliminationAndLastSurvivor(t *testing.T) {
	l := leagueForTest(t)
	for n := 0; n < 60 && l.Phase != "game_over"; n++ {
		now := l.DeadlineMS - 100
		if l.Phase == "preparation" {
			l.PlayBots(now)
			if l.Players[0].HP > 0 {
				if e := l.Apply("human", Action{Type: "ready", Round: l.Round}, now); e != nil {
					t.Fatal(e)
				}
			}
			l.Tick(now)
		}
		if l.Phase == "battle" {
			end := int64(0)
			for _, r := range l.Duels {
				if r.State.Battle.EndMS > end {
					end = r.State.Battle.EndMS
				}
			}
			l.Tick(end)
			if l.Phase == "finished" {
				l.Tick(end + 2000)
			}
		}
	}
	if l.Phase != "game_over" || len(l.Alive()) != 1 || l.Winner == "" {
		t.Fatalf("must finish with one survivor: %s %v", l.Phase, l.Alive())
	}
	if l.Players[0].HP != 0 || l.Place[0] < 2 {
		t.Fatal("empty human field should be eliminated")
	}
	if e := l.Apply("human", Action{Type: "buy", Round: l.Round}, 0); e == nil {
		t.Fatal("eliminated player cannot act")
	}
	for _, p := range l.Players {
		if p.HP < 0 {
			t.Fatal("HP must clamp to zero")
		}
	}
}
func TestLeagueGlobalReadyAndDeadline(t *testing.T) {
	l := leagueForTest(t)
	now := int64(1100)
	l.PlayBots(now)
	l.Tick(now)
	if l.Phase != "preparation" {
		t.Fatal("bots ready cannot start while human preparing")
	}
	if e := l.Apply("human", Action{Type: "ready", Round: l.Round}, now); e != nil {
		t.Fatal(e)
	}
	l.Tick(now)
	if l.Phase != "battle" {
		t.Fatal("all six ready must start three battles together")
	}
	l = leagueForTest(t)
	l.Tick(l.DeadlineMS)
	if l.Phase != "battle" {
		t.Fatal("deadline must start battle")
	}
}
