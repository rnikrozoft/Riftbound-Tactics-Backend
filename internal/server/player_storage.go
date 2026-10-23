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

type savedCard struct {
	CharacterID string `json:"character_id"`
	Copies      int    `json:"copies"`
}
type savedDeck struct {
	CardBackID string      `json:"card_back_id,omitempty"`
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Heroes     []int       `json:"heroes"`
	Cards      []savedCard `json:"cards"`
}
type playerData struct {
	CardBackShopVersion int         `json:"card_back_shop_version,omitempty"`
	OwnedCardBacks      []string    `json:"owned_card_backs"`
	SchemaVersion       int         `json:"schema_version"`
	Owned               []string    `json:"owned_characters"`
	Decks               []savedDeck `json:"decks"`
	SelectedID          string      `json:"selected_deck_id"`
}
type playerView struct {
	UserID         string             `json:"user_id"`
	ConfigVersion  string             `json:"config_version"`
	Config         *gameConfiguration `json:"config,omitempty"`
	Profile        playerData         `json:"profile"`
	ProfileVersion string             `json:"profile_version"`
	Wallet         map[string]int64   `json:"wallet"`
}

func characterKind(id string) (int, bool) {
	for _, c := range game.Catalog().Characters {
		if c.ID == id {
			return c.Kind, true
		}
	}
	return 0, false
}
func toSavedDeck(id string, d game.Deck) savedDeck {
	s := savedDeck{CardBackID: game.NormalizeCardBackID(d.CardBackID), ID: id, Name: d.Name, Heroes: d.Heroes, Cards: []savedCard{}}
	for _, c := range d.Cards {
		s.Cards = append(s.Cards, savedCard{game.Character(c.Kind).ID, c.Copies})
	}
	return s
}
func decodeSavedDeck(d savedDeck) (game.Deck, error) {
	out := game.Deck{CardBackID: game.NormalizeCardBackID(d.CardBackID), Name: d.Name, Heroes: d.Heroes}
	if !storageIDPattern.MatchString(d.ID) {
		return out, errors.New("Invalid deck ID")
	}
	for _, c := range d.Cards {
		kind, ok := characterKind(c.CharacterID)
		if !ok {
			return out, errors.New("Unknown character ID")
		}
		out.Cards = append(out.Cards, game.DeckCard{Kind: kind, Copies: c.Copies})
	}
	return out, game.ValidateDeck(out)
}
func collectionAllows(p playerData, d game.Deck) bool {
	if !ownsCardBack(p, d.CardBackID) {
		return false
	}
	if activeConfig == nil || activeConfig.Economy.AllUnlocked {
		return true
	}
	owned := map[string]bool{}
	for _, id := range p.Owned {
		owned[id] = true
	}
	for _, card := range d.Cards {
		if !owned[game.Character(card.Kind).ID] {
			return false
		}
	}
	return true
}
func playerWrite(id string, p playerData, version string) *runtime.StorageWrite {
	raw, _ := json.Marshal(p)
	return &runtime.StorageWrite{Collection: playerCollection, Key: playerKey, UserID: id, Value: string(raw), Version: version, PermissionRead: 1, PermissionWrite: 0}
}
func readPlayer(ctx context.Context, nk runtime.NakamaModule, id string) (playerData, string, error) {
	objects, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: playerCollection, Key: playerKey, UserID: id}})
	if err != nil {
		return playerData{}, "", err
	}
	if len(objects) == 0 {
		return playerData{}, "", nil
	}
	var p playerData
	if err = json.Unmarshal([]byte(objects[0].Value), &p); err != nil {
		return p, "", err
	}
	if p.SchemaVersion != 1 {
		return p, "", errors.New("Unsupported player storage schema")
	}
	return p, objects[0].Version, nil
}

// Replace retired hero slots without changing stable character IDs or unrelated cards.
func repairRetiredPlayer(p *playerData) bool {
	changed := false
	owned := map[string]bool{}
	for _, id := range p.Owned {
		owned[id] = true
	}
	for di := range p.Decks {
		deck := &p.Decks[di]
		heroes := append([]int{}, deck.Heroes...)
		cards := []savedCard{}
		repaired := false
		for _, card := range deck.Cards {
			kind, ok := characterKind(card.CharacterID)
			selected := false
			if ok {
				for _, g := range heroes {
					if game.Character(kind).Group == g {
						selected = true
						break
					}
				}
			}
			if ok && !selected {
				repaired = true
				continue
			}
			cards = append(cards, card)
		}
		originalCards := append([]savedCard{}, cards...)
		for i, card := range cards {
			if kind, ok := characterKind(card.CharacterID); ok && card.Copies > game.Character(kind).MaxCopies {
				cards[i].Copies = game.Character(kind).MaxCopies
				repaired = true
			}
		}
		for i, group := range heroes {
			if game.IsHeroGroup(group) {
				continue
			}
			replacement := -1
			preferred := -1
			for _, old := range game.Catalog().Characters {
				if old.Group != group {
					continue
				}
				for _, current := range game.Catalog().Characters {
					if current.Enabled && current.ScenePath == old.ScenePath {
						preferred = current.Group
						break
					}
				}
				break
			}
			candidates := append([]int{preferred}, game.HeroGroups()...)
			for _, candidate := range candidates {
				if !game.IsHeroGroup(candidate) {
					continue
				}
				used := false
				for _, selected := range heroes {
					if selected == candidate {
						used = true
					}
				}
				if !used {
					replacement = candidate
					break
				}
			}
			if replacement < 0 {
				continue
			}
			for cost := 2; cost <= 6; cost++ {
				options := []game.CharacterDefinition{}
				for _, c := range game.Catalog().Characters {
					if c.Enabled && c.Group == replacement && c.Cost == cost {
						options = append(options, c)
					}
				}
				n := 0
				for ci, card := range cards {
					kind, ok := characterKind(card.CharacterID)
					if !ok {
						continue
					}
					c := game.Character(kind)
					if c.Group != group || c.Cost != cost || n >= len(options) {
						continue
					}
					cards[ci].CharacterID = options[n].ID
					if cards[ci].Copies > options[n].MaxCopies {
						cards[ci].Copies = options[n].MaxCopies
					}
					n++
				}
			}
			heroes[i] = replacement
			repaired = true
		}
		for ci, card := range cards {
			kind, ok := characterKind(card.CharacterID)
			if !ok || game.Character(kind).Enabled {
				continue
			}
			old := game.Character(kind)
			for _, replacement := range game.Catalog().Characters {
				if !replacement.Enabled || replacement.Group != old.Group || replacement.Cost != old.Cost {
					continue
				}
				used := false
				for _, other := range cards {
					if other.CharacterID == replacement.ID {
						used = true
						break
					}
				}
				if used {
					continue
				}
				cards[ci].CharacterID = replacement.ID
				if cards[ci].Copies > replacement.MaxCopies {
					cards[ci].Copies = replacement.MaxCopies
				}
				repaired = true
				break
			}
		}
		if repaired {
			candidate := savedDeck{CardBackID: deck.CardBackID, ID: deck.ID, Name: deck.Name, Heroes: heroes, Cards: cards}
			if _, err := decodeSavedDeck(candidate); err != nil {
				continue
			}
			for i, card := range cards {
				if card.CharacterID != originalCards[i].CharacterID {
					owned[card.CharacterID] = true
				}
			}
			*deck = candidate
			changed = true
		}
	}
	// Keep historical ownership and transfer each owned asset to its new card identity.
	for _, id := range p.Owned {
		kind, ok := characterKind(id)
		if !ok {
			continue
		}
		old := game.Character(kind)
		for _, current := range game.Catalog().Characters {
			if current.Enabled && current.ScenePath == old.ScenePath && !owned[current.ID] {
				owned[current.ID] = true
				changed = true
			}
		}
	}
	// A previously purchased full group remains a full unlock in its new home.
	legacyCounts := map[int]int{}
	for _, id := range p.Owned {
		if kind, ok := characterKind(id); ok && !game.Character(kind).Enabled {
			legacyCounts[game.Character(kind).Group]++
		}
	}
	for group, count := range legacyCounts {
		if count < 10 {
			continue
		}
		scene := ""
		for _, old := range game.Catalog().Characters {
			if old.Group == group {
				scene = old.ScenePath
				break
			}
		}
		target := -1
		for _, current := range game.Catalog().Characters {
			if current.Enabled && current.ScenePath == scene {
				target = current.Group
				break
			}
		}
		for _, current := range game.Catalog().Characters {
			if current.Enabled && current.Group == target && !owned[current.ID] {
				owned[current.ID] = true
				changed = true
			}
		}
	}
	if changed {
		p.Owned = nil
		for id := range owned {
			p.Owned = append(p.Owned, id)
		}
		sort.Strings(p.Owned)
	}
	return changed
}

func containsOwnedBack(ids []string, id string) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}

// Grant actual permanent ownership without touching decks, wallet or cosmetics.
func grantFreeCharacterCards(p *playerData) bool {
	if activeConfig == nil {
		return false
	}
	owned := map[string]bool{}
	for _, id := range p.Owned {
		owned[id] = true
	}
	changed := false
	for _, c := range activeConfig.Catalog.Characters {
		if activeConfig.Economy.FreeCharacterCards && c.Enabled && !owned[c.ID] {
			p.Owned = append(p.Owned, c.ID)
			owned[c.ID] = true
			changed = true
		}
	}
	if activeConfig.Economy.FreeCardBacks {
		for _, back := range game.CardBacks {
			if !containsOwnedBack(p.OwnedCardBacks, back.ID) {
				p.OwnedCardBacks = append(p.OwnedCardBacks, back.ID)
				changed = true
			}
		}
		sort.Strings(p.OwnedCardBacks)
	}
	if changed {
		sort.Strings(p.Owned)
	}
	return changed
}

func ensurePlayer(ctx context.Context, nk runtime.NakamaModule, id string, legacy []savedDeck, selected string) (playerData, string, error) {
	p, v, err := readPlayer(ctx, nk, id)
	if err != nil {
		return p, v, err
	}
	if v != "" {
		repaired := repairRetiredPlayer(&p)
		granted := grantFreeCharacterCards(&p)
		restored, restoreErr := restorePaidCardBacks(ctx, nk, id, &p)
		if restoreErr != nil {
			return p, v, restoreErr
		}
		if repaired || granted || restored {
			acks, writeErr := nk.StorageWrite(ctx, []*runtime.StorageWrite{playerWrite(id, p, v)})
			if errors.Is(writeErr, runtime.ErrStorageRejectedVersion) {
				return readPlayer(ctx, nk, id)
			}
			if writeErr != nil {
				return p, v, writeErr
			}
			if len(acks) != 1 {
				return p, v, errors.New("Player migration acknowledgement missing")
			}
			v = acks[0].Version
		}
		return p, v, nil
	}
	starter := toSavedDeck("starter", game.DefaultDeck())
	p = playerData{CardBackShopVersion: 1, OwnedCardBacks: []string{game.DefaultCardBackID}, SchemaVersion: 1, Owned: []string{}, Decks: []savedDeck{starter}, SelectedID: starter.ID}
	// Starter ownership is expanded by the temporary free-card distribution below.
	for _, c := range starter.Cards {
		p.Owned = append(p.Owned, c.CharacterID)
	}
	grantFreeCharacterCards(&p)
	sort.Strings(p.Owned)
	if len(legacy) > 0 && len(legacy) <= 32 {
		imported := playerData{Decks: legacy, Owned: append([]string{}, p.Owned...)}
		repairRetiredPlayer(&imported)
		legacy = imported.Decks
		decks := []savedDeck{}
		seen := map[string]bool{}
		for _, deck := range legacy {
			d, e := decodeSavedDeck(deck)
			if e == nil && !seen[deck.ID] && collectionAllows(p, d) {
				decks = append(decks, deck)
				seen[deck.ID] = true
			}
		}
		if len(decks) > 0 {
			p.Decks = decks
			p.SelectedID = decks[0].ID
			if seen[selected] {
				p.SelectedID = selected
			}
		}
	}
	// Persist an explicit zero wallet together with the initial collection, once only.
	acks, _, err := nk.MultiUpdate(ctx, nil, []*runtime.StorageWrite{playerWrite(id, p, "*")}, nil, []*runtime.WalletUpdate{{UserID: id, Changeset: map[string]int64{"coins": 0}, Metadata: map[string]interface{}{"source": "player_initialize"}}}, true)
	if errors.Is(err, runtime.ErrStorageRejectedVersion) {
		return readPlayer(ctx, nk, id)
	}
	if err != nil {
		return p, "", err
	}
	if len(acks) != 1 {
		return p, "", errors.New("Player storage acknowledgement missing")
	}
	return p, acks[0].Version, nil
}
func viewPlayer(ctx context.Context, nk runtime.NakamaModule, id string, p playerData, v, cached string) (string, error) {
	account, err := nk.AccountGetId(ctx, id)
	if err != nil {
		return "", err
	}
	wallet := map[string]int64{}
	if err = json.Unmarshal([]byte(account.Wallet), &wallet); err != nil {
		return "", err
	}
	view := playerView{UserID: id, ConfigVersion: activeConfigVersion, Profile: p, ProfileVersion: v, Wallet: wallet}
	if cached != activeConfigVersion {
		view.Config = activeConfig
	}
	raw, err := json.Marshal(view)
	return string(raw), err
}
func playerBootstrap(ctx context.Context, l runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	var req struct {
		Cached   string      `json:"cached_config_version"`
		Legacy   []savedDeck `json:"legacy_decks"`
		Selected string      `json:"legacy_selected_id"`
	}
	if len(payload) > 128*1024 || json.Unmarshal([]byte(payload), &req) != nil {
		return "", runtime.NewError("Invalid bootstrap request", 3)
	}
	p, v, err := ensurePlayer(ctx, nk, id, req.Legacy, req.Selected)
	if err != nil {
		return "", err
	}
	return viewPlayer(ctx, nk, id, p, v, req.Cached)
}
func playerDecksSave(ctx context.Context, l runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	id, err := userID(ctx)
	if err != nil {
		return "", err
	}
	var req struct {
		Decks         []savedDeck `json:"decks"`
		Selected      string      `json:"selected_deck_id"`
		Version       string      `json:"profile_version"`
		ConfigVersion string      `json:"config_version"`
	}
	if len(payload) > 128*1024 || json.Unmarshal([]byte(payload), &req) != nil || req.Decks == nil || len(req.Decks) > 32 {
		return "", runtime.NewError("Save 0-32 valid decks", 3)
	}
	if req.ConfigVersion != activeConfigVersion {
		return "", runtime.NewError("Game configuration changed; login again", 9)
	}
	p, v, err := ensurePlayer(ctx, nk, id, nil, "")
	if err != nil {
		return "", err
	}
	if req.Version != v {
		return "", runtime.NewError("Collection changed; reload before saving", 10)
	}
	seen := map[string]bool{}
	for _, deck := range req.Decks {
		d, e := decodeSavedDeck(deck)
		if e != nil {
			return "", runtime.NewError(e.Error(), 3)
		}
		if seen[deck.ID] {
			return "", runtime.NewError("Duplicate deck ID", 3)
		}
		seen[deck.ID] = true
		if !collectionAllows(p, d) {
			return "", runtime.NewError("Deck contains a character you do not own", 7)
		}
	}
	if (len(req.Decks) > 0 && !seen[req.Selected]) || (len(req.Decks) == 0 && req.Selected != "") {
		return "", runtime.NewError("Select a saved deck", 3)
	}
	p.Decks = req.Decks
	p.SelectedID = req.Selected
	acks, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{playerWrite(id, p, v)})
	if err != nil {
		return "", err
	}
	return viewPlayer(ctx, nk, id, p, acks[0].Version, req.ConfigVersion)
}
func validateOwnedDeck(ctx context.Context, nk runtime.NakamaModule, id string, d game.Deck) error {
	if err := game.ValidateDeck(d); err != nil {
		return err
	}
	if (activeConfig == nil || activeConfig.Economy.AllUnlocked) && game.NormalizeCardBackID(d.CardBackID) == game.DefaultCardBackID {
		return nil
	}
	p, _, err := ensurePlayer(ctx, nk, id, nil, "")
	if err != nil {
		return err
	}
	if !collectionAllows(p, d) {
		return runtime.NewError("Deck contains a character you do not own", 7)
	}
	return nil
}
