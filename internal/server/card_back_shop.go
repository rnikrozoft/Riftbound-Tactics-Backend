package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"sort"
)

func ownsCardBack(p playerData, id string) bool {
	id = game.NormalizeCardBackID(id)
	if id == game.DefaultCardBackID {
		return true
	}
	for _, owned := range p.OwnedCardBacks {
		if owned == id {
			return true
		}
	}
	return false
}

func cardBackPurchase(ctx context.Context, l runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	if activeConfig != nil && activeConfig.Economy.FreeCardBacks {
		return "", runtime.NewError("All card backs are free; shop is closed", 9)
	}
	var req struct {
		CardBackID    string `json:"card_back_id"`
		RequestID     string `json:"request_id"`
		ConfigVersion string `json:"config_version"`
	}
	if len(payload) > 4096 || json.Unmarshal([]byte(payload), &req) != nil || !storageIDPattern.MatchString(req.RequestID) {
		return "", runtime.NewError("Invalid card back purchase", 3)
	}
	if req.ConfigVersion != activeConfigVersion {
		return "", runtime.NewError("Shop changed; refresh collection", 9)
	}
	back, ok := game.CardBack(req.CardBackID)
	if !ok || req.CardBackID == "" {
		return "", runtime.NewError("Card back unavailable", 3)
	}
	for attempt := 0; attempt < 4; attempt++ {
		p, v, e := ensurePlayer(ctx, nk, id, nil, "")
		if e != nil {
			return "", e
		}
		receipts, e := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: "riftbound_purchases", Key: req.RequestID, UserID: id}})
		if e != nil {
			return "", e
		}
		if len(receipts) > 0 {
			var previous struct {
				Type       string `json:"type"`
				CardBackID string `json:"card_back_id"`
			}
			if json.Unmarshal([]byte(receipts[0].Value), &previous) != nil || previous.Type != "card_back" || previous.CardBackID != back.ID {
				return "", runtime.NewError("Purchase request ID was already used", 9)
			}
			return viewPlayer(ctx, nk, id, p, v, req.ConfigVersion)
		}
		if ownsCardBack(p, back.ID) {
			return viewPlayer(ctx, nk, id, p, v, req.ConfigVersion)
		}
		p.OwnedCardBacks = append(p.OwnedCardBacks, back.ID)
		sort.Strings(p.OwnedCardBacks)
		receipt, _ := json.Marshal(map[string]interface{}{"type": "card_back", "card_back_id": back.ID, "price": back.Price, "currency": "coins"})
		acks, _, e := nk.MultiUpdate(ctx, nil, []*runtime.StorageWrite{playerWrite(id, p, v), {Collection: "riftbound_purchases", Key: req.RequestID, UserID: id, Value: string(receipt), Version: "*", PermissionRead: 1, PermissionWrite: 0}}, nil, []*runtime.WalletUpdate{{UserID: id, Changeset: map[string]int64{"coins": -back.Price}, Metadata: map[string]interface{}{"source": "card_back_purchase", "card_back_id": back.ID, "request_id": req.RequestID}}}, true)
		if errors.Is(e, runtime.ErrStorageRejectedVersion) {
			continue
		}
		var negative *runtime.WalletNegativeError
		if errors.As(e, &negative) {
			return "", runtime.NewError("Not enough coins", 9)
		}
		if e != nil {
			return "", e
		}
		for _, ack := range acks {
			if ack.Collection == playerCollection {
				return viewPlayer(ctx, nk, id, p, ack.Version, req.ConfigVersion)
			}
		}
		return "", errors.New("Purchase acknowledgement missing")
	}
	return "", runtime.NewError("Collection changed; retry purchase", 10)
}

// Undo the brief all-backs giveaway, preserving every recorded purchase.
func restorePaidCardBacks(ctx context.Context, nk runtime.NakamaModule, id string, p *playerData) (bool, error) {
	if activeConfig == nil || activeConfig.Economy.FreeCardBacks || p.CardBackShopVersion >= 1 {
		return false, nil
	}
	if len(p.OwnedCardBacks) >= len(game.CardBacks) {
		keep := map[string]bool{game.DefaultCardBackID: true}
		cursor := ""
		for {
			receipts, next, err := nk.StorageList(ctx, id, id, "riftbound_purchases", 100, cursor)
			if err != nil {
				return false, err
			}
			for _, object := range receipts {
				var receipt struct {
					Type       string `json:"type"`
					CardBackID string `json:"card_back_id"`
				}
				if json.Unmarshal([]byte(object.Value), &receipt) == nil && receipt.Type == "card_back" {
					keep[receipt.CardBackID] = true
				}
			}
			if next == "" {
				break
			}
			cursor = next
		}
		p.OwnedCardBacks = nil
		for _, back := range game.CardBacks {
			if keep[back.ID] {
				p.OwnedCardBacks = append(p.OwnedCardBacks, back.ID)
			}
		}
		for i := range p.Decks {
			if !keep[game.NormalizeCardBackID(p.Decks[i].CardBackID)] {
				p.Decks[i].CardBackID = game.DefaultCardBackID
			}
		}
	}
	p.CardBackShopVersion = 1
	return true, nil
}
