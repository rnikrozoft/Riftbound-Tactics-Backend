package main

import (
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"testing"
)

func TestMatchWinRequiresTerminalWinner(t *testing.T) {
	r := game.New("123456", "a", 42)
	r.Reserve("b")
	r.State.Winner = "A"
	for _, phase := range []string{"waiting", "preparation", "battle", "finished"} {
		r.State.Phase = phase
		if matchWinnerID(r.State) != "" {
			t.Fatal("round win awarded")
		}
	}
	r.State.Phase = "game_over"
	r.State.Players[1].HP = 0
	if matchWinnerID(r.State) != "a" {
		t.Fatal("winner not identified")
	}
	r.State.Winner = "DRAW"
	if matchWinnerID(r.State) != "" {
		t.Fatal("draw rewarded")
	}
	r.State.Winner = "B"
	if matchWinnerID(r.State) != "" {
		t.Fatal("eliminated player rewarded")
	}
}
