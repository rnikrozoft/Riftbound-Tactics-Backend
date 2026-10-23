package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"os"
	"time"
)

func InitModule(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, initializer runtime.Initializer) error {
	if err := registerStorageAccess(initializer); err != nil {
		return err
	}
	path := os.Getenv("RIFTBOUND_CHARACTERS_FILE")
	if path == "" {
		path = "/nakama/data/characters.json"
	}
	catalog, loadErr := game.LoadCharacterCatalog(path)
	if loadErr != nil {
		return loadErr
	}
	game.UseCatalog(catalog)
	if err := initializePlayerStorage(ctx, nk, initializer, catalog); err != nil {
		return err
	}
	if err := initializeLocalization(ctx, nk, initializer); err != nil {
		return err
	}
	logger.Info("Loaded %d character definitions from %s", len(catalog.Characters), path)
	catalogBytes, err := json.Marshal(game.Catalog())
	if err != nil {
		return err
	}
	catalogJSON := string(catalogBytes)
	if err := initializer.RegisterRpc("character_catalog", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		if _, err := userID(ctx); err != nil {
			return "", err
		}
		return catalogJSON, nil
	}); err != nil {
		return err
	}

	if err = initializer.RegisterRpc("mmr_self", selfMMR); err != nil {
		return err
	}
	if err = initializer.RegisterMatch("riftbound_room", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) (runtime.Match, error) {
		return &roomMatch{}, nil
	}); err != nil {
		return err
	}
	if err = initializer.RegisterRpc("room_create", createRoom); err != nil {
		return err
	}
	if err = initializer.RegisterRpc("room_join", joinRoom); err != nil {
		return err
	}
	if err = initializer.RegisterRpc("server_clock", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		if _, err := userID(ctx); err != nil {
			return "", err
		}
		out, _ := json.Marshal(map[string]int64{"server_ms": time.Now().UnixMilli()})
		return string(out), nil
	}); err != nil {
		return err
	}
	if err = nk.LeaderboardCreate(ctx, winLeaderboard, true, "desc", "best", "", map[string]interface{}{"points_per_win": 1, "name": "All-time match wins"}, true); err != nil {
		return err
	}
	if err = nk.LeaderboardCreate(ctx, mmrLeaderboard, true, "desc", "set", "", map[string]interface{}{"name": "Skill MMR"}, true); err != nil {
		return err
	}
	if err := registerMatchmaking(initializer); err != nil {
		return err
	}
	if err := startLeaderboardReconciler(initializer, logger, nk); err != nil {
		return err
	}
	logger.Info("Riftbound Tactics authoritative Go runtime loaded")
	return nil
}
