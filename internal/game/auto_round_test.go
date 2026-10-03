package game

import "testing"

func TestAutomaticNextRound(t *testing.T) {
	r := setup(t)
	r.Player("a").Coins = 2
	r.Player("b").Coins = 3
	plan := r.Start(1100)
	r.Finish(plan.EndMS)
	if r.AutoAdvance(plan.EndMS + 1999) {
		t.Fatal("advanced before replay pause")
	}
	if !r.AutoAdvance(plan.EndMS+2000) || r.State.Round != 2 || r.State.Phase != "preparation" {
		t.Fatal("next round needs no client actions")
	}
	if r.Player("a").Coins != 8 || r.Player("b").Coins != 9 {
		t.Fatal("income must accumulate once")
	}
	if r.AutoAdvance(plan.EndMS + 3000) {
		t.Fatal("advanced twice")
	}
	r.State.Phase = "game_over"
	if r.AutoAdvance(plan.EndMS + 99999) {
		t.Fatal("terminal match advanced")
	}
}
