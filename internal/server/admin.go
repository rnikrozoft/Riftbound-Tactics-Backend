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

// Nakama authenticates the runtime HTTP key before dispatch. Player tokens are denied.
func requireAdmin(ctx context.Context) error {
	if id, _ := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string); id != "" {
		return runtime.NewError("Server administration only", 7)
	}
	mode, _ := ctx.Value(runtime.RUNTIME_CTX_MODE).(string)
	if mode != "rpc" {
		return runtime.NewError("Server administration only", 7)
	}
	return nil
}
func adminConfigGet(ctx context.Context, l runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	if err := requireAdmin(ctx); err != nil {
		return "", err
	}
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: configCollection, Key: configKey, UserID: systemStorageOwner}})
	if err != nil {
		return "", err
	}
	if len(objects) != 1 {
		return "", errors.New("Configuration missing")
	}
	raw, _ := json.Marshal(map[string]interface{}{"storage_version": objects[0].Version, "config": json.RawMessage(objects[0].Value), "active_version": activeConfigVersion})
	return string(raw), nil
}
func adminConfigPublish(ctx context.Context, l runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	if err := requireAdmin(ctx); err != nil {
		return "", err
	}
	var req struct {
		Version          string                                `json:"storage_version"`
		Economy          *economySettings                      `json:"economy,omitempty"`
		CharacterPatches map[string]map[string]json.RawMessage `json:"character_patches,omitempty"`
		NewCharacters    []game.CharacterDefinition            `json:"new_characters,omitempty"`
	}
	if len(payload) > 128*1024 || json.Unmarshal([]byte(payload), &req) != nil || req.Version == "" || req.Version == "*" {
		return "", runtime.NewError("Provide a bounded configuration patch and current storage version", 3)
	}
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: configCollection, Key: configKey, UserID: systemStorageOwner}})
	if err != nil {
		return "", err
	}
	if len(objects) != 1 || objects[0].Version != req.Version {
		return "", runtime.NewError("Published configuration changed; export it again", 10)
	}
	var config gameConfiguration
	if err = json.Unmarshal([]byte(objects[0].Value), &config); err != nil {
		return "", err
	}
	if req.Economy != nil {
		config.Economy = *req.Economy
	}
	for id, patch := range req.CharacterPatches {
		index := -1
		for i, c := range config.Catalog.Characters {
			if c.ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			return "", runtime.NewError("Unknown character patch ID", 3)
		}
		raw, _ := json.Marshal(config.Catalog.Characters[index])
		var fields map[string]json.RawMessage
		json.Unmarshal(raw, &fields)
		for key, value := range patch {
			if key == "kind" || key == "character_id" || key == "group" {
				return "", runtime.NewError("Character identity cannot be patched", 3)
			}
			if _, ok := fields[key]; !ok {
				if key != "combat" {
					return "", runtime.NewError("Unknown character setting", 3)
				}
			}
			fields[key] = value
		}
		raw, _ = json.Marshal(fields)
		if err = json.Unmarshal(raw, &config.Catalog.Characters[index]); err != nil {
			return "", runtime.NewError("Invalid character patch", 3)
		}
	}
	config.Catalog.Characters = append(config.Catalog.Characters, req.NewCharacters...)
	retireStarterGroups(&config)
	if err = validateConfiguration(config); err != nil {
		return "", runtime.NewError(err.Error(), 3)
	}
	raw, _ := json.Marshal(config)
	acks, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: configCollection, Key: configKey, Value: string(raw), Version: req.Version, PermissionRead: 0, PermissionWrite: 0}})
	if err != nil {
		return "", err
	}
	result, _ := json.Marshal(map[string]interface{}{"config_version": configurationHash(config), "storage_version": acks[0].Version, "restart_required": true})
	return string(result), nil
}
func adminPlayerGrant(ctx context.Context, l runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	if err := requireAdmin(ctx); err != nil {
		return "", err
	}
	type grantRequest struct {
		UserID      string `json:"user_id"`
		CharacterID string `json:"character_id"`
		Coins       int64  `json:"coins"`
		RequestID   string `json:"request_id"`
	}
	var req grantRequest
	if len(payload) > 4096 || json.Unmarshal([]byte(payload), &req) != nil || req.UserID == "" || req.Coins < 0 || req.Coins > 1000000000 || !storageIDPattern.MatchString(req.RequestID) {
		return "", runtime.NewError("Invalid grant", 3)
	}
	if req.CharacterID != "" {
		if _, ok := characterKind(req.CharacterID); !ok {
			return "", runtime.NewError("Unknown character", 3)
		}
	}
	for attempt := 0; attempt < 4; attempt++ {
		p, v, err := ensurePlayer(ctx, nk, req.UserID, nil, "")
		if err != nil {
			return "", err
		}
		receipts, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: "riftbound_grants", Key: req.RequestID, UserID: req.UserID}})
		if err != nil {
			return "", err
		}
		receiptJSON, _ := json.Marshal(req)
		if len(receipts) > 0 {
			var previous grantRequest
			if json.Unmarshal([]byte(receipts[0].Value), &previous) != nil || previous != req {
				return "", runtime.NewError("Grant ID reused for different operation", 9)
			}
			return viewPlayer(ctx, nk, req.UserID, p, v, activeConfigVersion)
		}
		if req.CharacterID != "" && !ownsCharacter(p, req.CharacterID) {
			p.Owned = append(p.Owned, req.CharacterID)
			sort.Strings(p.Owned)
		}
		acks, _, err := nk.MultiUpdate(ctx, nil, []*runtime.StorageWrite{playerWrite(req.UserID, p, v), {Collection: "riftbound_grants", Key: req.RequestID, UserID: req.UserID, Value: string(receiptJSON), Version: "*", PermissionRead: 1, PermissionWrite: 0}}, nil, []*runtime.WalletUpdate{{UserID: req.UserID, Changeset: map[string]int64{"coins": req.Coins}, Metadata: map[string]interface{}{"source": "admin_grant", "request_id": req.RequestID}}}, true)
		if errors.Is(err, runtime.ErrStorageRejectedVersion) {
			continue
		}
		if err != nil {
			return "", err
		}
		for _, ack := range acks {
			if ack.Collection == playerCollection {
				return viewPlayer(ctx, nk, req.UserID, p, ack.Version, activeConfigVersion)
			}
		}
	}
	return "", runtime.NewError("Collection changed; retry grant", 10)
}

// Unlock a complete character group atomically, charging only variants not yet owned.
