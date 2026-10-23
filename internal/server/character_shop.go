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

func ownsCharacter(p playerData, id string) bool {
	for _, owned := range p.Owned {
		if owned == id {
			return true
		}
	}
	return false
}
func characterPurchase(ctx context.Context, l runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	if activeConfig != nil && activeConfig.Economy.FreeCharacterCards {
		return "", runtime.NewError("All character cards are free; character shop is closed", 9)
	}
	var req struct {
		CharacterID   string `json:"character_id"`
		RequestID     string `json:"request_id"`
		ConfigVersion string `json:"config_version"`
	}
	if len(payload) > 4096 || json.Unmarshal([]byte(payload), &req) != nil || !storageIDPattern.MatchString(req.RequestID) {
		return "", runtime.NewError("Invalid purchase request", 3)
	}
	if req.ConfigVersion != activeConfigVersion {
		return "", runtime.NewError("Shop changed; login again", 9)
	}
	kind, ok := characterKind(req.CharacterID)
	if !ok || !game.Character(kind).Enabled {
		return "", runtime.NewError("Character unavailable", 3)
	}
	price := activeConfig.Economy.Prices[req.CharacterID]
	for attempt := 0; attempt < 4; attempt++ {
		p, v, err := ensurePlayer(ctx, nk, id, nil, "")
		if err != nil {
			return "", err
		}
		receipt, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: "riftbound_purchases", Key: req.RequestID, UserID: id}})
		if err != nil {
			return "", err
		}
		if len(receipt) > 0 {
			var previous struct {
				CharacterID string `json:"character_id"`
			}
			if json.Unmarshal([]byte(receipt[0].Value), &previous) != nil || previous.CharacterID != req.CharacterID {
				return "", runtime.NewError("Purchase request ID was already used", 9)
			}
			return viewPlayer(ctx, nk, id, p, v, req.ConfigVersion)
		}
		if ownsCharacter(p, req.CharacterID) {
			return viewPlayer(ctx, nk, id, p, v, req.ConfigVersion)
		}
		p.Owned = append(p.Owned, req.CharacterID)
		sort.Strings(p.Owned)
		receiptJSON, _ := json.Marshal(map[string]interface{}{"character_id": req.CharacterID, "price": price, "currency": "coins"})
		acks, _, err := nk.MultiUpdate(ctx, nil, []*runtime.StorageWrite{playerWrite(id, p, v), {Collection: "riftbound_purchases", Key: req.RequestID, UserID: id, Value: string(receiptJSON), Version: "*", PermissionRead: 1, PermissionWrite: 0}}, nil, []*runtime.WalletUpdate{{UserID: id, Changeset: map[string]int64{"coins": -price}, Metadata: map[string]interface{}{"source": "character_purchase", "character_id": req.CharacterID, "request_id": req.RequestID}}}, true)
		if errors.Is(err, runtime.ErrStorageRejectedVersion) {
			continue
		}
		var negative *runtime.WalletNegativeError
		if errors.As(err, &negative) {
			return "", runtime.NewError("Not enough coins", 9)
		}
		if err != nil {
			return "", err
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

func characterGroupPurchase(ctx context.Context, l runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	if activeConfig != nil && activeConfig.Economy.FreeCharacterCards {
		return "", runtime.NewError("All character cards are free; character shop is closed", 9)
	}
	var req struct {
		Group         *int   `json:"group"`
		RequestID     string `json:"request_id"`
		ConfigVersion string `json:"config_version"`
	}
	if len(payload) > 4096 || json.Unmarshal([]byte(payload), &req) != nil || req.Group == nil || *req.Group < 0 || *req.Group >= len(activeConfig.Catalog.GroupNames) || !storageIDPattern.MatchString(req.RequestID) {
		return "", runtime.NewError("Invalid group purchase", 3)
	}
	if req.ConfigVersion != activeConfigVersion {
		return "", runtime.NewError("Shop changed; refresh collection", 9)
	}
	for attempt := 0; attempt < 4; attempt++ {
		p, v, err := ensurePlayer(ctx, nk, id, nil, "")
		if err != nil {
			return "", err
		}
		receipts, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: "riftbound_purchases", Key: req.RequestID, UserID: id}})
		if err != nil {
			return "", err
		}
		if len(receipts) > 0 {
			var receipt struct {
				Type  string `json:"type"`
				Group int    `json:"group"`
			}
			if json.Unmarshal([]byte(receipts[0].Value), &receipt) != nil || receipt.Type != "character_group" || receipt.Group != *req.Group {
				return "", runtime.NewError("Purchase request ID was already used", 9)
			}
			return viewPlayer(ctx, nk, id, p, v, req.ConfigVersion)
		}
		missing := []string{}
		price := int64(0)
		available := false
		for _, ch := range activeConfig.Catalog.Characters {
			if ch.Enabled && ch.Group == *req.Group {
				available = true
				if !ownsCharacter(p, ch.ID) {
					missing = append(missing, ch.ID)
					price += activeConfig.Economy.Prices[ch.ID]
				}
			}
		}
		if !available {
			return "", runtime.NewError("Character group unavailable", 3)
		}
		if len(missing) == 0 {
			return viewPlayer(ctx, nk, id, p, v, req.ConfigVersion)
		}
		p.Owned = append(p.Owned, missing...)
		sort.Strings(p.Owned)
		receipt, _ := json.Marshal(map[string]interface{}{"type": "character_group", "group": *req.Group, "character_ids": missing, "price": price, "currency": "coins"})
		acks, _, err := nk.MultiUpdate(ctx, nil, []*runtime.StorageWrite{playerWrite(id, p, v), {Collection: "riftbound_purchases", Key: req.RequestID, UserID: id, Value: string(receipt), Version: "*", PermissionRead: 1, PermissionWrite: 0}}, nil, []*runtime.WalletUpdate{{UserID: id, Changeset: map[string]int64{"coins": -price}, Metadata: map[string]interface{}{"source": "character_group_purchase", "group": *req.Group, "request_id": req.RequestID}}}, true)
		if errors.Is(err, runtime.ErrStorageRejectedVersion) {
			continue
		}
		var negative *runtime.WalletNegativeError
		if errors.As(err, &negative) {
			return "", runtime.NewError("Not enough coins", 9)
		}
		if err != nil {
			return "", err
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
