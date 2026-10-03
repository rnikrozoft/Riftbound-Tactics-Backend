package main

import (
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"testing"
)

func TestRecipientFormationPrivacy(t *testing.T) {
	room := game.New("123456", "owner", 1)
	if err := room.Reserve("opponent"); !err {
		t.Fatal(err)
	}
	room.State.Players[0].Hand = []game.Card{{Token: 3, Kind: 4}}
	room.State.Players[1].Hand = []game.Card{{Token: 4, Kind: 2}, {Token: 5, Kind: 1}}
	room.State.Players[0].Units = []game.Unit{{Token: 1, Slot: 0}}
	room.State.Players[1].Units = []game.Unit{{Token: 2, Slot: 2}}
	for _, phase := range []string{"waiting", "preparation", "battle", "finished"} {
		room.State.Phase = phase
		for i, id := range []string{"owner", "opponent"} {
			view := recipientState(room.State, id)
			if len(view.Players[i].Units) != 1 {
				t.Fatal("own formation hidden")
			}
			expectedCount := 0
			if phase == "battle" || phase == "finished" {
				expectedCount = len(room.State.Players[1-i].Hand)
			}
			if view.Players[1-i].HandCount != expectedCount || len(view.Players[1-i].Hand) != 0 {
				t.Fatal("hand count incorrect or card identities leaked")
			}
			want := 1
			if phase == "waiting" || phase == "preparation" {
				want = 0
			}
			if len(view.Players[1-i].Units) != want {
				t.Fatalf("phase %s leaked or hid opponent", phase)
			}
			if len(room.State.Players[0].Units) != 1 || len(room.State.Players[1].Units) != 1 {
				t.Fatal("projection mutated authoritative state")
			}
		}
	}
}
