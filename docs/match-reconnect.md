# Reopening an unfinished match

The server stores a private, system-owned active-match pointer per human account.
The authenticated `active_match` RPC checks that the authoritative match still
exists and signals it to verify membership, HP and game-over status. Clients
cannot supply another account ID or read/write these pointers directly. Stale
pointers are removed using their storage version; transient failures retain them.

On startup, a client with a saved device identity authenticates again, discovers
the active match, and uses the existing recovery flow to rejoin and receive an
authoritative snapshot. Deck, formation, hand and economy remain in the match;
the client does not recreate them from local saves. A replacement session resets
its action sequence and supersedes an older presence.

Closing the window leaves the pointer intact. Explicitly returning to the lobby
or leaving for the defeat replay calls `active_match_leave`, which only removes
the authenticated account's pointer if its match ID still matches the request.
This does not introduce new surrender or combat rules.

Started matches no longer terminate merely because all sockets have been absent
for 60 seconds. Their ordinary game clocks continue. Waiting rooms expire after
10 minutes; completed empty matches retain their existing cleanup delay. A
private match waiting for a next-round action can therefore remain alive while
all players are offline.

Match simulation still lives in Nakama process memory: restarting the backend
does not restore a match, and discovery discards pointers to missing matches.
The client and updated backend plugin must both be deployed for this flow.

Validation: `go test ./...`, `go vet ./...`, and client
`dotnet build RiftboundTactics.csproj --no-restore`. Active-match unit tests cover
account scoping, private storage, versioned stale cleanup, transient failures,
and protection against an old session clearing a newer match pointer.

Live validation completed against the rebuilt local Docker Nakama service using
`tests/cold_reconnect.tscn` in separate Godot processes. Run with
`RIFT_RECONNECT_PHASE=seed`, followed by `resume` to verify match discovery,
coins, deployed token/slot, round, accepted actions and explicit-leave cleanup.
Run `seed` again followed by `ui` to exercise Welcome -> Lobby recovery ->
gameplay automatically with the original match and authoritative state. Fixture
accounts are separate from saved player accounts; the UI test injects only its
fixture identity in memory. Godot reports two ObjectDB instances at test exit;
both reconnect checks exit successfully.

## One active login per account

`local.yml` enables Nakama's native `session.single_session` and
`session.single_socket`. A newer authentication invalidates old access and
refresh tokens; connecting its socket disconnects older sockets. The client
handles the server's `single_socket` notification and checks its authenticated
account every five seconds, including HTTP-only lobby screens. Invalidated
clients return to Welcome with a takeover notice and require an explicit login;
they never authenticate automatically to reclaim the account. Ordinary network
failures do not trigger this forced-login path. The active-match record remains
available to the latest login.

`tests/single_account.tscn` passed against the running local Nakama service in
both `RIFT_SINGLE_ACCOUNT_MODE=socket` and `http`. Tests verify the forced-login
screen and notice, rejected old access and refresh tokens, a valid newest login,
and preservation/rejoin of the active match for the socket case. These simulate
two clients on one host, rather than two physical computers.
