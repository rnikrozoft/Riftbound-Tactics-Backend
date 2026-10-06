package main

import (
	"context"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"testing"
)

type reconnectPresence struct {
	runtime.Presence
	user, session string
}

func (p reconnectPresence) GetUserId() string    { return p.user }
func (p reconnectPresence) GetSessionId() string { return p.session }

func TestLatestPresenceSurvivesDelayedOldLeave(t *testing.T) {
	l, err := game.NewLeague([]game.Seat{{ID: "human", Deck: game.DefaultDeck(), Rating: 1000}, {ID: "other", Deck: game.DefaultDeck(), Rating: 1000}}, 9)
	if err != nil {
		t.Fatal(err)
	}
	old := reconnectPresence{user: "human", session: "old"}
	latest := reconnectPresence{user: "human", session: "latest"}
	s := &leagueState{league: l, presences: map[string]runtime.Presence{"human": old}}
	l.Connected("human", true)
	d := &fakeKickDispatcher{fail: true}
	m := &leagueMatch{}
	_, accepted, _ := m.MatchJoinAttempt(context.Background(), nil, nil, nil, d, 0, s, latest, nil)
	if accepted || s.presences["human"] != old {
		t.Fatal("failed replacement must preserve the old presence and require retry")
	}
	d.fail = false
	_, accepted, _ = m.MatchJoinAttempt(context.Background(), nil, nil, nil, d, 0, s, latest, nil)
	if !accepted || len(d.kicked) != 1 {
		t.Fatal("newest session must replace the old one")
	}
	s.sequences = map[string]int64{"human": 99}
	m.MatchJoin(context.Background(), nil, nil, nil, d, 0, s, []runtime.Presence{latest})
	if s.sequences["human"] != 0 {
		t.Fatal("new socket inherited stale sequence")
	}
	m.MatchLeave(context.Background(), nil, nil, nil, d, 0, s, []runtime.Presence{old})
	if s.presences["human"] != latest || !l.Players[l.Index("human")].Connected {
		t.Fatal("delayed leave from old socket must not disconnect the latest session")
	}
}
