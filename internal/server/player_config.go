package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
)

const playerCollection = "riftbound_player"
const playerKey = "collection_decks"
const configCollection = "riftbound_config"
const configKey = "published"

type economySettings struct {
	FreeCardBacks      bool             `json:"free_card_backs"`
	FreeCharacterCards bool             `json:"free_character_cards"`
	AllUnlocked        bool             `json:"all_unlocked"`
	Currency           string           `json:"currency"`
	Prices             map[string]int64 `json:"prices"`
}
type gameConfiguration struct {
	CardBacks     []game.CardBackDefinition `json:"card_backs"`
	SchemaVersion int                       `json:"schema_version"`
	Catalog       game.CharacterCatalog     `json:"catalog"`
	Economy       economySettings           `json:"economy"`
}

var activeConfig *gameConfiguration
var activeConfigVersion string

func defaultConfiguration(c game.CharacterCatalog) gameConfiguration {
	prices := map[string]int64{}
	for _, ch := range c.Characters {
		prices[ch.ID] = 100
	}
	return gameConfiguration{CardBacks: game.CardBacks, SchemaVersion: 1, Catalog: c, Economy: economySettings{FreeCardBacks: false, FreeCharacterCards: true, AllUnlocked: false, Currency: "coins", Prices: prices}}
}
func configurationHash(c gameConfiguration) string {
	raw, _ := json.Marshal(c)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func validateConfiguration(c gameConfiguration) error {
	if c.SchemaVersion != 1 || c.Economy.Currency != "coins" {
		return errors.New("Unsupported configuration schema or currency")
	}
	if err := game.ValidateCharacterCatalog(c.Catalog); err != nil {
		return err
	}
	for _, ch := range c.Catalog.Characters {
		if price, ok := c.Economy.Prices[ch.ID]; !ok || price < 1 || price > 1000000000 {
			return errors.New("Each character requires a positive shop price")
		}
	}
	old := game.Catalog()
	if len(c.Catalog.Characters) < len(old.Characters) || len(c.Catalog.GroupNames) != len(old.GroupNames) {
		return errors.New("Published character IDs and groups cannot be removed")
	}
	for i, ch := range old.Characters {
		if c.Catalog.Characters[i].ID != ch.ID || c.Catalog.Characters[i].Group != ch.Group {
			return errors.New("Character IDs and groups must remain stable")
		}
	}
	for i, n := range old.GroupNames {
		if c.Catalog.GroupNames[i] != n {
			return errors.New("Group identities must remain stable")
		}
	}
	return nil
}
func retireStarterGroups(c *gameConfiguration) {
	c.CardBacks = game.CardBacks
	c.Economy.FreeCharacterCards = true
	c.Economy.FreeCardBacks = false
	if c.Catalog.RosterVersion < game.Catalog().RosterVersion {
		c.Catalog = game.Catalog()
		if c.Economy.Prices == nil {
			c.Economy.Prices = map[string]int64{}
		}
		for _, ch := range c.Catalog.Characters {
			if c.Economy.Prices[ch.ID] <= 0 {
				c.Economy.Prices[ch.ID] = 100
			}
		}
	}
	for i := range c.Catalog.Characters {
		if c.Catalog.Characters[i].MaxCopies > 4 {
			c.Catalog.Characters[i].MaxCopies = 4
		}
		if c.Catalog.Characters[i].ID == "healing_totem" || c.Catalog.Characters[i].SupportEffect != "" {
			c.Catalog.Characters[i].Enabled = true
		} else if c.Catalog.Characters[i].Group < 45 {
			c.Catalog.Characters[i].Enabled = false
		}
	}
}

func initializePlayerStorage(ctx context.Context, nk runtime.NakamaModule, i runtime.Initializer, c game.CharacterCatalog) error {
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: configCollection, Key: configKey, UserID: systemStorageOwner}})
	if err != nil {
		return err
	}
	config := defaultConfiguration(c)
	if len(objects) == 0 {
		raw, _ := json.Marshal(config)
		_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: configCollection, Key: configKey, Value: string(raw), Version: "*", PermissionRead: 0, PermissionWrite: 0}})
		if errors.Is(err, runtime.ErrStorageRejectedVersion) {
			objects, err = nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: configCollection, Key: configKey, UserID: systemStorageOwner}})
		}
		if err != nil {
			return err
		}
	}
	if len(objects) > 0 {
		config = gameConfiguration{}
		if err = json.Unmarshal([]byte(objects[0].Value), &config); err != nil {
			return err
		}
	}
	retireStarterGroups(&config)
	if err = validateConfiguration(config); err != nil {
		return err
	}
	// Persist the roster upgrade so administration sees the same catalog as players.
	if len(objects) > 0 {
		raw, _ := json.Marshal(config)
		var stored gameConfiguration
		_ = json.Unmarshal([]byte(objects[0].Value), &stored)
		if configurationHash(stored) != configurationHash(config) {
			_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: configCollection, Key: configKey, UserID: systemStorageOwner, Value: string(raw), Version: objects[0].Version, PermissionRead: 0, PermissionWrite: 0}})
			if err != nil {
				return err
			}
		}
	}
	// Immutable for the lifetime of the node: publishing does not change running battles.
	activeConfig = &config
	activeConfigVersion = configurationHash(config)
	game.UseCatalog(config.Catalog)
	for name, fn := range map[string]func(context.Context, runtime.Logger, *sql.DB, runtime.NakamaModule, string) (string, error){
		"player_bootstrap": playerBootstrap, "player_decks_save": playerDecksSave, "character_purchase": characterPurchase, "character_group_purchase": characterGroupPurchase, "card_back_purchase": cardBackPurchase,
		"admin_config_get": adminConfigGet, "admin_config_publish": adminConfigPublish, "admin_player_grant": adminPlayerGrant,
	} {
		if err = i.RegisterRpc(name, fn); err != nil {
			return err
		}
	}
	return nil
}
