package main

import (
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"testing"
)

func ratedLeague(t *testing.T) *game.League {
	t.Helper()
	seats := []game.Seat{}
	for i := 0; i < 6; i++ {
		seats = append(seats, game.Seat{ID: string(rune('a' + i)), Deck: game.DefaultDeck(), Rating: 1000})
	}
	l, err := game.NewLeague(seats, 1)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func TestPlacementMMREqualRatings(t *testing.T) {
	l := ratedLeague(t)
	want := []int{16, 10, 3, -3, -10, -16}
	sum := 0
	for i := range l.Place {
		l.Place[i] = i + 1
	}
	for i := range l.Place {
		d, r, e := placementMMR(l, i)
		if e != nil || !r || d != want[i] {
			t.Fatalf("place %d: %d %v %v", i+1, d, r, e)
		}
		sum += d
	}
	if sum != 0 {
		t.Fatal("equal-rating population should be balanced")
	}
}
func TestPlacementMMREarlyEliminationAndTies(t *testing.T) {
	l := ratedLeague(t)
	l.Place[5] = 6
	early, _, e := placementMMR(l, 5)
	if e != nil {
		t.Fatal(e)
	}
	for i := range l.Place {
		l.Place[i] = i + 1
	}
	late, _, _ := placementMMR(l, 5)
	if early != late || early != -16 {
		t.Fatal("early eliminated result must equal final result")
	}
	for i := range l.Place {
		l.Place[i] = 1
	}
	for i := range l.Place {
		d, _, _ := placementMMR(l, i)
		if d != 0 {
			t.Fatal("equal-rated tied players must not move")
		}
	}
	l.Place[0] = 0
	if _, _, err := placementMMR(l, 0); err == nil {
		t.Fatal("unresolved placement rated")
	}
}
func TestPlacementMMRStrengthAndBots(t *testing.T) {
	l := ratedLeague(t)
	for i := range l.Place {
		l.Place[i] = i + 1
	}
	base, _, _ := placementMMR(l, 0)
	l.Seats[0].Rating = 800
	upset, _, _ := placementMMR(l, 0)
	if upset <= base {
		t.Fatal("lower rating beating stronger humans must earn more")
	}
	l.Seats[0].Rating = 1200
	expected, _, _ := placementMMR(l, 0)
	if expected >= base {
		t.Fatal("strong player beating weaker humans must earn less")
	}
	for i := 1; i < 6; i++ {
		l.Seats[i].Bot = true
	}
	d, r, e := placementMMR(l, 0)
	if e != nil || r || d != 0 {
		t.Fatal("solo bot match must not change MMR")
	}
	l.Seats[1].Bot = false
	l.Seats[0].Rating = 1000
	l.Seats[1].Rating = 1000
	d, r, e = placementMMR(l, 0)
	if e != nil || !r || d != 16 {
		t.Fatal("two humans compare only to one another")
	}
	l.Seats[5].Rating = 9000
	d, _, _ = placementMMR(l, 0)
	if d != 16 {
		t.Fatal("bot rating must not affect skill score")
	}
	if _, _, err := placementMMR(l, 5); err == nil {
		t.Fatal("bot account must not be rated")
	}
}
