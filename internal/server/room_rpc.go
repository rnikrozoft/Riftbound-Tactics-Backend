package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"math/big"
	"strconv"
	"strings"
	"time"
)

func roomResponse(code, id string) (string, error) {
	out, _ := json.Marshal(map[string]string{"code": code, "match_id": id})
	return string(out), nil
}
func createRoom(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	var request struct {
		Deck *game.Deck `json:"deck"`
	}
	if len(payload) > 16384 || (payload != "" && json.Unmarshal([]byte(payload), &request) != nil) {
		return "", runtime.NewError("Invalid deck request", 3)
	}
	deck := game.DefaultDeck()
	if request.Deck != nil {
		deck = *request.Deck
	}
	if err := validateOwnedDeck(ctx, nk, id, deck); err != nil {
		return "", runtime.NewError(err.Error(), 3)
	}
	deckBytes, _ := json.Marshal(deck)
	for attempt := 0; attempt < 20; attempt++ {
		number, err := rand.Int(rand.Reader, big.NewInt(900000))
		if err != nil {
			return "", err
		}
		code := strconv.FormatInt(100000+number.Int64(), 10)
		reservation := roomDirectoryEntry{Creator: id, ExpiresMs: time.Now().Add(30 * time.Minute).UnixMilli()}
		raw, _ := json.Marshal(reservation)
		acks, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: roomDirectoryCollection, Key: code, Value: string(raw), Version: "*", PermissionRead: 0, PermissionWrite: 0}})
		if errors.Is(err, runtime.ErrStorageRejectedVersion) {
			reclaimExpiredRoom(ctx, nk, code)
			continue
		}
		if err != nil || len(acks) != 1 {
			return "", runtime.NewError("Room storage unavailable", 14)
		}
		matchID, err := nk.MatchCreate(ctx, "riftbound_room", map[string]interface{}{"code": code, "creator": id, "deck": string(deckBytes)})
		if err != nil {
			nk.StorageDelete(ctx, []*runtime.StorageDelete{{Collection: roomDirectoryCollection, Key: code, Version: acks[0].Version}})
			logger.Error("MatchCreate: %v", err)
			return "", runtime.NewError("Unable to create room", 14)
		}
		reservation.MatchID = matchID
		raw, _ = json.Marshal(reservation)
		if _, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: roomDirectoryCollection, Key: code, Value: string(raw), Version: acks[0].Version, PermissionRead: 0, PermissionWrite: 0}}); err != nil {
			return "", runtime.NewError("Room storage unavailable", 14)
		}
		return roomResponse(code, matchID)
	}
	return "", runtime.NewError("Unable to allocate room code", 14)
}
func joinRoom(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	if _, err := userID(ctx); err != nil {
		return "", err
	}
	var request struct {
		Code string `json:"code"`
	}
	if len(payload) > 16384 || json.Unmarshal([]byte(payload), &request) != nil {
		return "", runtime.NewError("Invalid request", 3)
	}
	code := strings.TrimSpace(request.Code)
	if len(code) != 6 {
		return "", runtime.NewError("Enter a 6-digit room code", 3)
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return "", runtime.NewError("Enter a 6-digit room code", 3)
		}
	}
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: roomDirectoryCollection, Key: code, UserID: systemStorageOwner}})
	if err != nil {
		return "", runtime.NewError("Room storage unavailable", 14)
	}
	var entry roomDirectoryEntry
	if len(objects) != 1 || json.Unmarshal([]byte(objects[0].Value), &entry) != nil || entry.MatchID == "" || entry.ExpiresMs <= time.Now().UnixMilli() {
		return "", runtime.NewError("Room not found", 5)
	}
	matchID := entry.MatchID
	match, err := nk.MatchGet(ctx, matchID)
	if err != nil || match == nil {
		return "", runtime.NewError("Room closed", 5)
	}
	return roomResponse(code, matchID)
}
