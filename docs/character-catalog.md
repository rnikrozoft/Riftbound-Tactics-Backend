# Character catalog

The runtime source is `data/characters.json`. Nakama loads it at startup and rejects invalid identities, group definitions, card costs, copy limits, paths, behavior keys, crops and star stats. `RIFTBOUND_CHARACTERS_FILE` can override the path. Excel templates are no longer synchronized with runtime changes.

## Character groups and decks

`group_names` supplies names for groups 0–7: Knight, Ranger, Mage, Guardian, Neutral, Orc, Demon and Blood Monster. Group 4 is the separate Neutral collection; it cannot be selected as a character. Each selectable character has two card kinds at each cost 2–6. A deck selects three different characters, their thirty kinds, and ten Neutral kinds (two per cost), with one to four copies per kind.

The original Orc Raider (kind 60), Demon (61) and Blood Monster (62) keep their kinds but now belong to groups 5, 6 and 7 respectively. Each new character has ten starting card kinds using its existing artwork and basic attacks. The additional Raider/Guard cards are starter data, without new special mechanics. Neutral retains its original twenty options.

The client repairs older saved decks that used kinds 60–62 as Neutral by choosing unused Neutral cards of the same cost, preserving names, selected characters and copy counts. It keeps a backup at `user://decks-before-character-groups.json`. Decks that select the new characters can include those original kinds directly.

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
