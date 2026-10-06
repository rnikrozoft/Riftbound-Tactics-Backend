package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"sync"
	"testing"
	"time"
)

func queueContext(id string) context.Context {
	ctx := context.WithValue(context.Background(), runtime.RUNTIME_CTX_USER_ID, id)
	return context.WithValue(ctx, runtime.RUNTIME_CTX_SESSION_ID, "session-"+id)
}
func resetQueue(t *testing.T) {
	t.Helper()
	queue.Lock()
	queue.entries = map[string]*queueEntry{}
	queue.Unlock()
	t.Cleanup(func() { queue.Lock(); queue.entries = map[string]*queueEntry{}; queue.Unlock() })
}
func TestMatchmakerUsesTrustedRank(t *testing.T) {
	resetQueue(t)
	queue.entries["p"] = &queueEntry{Seat: game.Seat{ID: "p", Rating: 1200}, Session: "session-p", Since: time.Now().UnixMilli()}
	add := &rtapi.MatchmakerAdd{MinCount: 1, MaxCount: 99, Query: "*", NumericProperties: map[string]float64{"rating": 99999}}
	envelope := &rtapi.Envelope{Message: &rtapi.Envelope_MatchmakerAdd{MatchmakerAdd: add}}
	if _, err := beforeMatchmaker(queueContext("p"), nil, nil, nil, envelope); err != nil {
		t.Fatal(err)
	}
	if add.MinCount != 2 || add.MaxCount != 6 || add.NumericProperties["rating"] != 1200 || add.Query != "+properties.game:riftbound-six +properties.rating:>=1050 +properties.rating:<=1350" {
		t.Fatalf("untrusted request not overridden: %v", add)
	}
	queue.entries["p"].Since -= 10001
	beforeMatchmaker(queueContext("p"), nil, nil, nil, envelope)
	if add.Query != "+properties.game:riftbound-six +properties.rating:>=900 +properties.rating:<=1500" {
		t.Fatal("waiting range must widen with a bounded gap")
	}
	if _, err := beforeMatchmaker(queueContext("unknown"), nil, nil, nil, envelope); err == nil {
		t.Fatal("unregistered session accepted")
	}
}

type fakeQueueNakama struct {
	runtime.NakamaModule
	creates int
}

func (n *fakeQueueNakama) MatchCreate(ctx context.Context, name string, params map[string]interface{}) (string, error) {
	n.creates++
	return fmt.Sprintf("match-%d", n.creates), nil
}

type fakeQueuePresence struct {
	runtime.Presence
	id string
}

func (p fakeQueuePresence) GetUserId() string    { return p.id }
func (p fakeQueuePresence) GetSessionId() string { return "session-" + p.id }

type fakeQueueEntry struct {
	runtime.MatchmakerEntry
	id string
}

func (e fakeQueueEntry) GetPresence() runtime.Presence { return fakeQueuePresence{id: e.id} }
func TestMatchmakerTimeoutRaceAssignsOnce(t *testing.T) {
	resetQueue(t)
	queue.entries["p"] = &queueEntry{Seat: game.Seat{ID: "p", Rating: 1000, Deck: game.DefaultDeck()}, Session: "session-p", Since: time.Now().UnixMilli() - 21000}
	nk := &fakeQueueNakama{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		matchmade(context.Background(), nil, nil, nk, []runtime.MatchmakerEntry{fakeQueueEntry{id: "p"}})
	}()
	go func() { defer wg.Done(); queueStatus(queueContext("p"), nil, nil, nk, "{}") }()
	wg.Wait()
	if nk.creates != 1 || queue.entries["p"].MatchID == "" {
		t.Fatal("match callback and fallback must produce one assignment")
	}
	response, err := queueStatus(queueContext("p"), nil, nil, nk, "{}")
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]string
	json.Unmarshal([]byte(response), &parsed)
	if parsed["match_id"] != queue.entries["p"].MatchID {
		t.Fatal("assignment should be idempotent")
	}
	if _, err := queueCancel(queueContext("p"), nil, nil, nk, "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := queueStatus(queueContext("p"), nil, nil, nk, "{}"); err == nil {
		t.Fatal("cancelled search still active")
	}
}

type fakeKickDispatcher struct {
	runtime.MatchDispatcher
	kicked []string
	fail   bool
}

func (d *fakeKickDispatcher) MatchKick(ps []runtime.Presence) error {
	if d.fail {
		return fmt.Errorf("temporary kick failure")
	}
	for _, p := range ps {
		d.kicked = append(d.kicked, p.GetUserId())
	}
	return nil
}
func TestEliminatedPlayerLeavesMatchAndCannotRejoin(t *testing.T) {
	l, err := game.NewLeague([]game.Seat{{ID: "dead", Deck: game.DefaultDeck(), Rating: 1000}, {ID: "alive", Deck: game.DefaultDeck(), Rating: 1000}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	l.Connected("dead", true)
	l.Connected("alive", true)
	l.Players[0].HP = 0
	l.Place[0] = 6
	s := &leagueState{league: l, presences: map[string]runtime.Presence{"dead": fakeQueuePresence{id: "dead"}, "alive": fakeQueuePresence{id: "alive"}}}
	d := &fakeKickDispatcher{fail: true}
	kickEliminated(s, d, nil, 1000)
	if len(s.presences) != 2 {
		t.Fatal("failed kick must retry rather than lose tracking")
	}
	d.fail = false
	kickEliminated(s, d, nil, 1001)
	if len(d.kicked) != 1 || d.kicked[0] != "dead" || len(s.presences) != 1 || l.Players[0].Connected || !l.Players[1].Connected {
		t.Fatal("only eliminated presence should disconnect")
	}
	if l.Index("dead") < 0 || l.Place[0] != 6 {
		t.Fatal("retain eliminated seat and placement")
	}
	_, accepted, _ := (&leagueMatch{}).MatchJoinAttempt(context.Background(), nil, nil, nil, d, 0, s, fakeQueuePresence{id: "dead"}, nil)
	if accepted {
		t.Fatal("eliminated player must not reconnect to old game")
	}
	kickEliminated(s, d, nil, 1002)
	if len(d.kicked) != 1 {
		t.Fatal("kick must occur once")
	}
}

func TestSessionEndOnlyRemovesItsOwnQueue(t *testing.T) {
	resetQueue(t)
	queue.entries["p"] = &queueEntry{Session: "new-session"}
	queueSessionEnded(queueContext("p"), nil, nil)
	if queue.entries["p"] == nil {
		t.Fatal("late old event removed new search")
	}
	ctx := context.WithValue(queueContext("p"), runtime.RUNTIME_CTX_SESSION_ID, "new-session")
	queueSessionEnded(ctx, nil, nil)
	if queue.entries["p"] != nil {
		t.Fatal("ended session left stale search")
	}
}
