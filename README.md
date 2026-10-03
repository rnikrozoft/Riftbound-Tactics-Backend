# Riftbound Tactics Backend

Go authoritative matches on Nakama 3.37.0 and PostgreSQL 16.8. The plugin builder and server versions are pinned together for Go ABI compatibility.

## Start locally

```powershell
docker compose up -d --build
docker compose ps
docker compose logs -f nakama
```

Game endpoint: http://127.0.0.1:7350. Local Nakama console: http://127.0.0.1:7351 (admin / riftbound-debug-console). Development server key: riftbound-debug-key. Configuration is for local development; replace credentials and configure TLS before public deployment.

Stop while preserving database: `docker compose down`. PostgreSQL uses the named `postgres_data` volume. Tables `rift_rooms` and `rift_battles` store room lookup and complete computed battle results.

## Play with two clients

Run two instances of the Godot .NET client. CREATE assigns player A and a six-digit code. Enter that code and JOIN in the second instance to become B. Each game process authenticates with a fresh random device ID and creates a new Nakama user; it never reuses a saved session.

Preparation begins once both players join. Each player gets three randomized shop offers at shop level 2 (up to five at higher levels), up to ten hand cards and six deployment slots. The countdown is 60 seconds. Both players pressing BATTLE starts early; otherwise the server starts at the deadline. Ready players cannot edit their formations. Empty formations are permitted and resolve as a draw or forfeit.

Each client sees its own heroes on the left with white outlines and opponents on the right with red outlines. Opponent formations are withheld from preparation snapshots and revealed only when combat begins, including on subsequent rounds. Shop offers and hands remain private. The server validates every action, computes the complete attack/HP/death sequence, persists it, and distributes the same timestamped replay to both clients. Clients animate those events without calculating damage. Battle starts 1.5 seconds after computation to allow delivery. NEXT requires both players; cards and formations persist and deployed veterans cannot return to hand.

For a phone, set the client's OnlineBattle Host inspector property or `RIFTBOUND_HOST` to this PC's LAN address. Port 7350 must be reachable. Desktop defaults to localhost.

## Protocol

Authenticated socket RPCs: `room_create`, `room_join` with `{ "code": "123456" }`, and `server_clock`.

Match data opcode 1: action `{ type, token, slot, round, sequence }`. Types: buy, deploy, move, return, sell, reroll, lock, upgrade, ready, next. Move onto an occupied owned slot swaps units. Selling removes a card or deployed unit. The server checks ownership, phase, round, capacity, veteran restrictions and monotonic sequence numbers.

Opcode 2: state snapshot with revision, server_ms, per-client ack_sequence, phase, round, deadline_ms, players and optional battle. Opponent offers/hand are empty. Opcode 3: rejection containing message and sequence. Clients lock edits while awaiting acknowledgement and render the server's accepted state.

The battle plan contains unit IDs, initial HP, ordered attack events, absolute at_ms timestamps, final HP/death per hit, winner and end_ms. Clock synchronization uses socket RPC round-trip samples. Late clients skip completed attack visuals and apply their server HP. A third player or a second simultaneous session for an occupied seat is rejected.

A disconnected seat remains reserved to its existing user. A newly launched debug process gets a different ID, so create a new room to test again. Fully empty rooms terminate after 60 seconds; waiting rooms terminate after ten minutes. Room lookup expires after 30 minutes. Active match state is in memory; restarting Nakama ends active matches while stored battle results remain.

## Validation

```powershell
go test ./...
docker run --rm --entrypoint go --mount "type=bind,source=$PWD,target=/backend" --workdir /backend heroiclabs/nakama-pluginbuilder:3.37.0 test -race ./...
```

The Docker build also runs Go tests before building the plugin. Tests cover ownership, room capacity, deadlines, ready locks, limits, swapping, veterans, persistent next rounds and complete combat plans. Client integration runs two real Nakama sessions, rejects a third, checks mirrored slots and identical replays, and waits the actual 60-second preparation timeout.

Opponent snapshots expose hand_count during battle and finished phases, while hand remains an empty array. Preparation exposes no opponent hand count. The client displays this count as card backs during battle only.

## Match economy

Marvel Duel-inspired baseline: shop starts at level 2, max level 6; slots by level 2�6 are 3/4/4/5/5. Base next-step upgrade costs are 2/8/10/14. Each new round discounts the pending upgrade by 2 (minimum 0); upgrading resets the next step to its base price. Upgrading leaves offers unchanged; new capacity applies on reroll or the normal next-round refresh/refill. Round-start income is 4/6/9/12/15/18/20, capped at 20 from round 7. Unspent Coin carries over.

Reroll costs 2 each time and draws distinct eligible kinds whose price is at most shop level. Lock is free and preserves remaining offers while filling vacancies at the next preparation. Unlock before paid reroll. The mock catalog has 30 definitions, six per cost 2�6, with four copies each per player. Buying depletes that player's pool; selling does not replenish it. Empty pools can yield partially empty shops.

User customization: selling refunds floor(original purchase price / 2), including deployed units. Battle completion awards no Coin for wins, losses or draws. Both players receive the scheduled income only at round start. Prices survive deploy/return. All rules are server authoritative. New `shop_level` and `upgrade_cost` snapshot fields drive the client's upgrade button. See the client's docs/marvel-duel-shop-research.md for sources, version limitations and the mock-catalog scope.

Player HP starts at 30 and persists between rounds. Battle damage = winning shop level + sum of surviving character stars (one to four stars). A draw deals zero damage. Zero HP produces terminal `game_over` with a winner; shop, ready and next-round actions are rejected. The client displays player HP beside Coin in both profiles, and shows a large central countdown with asset particles during the last 15 seconds of preparation.

Buying the same card kind again upgrades its existing hand card or returns its upgraded deployed unit to hand, one additional star per purchased copy (1/2/3/4 stars, four copies maximum). Merges preserve the original token, slot and veteran flag and are allowed with a full hand/field. A capped character cannot be purchased again. HP = 100 � stars, Attack = 30 � stars, Speed = 10 + 2 � (stars - 1); combat damage scales by stars and turn cadence uses Speed. Paid cost accumulates on the combined character, and selling refunds floor(total paid / 2). Upgrades retain stars through deploy/return and next rounds. The client uses existing TravelBookLite star icons and animated Super Pixel Effects Gigapack Level Up/heal particles; no generated effect artwork.

The field remains six units (independent of the five-offer shop). Ready only confirms battle readiness: until the actual battle phase/deadline, a ready player may sell, move/swap, buy and deploy into vacant field slots. Returning any field unit to hand is forbidden once ready, including newly deployed units. Edits retain ready status and do not alter the deadline. Actual battle freezes preparation actions.

Upgrading a deployed character automatically removes it from its field slot and returns the upgraded card to hand, even after Ready. Token, stars, investment and veteran status persist. The existing asset upgrade animation plays on the returned hand card. Field upgrades require a free hand slot; if full, the purchase is rejected without charging coins or consuming copies/offers. Upgrading an existing hand card still works with a full hand because it occupies the same slot. Manual return restrictions remain unchanged.
