# Character catalog

The runtime source is `data/characters.json`. Nakama loads it at startup and rejects invalid identities, group definitions, card costs, copy limits, paths, behavior keys, crops and star stats. `RIFTBOUND_CHARACTERS_FILE` can override the path. Excel templates are no longer synchronized with runtime changes.

## Character groups and decks

Group names and numeric IDs remain stable for saved-data compatibility. Ranger, Mage, Guardian and Neutral (groups 1–4) are retired and all their definitions are disabled. Shop, collection, hero selection and combat simulator exclude them. The remaining **41 active character groups use 41 distinct scene/texture sets**. JSON validation rejects two active groups that share a character scene.

A deck selects three different active characters and **30 distinct card kinds**: two cards at each cost 2–6 for each selected group, with one to four copies per kind. Neutral slots are removed. The free starter groups are **Knight, Orc and Demon**.

Existing saved decks keep their IDs, names, selected status and copy counts. Bootstrap replaces retired hero slots with unused active groups, removes Neutral and old unselected support cards, and grants ownership of replacement cards without a wallet charge. Deleted deck lists remain empty. Offline deck migration preserves a backup at `user://decks-before-character-groups.json`. Retired character IDs remain reserved; unrelated characters are never renumbered.

The original Orc Raider (kind 60), Demon (61) and Blood Monster (62) retain their kinds and groups 5, 6 and 7. They remain available in decks selecting their respective characters.

## Card backs

Twenty existing designs are defined in `internal/game/data/card_backs.json`. `card_back_00` is free; the other nineteen cost 100 wallet coins each. `card_back_purchase` uses account-bound ownership, server prices, atomic wallet/profile/receipt updates, version checks and retry-safe purchase IDs. Free character access does not unlock paid card backs.

Saved and runtime decks carry one `card_back_id`; an omitted value selects the free default. Ownership is enforced for online deck save, rooms and matchmaking. Player snapshots include the selected back for opponent hands, reconnects and replays. Buying a back does not select it automatically or change existing decks. The client selects it in **Decks → Edit deck → Choose card back**, then saves the deck.

## Update definitions

1. Edit the JSON catalog. Keep existing `kind` and `character_id` stable, append consecutive kinds, and provide ordered stat rows for stars 1–4.
2. Include new scenes/images in the client before enabling their server definitions. Character scenes must contain `BattleUnit`, `AnimatedSprite2D` and idle/walk/attack/hit/die animations.
3. Validate and regenerate the packaged client fallback:

```sh
go run ./cmd/catalogtool -json /path/to/riftbound-tactics/data/characters.json
```

4. Rebuild and restart the backend, then log in again:

```sh
docker compose up -d --build nakama
```

The authenticated `character_catalog` RPC supplies the same data at Guest login. Loading happens at startup; restarting ends active matches. Both teams instantiate the card's `scene_path`, with opposite facing and outlines identifying enemies. The legacy `enemy_scene_path` mirrors `scene_path`.

HP, damage range and speed affect combat. Attack is the displayed attack value. Supported mechanics remain `basic_attack`/`each_turn` and passive `none`. New shields, healing, stun or passive mechanics require combat implementation. Buying depletes only the player's chosen deck pool; selling does not return copies.

`internal/game/data/characters.json` and `internal/game/testdata/characters.xlsx` are fixed legacy fixtures for isolated tests. They are not the runtime catalog. The catalog tool can still read an explicitly supplied `.xlsx` for legacy imports; deployment defaults to JSON.
