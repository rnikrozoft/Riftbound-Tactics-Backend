# Nakama API and persistence audit

Reviewed 2026-10-23 against Nakama 3.37.0 / nakama-common 1.44.2 and the configured single-server deployment.

## Feature coverage

| Feature | Client/server API | Checks and behavior |
| --- | --- | --- |
| Guest login and profile | AuthenticateDevice, GetAccount, UpdateAccount | SDK authentication and refresh; server session/socket replacement settings retained. Reconnect reuses the session without authenticating another token. |
| Character catalog | authenticated character_catalog RPC | Immutable active catalog serialized once at startup; package assets remain client resources. |
| Translations | localization_bootstrap RPC, StorageRead/StorageWrite | Private system-owned configuration, versioned publication and conditional download. |
| Collection, starter cards and decks | player_bootstrap / player_decks_save, StorageRead/StorageWrite | Owner-readable/server-writable object, version checks, authoritative deck and ownership validation. |
| Character/group/card-back purchases | purchase RPCs, MultiUpdate | Profile, create-only request receipt and wallet debit commit atomically; bounded retries, trusted server prices, wallet ledger enabled. |
| Admin grants | admin_player_grant, MultiUpdate | Runtime HTTP-key administration; player tokens denied; receipt, profile and wallet credit commit together. |
| Configuration publication | admin_config_get / admin_config_publish, StorageWrite | Current storage version required; private published document; running matches retain immutable active configuration. |
| Private room codes | room_create / room_join, StorageWrite/Read/Delete, MatchCreate/Get | Create-only code reservation followed by CAS publication; owner/match/version checked before cleanup. |
| Matchmaking | MatchmakerAdd/Remove, before hook, MatchmakerMatched, MatchCreate | Rating/deck/session validated by server; original gap widening, player counts and bot fallback retained. See deployment boundary below. |
| Room and league gameplay | authoritative Match handlers, SendMatchState/BroadcastMessage | Game state lives in the match; ownership, phase, round and action sequence checks retained; per-recipient private-state redaction retained. |
| Reconnect and elimination | JoinMatch, MatchKick, presence session IDs | Old session leave events cannot disconnect replacements. Terminal rating is committed and included before the eliminated client's final snapshot/kick. |
| Battle archives | StorageWrite, riftbound_battles | Private immutable object per match/round, create-only idempotency; original room plan / league plan-array shapes retained inside the result field. |
| Skill rating | mmr_self, StorageRead/StorageWrite | Per-owner account CAS and create-only match receipt commit in one atomic batch. Initial 1000, K=32, placement comparisons, floor zero and unrated bot games retain their original calculations. |
| Match wins | StorageRead/StorageWrite, LeaderboardRecordWrite | Per-owner absolute counter and match receipt commit atomically. BEST leaderboard writes prevent delayed lower totals replacing newer totals. |
| Rating leaderboard | LeaderboardRecordWrite, client ListLeaderboardRecords | SET preserves rating decreases; failed/superseded writes remain dirty for reconciliation. Current account is read before every sync. |
| Reconciliation and shutdown | StorageList, RegisterShutdown | Bounded 100-object pages with cursors use authoritative storage, not an index as an existence/count oracle. Background contexts have deadlines; shutdown stops reconciliation and drains in-flight match writes within the configured grace period. |
| Replays | local client archive | Device/account-local replay history and playback remain local; no custom backend table is needed by this feature. |

## Client storage access

Before-write and before-delete API hooks reserve all `riftbound_` collections for server operations. Object permissions alone protect existing objects but do not stop a client from creating a new self-owned object before the server does. The hooks close that first-write gap for inventories, purchase receipts, room records and ratings. Runtime StorageWrite/MultiUpdate calls still perform the authorized server mutations. The live storage tool tests these rejected first-time writes with fresh accounts.

## Changes from the previous backend

No runtime SQL, custom DDL or custom tables are used. The db argument remains only where Nakama requires it in callback signatures. Existing SQL rows are intentionally not imported or deleted. New ratings/win counters begin from their storage defaults, as requested; pre-existing leaderboard rows are not reset by this change.

Battle, win and rating persistence run through bounded workers (32 active operations, 10-second deadline) outside the authoritative tick. Workers receive serialized plans/scalar values and return through buffered channels; only the match goroutine mutates match state. The match retries failed score settlement without applying gameplay twice. Battle writes are attempted during the battle phase and their completion is polled across subsequent phases. As before, this is not a crash-recovery system for in-memory matches.

## Deployment boundary

The supplied compose configuration runs one Nakama node. Matchmaking reservation/cancellation currently uses a mutex-protected, session-scoped in-memory queue around the built-in Nakama matchmaker; rating leaderboard SET writes use owner-striped locks on that node. These retain existing single-node semantics. They are **not a multi-node coordination solution**: horizontal deployment would require shared queue claims and distributed ordering of rating leaderboard writes. Storage CAS protects durable score increments across concurrent writers, but process-local locks do not order cross-node leaderboard writes. Do not treat this audit as cluster certification.

Critical persistence is not sent through Nakama's lossy telemetry Event queue. Archived rounds and receipts use individual objects rather than growing arrays inside a shared object. No new global shared storage counter is introduced.

## Sources

- [Server framework best practices](https://heroiclabs.com/docs/nakama/server-framework/introduction/best-practices/)
- [Storage engine and permissions](https://heroiclabs.com/docs/nakama/concepts/storage/)
- [Go runtime restrictions](https://heroiclabs.com/docs/nakama/server-framework/go-runtime/)

## Validation

- go test ./... and go vet ./...
- Linux plugin-builder: go test -race ./... and plugin compilation, using the pinned server version.
- Unit tests include concurrent/duplicate rating results, rating floor, unrated bot results, immutable battle records, win receipts, failed storage transactions, leaderboard retry/supersession, private permissions, nonblocking tasks and final-rating ordering.
- Live Godot integration validates room RPCs, redaction, replay consistency and the real preparation timeout; the six-human MMR suite validates persistence, elimination, reconnect, leaderboard values and duplicate final ticks.


Final local checks on 2026-10-23: Go tests/vet and Linux race tests passed; Nakama/PostgreSQL reported healthy; live player-storage API checks passed (29 checks); two-client online integration passed (93 checks, including the real 60-second deadline); the final six-human MMR run passed (68 checks). Client native interaction, rendering and combat validation are recorded in the client source-layout document.
