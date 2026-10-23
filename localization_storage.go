package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/localization"
	"os"
)

const localizationKey = "localization"

type publishedLocalization struct {
	Version  string                 `json:"version"`
	Document *localization.Document `json:"document,omitempty"`
}

func publishLocalization(ctx context.Context, nk runtime.NakamaModule, doc localization.Document) error {
	record := publishedLocalization{localization.Version(doc), &doc}
	raw, _ := json.Marshal(record)
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: configCollection, Key: localizationKey, UserID: systemStorageOwner}})
	if err != nil {
		return err
	}
	version := "*"
	if len(objects) > 0 {
		var previous publishedLocalization
		if err = json.Unmarshal([]byte(objects[0].Value), &previous); err != nil {
			return err
		}
		if previous.Version == record.Version {
			return nil
		}
		version = objects[0].Version
	}
	_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: configCollection, Key: localizationKey, UserID: systemStorageOwner, Value: string(raw), Version: version, PermissionRead: 0, PermissionWrite: 0}})
	return err
}
func initializeLocalization(ctx context.Context, nk runtime.NakamaModule, i runtime.Initializer) error {
	filename := os.Getenv("RIFTBOUND_LOCALIZATION_FILE")
	if filename == "" {
		filename = "/nakama/data/localization.xlsx"
	}
	doc, err := localization.ReadWorkbook(filename)
	if err != nil {
		return fmt.Errorf("localization workbook: %w", err)
	}
	if err = publishLocalization(ctx, nk, doc); err != nil {
		return err
	}
	return i.RegisterRpc("localization_bootstrap", localizationBootstrap)
}
func localizationBootstrap(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	if _, err := userID(ctx); err != nil {
		return "", err
	}
	var request struct {
		CachedVersion string `json:"cached_version"`
	}
	if payload != "" {
		if err := json.Unmarshal([]byte(payload), &request); err != nil {
			return "", runtime.NewError("Invalid localization request", 3)
		}
	}
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: configCollection, Key: localizationKey, UserID: systemStorageOwner}})
	if err != nil {
		return "", err
	}
	if len(objects) != 1 {
		return "", runtime.NewError("Localization is unavailable", 13)
	}
	var response publishedLocalization
	if err = json.Unmarshal([]byte(objects[0].Value), &response); err != nil {
		return "", err
	}
	if response.Version == request.CachedVersion {
		response.Document = nil
	}
	raw, err := json.Marshal(response)
	return string(raw), err
}
