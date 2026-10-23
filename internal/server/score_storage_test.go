package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"sync"
	"testing"
	"time"
)

func TestRatingStorageDuplicateAndConcurrentResults(t *testing.T) {
	nk := inventoryFake()
	ctx := context.Background()
	first, err := persistMMR(ctx, nk, "match", "player", 1, 16, true)
	if err != nil || first != (MMRResult{1000, 1016, 16, true}) {
		t.Fatal(first, err)
	}
	duplicate, err := persistMMR(ctx, nk, "match", "player", 6, -99, true)
	if err != nil || duplicate != first {
		t.Fatal("duplicate changed rating", duplicate, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := persistMMR(ctx, nk, fmt.Sprint("other-", i), "player", 2, 10, true); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	account, _, err := readRating(ctx, nk, "player")
	if err != nil || account.Rating != 1076 || account.Games != 7 || account.Synced {
		t.Fatal(account, err)
	}
	for _, object := range nk.objects {
		if object.PermissionRead != 0 || object.PermissionWrite != 0 {
			t.Fatal("scores exposed to client writes/reads")
		}
	}
}

func TestRatingFloorAndUnratedResult(t *testing.T) {
	nk := inventoryFake()
	ctx := context.Background()
	result, err := persistMMR(ctx, nk, "loss", "p", 6, -2000, true)
	if err != nil || result.After != 0 || result.Delta != -1000 {
		t.Fatal(result, err)
	}
	result, err = persistMMR(ctx, nk, "bots-only", "solo", 1, 0, false)
	account, _, _ := readRating(ctx, nk, "solo")
	if err != nil || result.Rated || result.After != initialMMR || account.Games != 0 {
		t.Fatal(result, account, err)
	}
}

func TestWinReceiptsAndBattleImmutability(t *testing.T) {
	nk := inventoryFake()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := recordWin(ctx, nk, "same", "p"); err != nil {
			t.Fatal(err)
		}
	}
	if err := recordWin(ctx, nk, "other", "p"); err != nil {
		t.Fatal(err)
	}
	if err := recordWin(ctx, nk, "same", "different-winner"); err != nil {
		t.Fatal(err)
	}
	other, _, _ := readWins(ctx, nk, "different-winner")
	if other.Wins != 0 {
		t.Fatal("one match counted for two winners")
	}
	account, _, err := readWins(ctx, nk, "p")
	if err != nil || account.Wins != 2 {
		t.Fatal(account, err)
	}
	original := json.RawMessage(`{"winner":"A"}`)
	if err := persistBattle(ctx, nk, "m", 1, "123456", original); err != nil {
		t.Fatal(err)
	}
	if err := persistBattle(ctx, nk, "m", 1, "654321", json.RawMessage(`{"winner":"B"}`)); err != nil {
		t.Fatal(err)
	}
	object := nk.objects[objectKey(battleCollection, "m:1", systemStorageOwner)]
	var stored struct {
		Code   string `json:"code"`
		Result struct {
			Winner string `json:"winner"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(object.Value), &stored); err != nil || stored.Code != "123456" || stored.Result.Winner != "A" || object.PermissionRead != 0 || object.PermissionWrite != 0 {
		t.Fatal(object, err)
	}
}

type failingScoreStorage struct {
	*inventoryNakama
	fail bool
}

func (n *failingScoreStorage) StorageWrite(ctx context.Context, w []*runtime.StorageWrite) ([]*api.StorageObjectAck, error) {
	if n.fail {
		return nil, errors.New("storage unavailable")
	}
	return n.inventoryNakama.StorageWrite(ctx, w)
}
func TestFailedScoreWriteDoesNotConsumeReceipt(t *testing.T) {
	nk := &failingScoreStorage{inventoryFake(), true}
	ctx := context.Background()
	if _, err := persistMMR(ctx, nk, "m", "p", 1, 16, true); err == nil {
		t.Fatal("failure hidden")
	}
	if len(nk.objects) != 0 {
		t.Fatal("partial transaction")
	}
	nk.fail = false
	result, err := persistMMR(ctx, nk, "m", "p", 1, 16, true)
	if err != nil || result.After != 1016 {
		t.Fatal(result, err)
	}
}

func TestMatchTaskDoesNotBlockAndPublishesOnce(t *testing.T) {
	release := make(chan struct{})
	task := startTask(func(ctx context.Context) (int, error) {
		select {
		case <-release:
			return 42, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	})
	if _, _, done := task.poll(); done {
		t.Fatal("unfinished task completed")
	}
	close(release)
	deadline := time.After(time.Second)
	for {
		if value, err, done := task.poll(); done {
			if err != nil || value != 42 {
				t.Fatal(value, err)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("worker did not complete")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, _, done := task.poll(); done {
		t.Fatal("result consumed twice")
	}
}

type scoreNakama struct {
	*inventoryNakama
	failLeaderboard bool
	writes          int
	duringWrite     func()
}

func (n *scoreNakama) UsersGetId(ctx context.Context, ids, facebookIDs []string) ([]*api.User, error) {
	return []*api.User{{Id: ids[0], Username: "player"}}, nil
}
func (n *scoreNakama) LeaderboardRecordWrite(ctx context.Context, leaderboard, id, name string, score, subscore int64, metadata map[string]interface{}, operator *int) (*api.LeaderboardRecord, error) {
	n.writes++
	if n.failLeaderboard {
		return nil, errors.New("leaderboard unavailable")
	}
	if n.duringWrite != nil {
		n.duringWrite()
	}
	return &api.LeaderboardRecord{Score: score}, nil
}
func TestLeaderboardFailureAndSupersededRatingStayDirty(t *testing.T) {
	ctx := context.Background()
	nk := &scoreNakama{inventoryNakama: inventoryFake(), failLeaderboard: true}
	if _, err := persistMMR(ctx, nk, "m", "p", 1, 16, true); err != nil {
		t.Fatal(err)
	}
	if err := syncMMR(ctx, nk, "p"); err == nil {
		t.Fatal("leaderboard failure hidden")
	}
	a, _, _ := readRating(ctx, nk, "p")
	if a.Synced {
		t.Fatal("failed leaderboard marked clean")
	}
	nk.failLeaderboard = false
	nk.duringWrite = func() {
		if _, err := persistMMR(ctx, nk, "new", "p", 6, -10, true); err != nil {
			t.Error(err)
		}
	}
	if err := syncMMR(ctx, nk, "p"); err != nil {
		t.Fatal(err)
	}
	a, _, _ = readRating(ctx, nk, "p")
	if a.Synced || a.Rating != 1006 || a.Games != 2 {
		t.Fatal("newer result overwritten or marked clean", a)
	}
	nk.duringWrite = nil
	if err := syncMMR(ctx, nk, "p"); err != nil {
		t.Fatal(err)
	}
	a, _, _ = readRating(ctx, nk, "p")
	if !a.Synced {
		t.Fatal("successful retry stayed dirty")
	}
}
