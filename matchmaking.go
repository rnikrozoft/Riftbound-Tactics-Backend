package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"sync"
	"time"
)

type queueEntry struct {
	Seat    game.Seat
	Session string
	Since   int64
	MatchID string
}

var queue = struct {
	sync.Mutex
	entries map[string]*queueEntry
}{entries: map[string]*queueEntry{}}

func queueSession(ctx context.Context) string {
	s, _ := ctx.Value(runtime.RUNTIME_CTX_SESSION_ID).(string)
	return s
}
func registerMatchmaking(i runtime.Initializer) error {
	if err := i.RegisterMatch("riftbound_league", func(context.Context, runtime.Logger, *sql.DB, runtime.NakamaModule) (runtime.Match, error) {
		return &leagueMatch{}, nil
	}); err != nil {
		return err
	}
	for name, fn := range map[string]func(context.Context, runtime.Logger, *sql.DB, runtime.NakamaModule, string) (string, error){"queue_begin": queueBegin, "queue_status": queueStatus, "queue_cancel": queueCancel} {
		if err := i.RegisterRpc(name, fn); err != nil {
			return err
		}
	}
	if err := i.RegisterBeforeRt("MatchmakerAdd", beforeMatchmaker); err != nil {
		return err
	}
	if err := i.RegisterEventSessionEnd(queueSessionEnded); err != nil {
		return err
	}
	return i.RegisterMatchmakerMatched(matchmade)
}
func queueBegin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	session := queueSession(ctx)
	if session == "" {
		return "", runtime.NewError("Use a realtime session", 3)
	}
	var req struct {
		Deck game.Deck `json:"deck"`
	}
	if len(payload) > 16384 || json.Unmarshal([]byte(payload), &req) != nil {
		return "", runtime.NewError("Invalid deck", 3)
	}
	if err := validateOwnedDeck(ctx, nk, id, req.Deck); err != nil {
		return "", runtime.NewError(err.Error(), 3)
	}
	mmr, err := loadMMR(ctx, db, id)
	if err != nil {
		return "", err
	}
	now := time.Now().UnixMilli()
	queue.Lock()
	defer queue.Unlock()
	for k, v := range queue.entries {
		if now-v.Since > 30000 {
			delete(queue.entries, k)
		}
	}
	if old := queue.entries[id]; old != nil && old.Session == session {
		return "", runtime.NewError("Already searching or assigned; cancel the previous search first", 9)
	}
	// Match only by persisted server-owned skill MMR. Wins do not enter this path.
	queue.entries[id] = &queueEntry{Seat: game.Seat{ID: id, Deck: req.Deck, Rating: mmr.Rating}, Session: session, Since: now}
	return `{"match_id":"","code":"SEARCHING"}`, nil
}
func beforeMatchmaker(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *rtapi.Envelope) (*rtapi.Envelope, error) {
	id, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	queue.Lock()
	defer queue.Unlock()
	q := queue.entries[id]
	if q == nil || q.Session != queueSession(ctx) || q.MatchID != "" {
		return nil, runtime.NewError("Start matchmaking through the lobby first", 9)
	}
	a := in.GetMatchmakerAdd()
	if a == nil {
		return nil, runtime.NewError("Invalid matchmaker request", 3)
	}
	gap := 150
	if time.Now().UnixMilli()-q.Since >= 10000 {
		gap = 300
	}
	a.MinCount = 2
	a.MaxCount = 6
	a.CountMultiple = nil
	a.StringProperties = map[string]string{"game": "riftbound-six"}
	a.NumericProperties = map[string]float64{"rating": float64(q.Seat.Rating)}
	a.Query = fmt.Sprintf("+properties.game:riftbound-six +properties.rating:>=%d +properties.rating:<=%d", q.Seat.Rating-gap, q.Seat.Rating+gap)
	return in, nil
}

// The callback and timeout fallback share one lock, so a person is reserved once.
func matchmade(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, entries []runtime.MatchmakerEntry) (string, error) {
	queue.Lock()
	defer queue.Unlock()
	seats := []game.Seat{}
	seen := map[string]bool{}
	for _, e := range entries {
		p := e.GetPresence()
		q := queue.entries[p.GetUserId()]
		if q == nil || q.Session != p.GetSessionId() || q.MatchID != "" || seen[p.GetUserId()] {
			return "", runtime.NewError("Search no longer active", 9)
		}
		seen[p.GetUserId()] = true
		seats = append(seats, q.Seat)
	}
	return assignLeague(ctx, nk, seats)
}
func assignLeague(ctx context.Context, nk runtime.NakamaModule, seats []game.Seat) (string, error) {
	raw, _ := json.Marshal(seats)
	id, err := nk.MatchCreate(ctx, "riftbound_league", map[string]interface{}{"seats": string(raw)})
	if err != nil {
		return "", err
	}
	for _, s := range seats {
		queue.entries[s.ID].MatchID = id
	}
	return id, nil
}
func queueStatus(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	queue.Lock()
	defer queue.Unlock()
	q := queue.entries[id]
	if q == nil || q.Session != queueSession(ctx) {
		return "", runtime.NewError("Search expired or cancelled", 5)
	}
	if q.MatchID == "" && time.Now().UnixMilli()-q.Since >= 20000 {
		if _, err := assignLeague(ctx, nk, []game.Seat{q.Seat}); err != nil {
			return "", err
		}
	}
	return roomResponse("MATCHMAKING", q.MatchID)
}
func queueCancel(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	queue.Lock()
	defer queue.Unlock()
	if q := queue.entries[id]; q != nil && q.Session == queueSession(ctx) {
		delete(queue.entries, id)
	}
	return "{}", nil
}

// Events are asynchronous: an old socket ending must not remove a newer search.
func queueSessionEnded(ctx context.Context, logger runtime.Logger, evt *api.Event) {
	id, err := userID(ctx)
	if err != nil {
		return
	}
	session := queueSession(ctx)
	if session == "" {
		return
	}
	queue.Lock()
	defer queue.Unlock()
	if entry := queue.entries[id]; entry != nil && entry.Session == session {
		delete(queue.entries, id)
	}
}
