# Go rewrite review (2026-10-04)

Review of the Go rewrite (`70429f6`..`94da441`) against the last Bash version
(`0595122`), plus the uncommitted 0.10.0 working-tree changes on `go-rewrite`.
Targets: Linux, macOS, Windows; local clients, SSH-shell clients, and
`herdr --remote` clients that may or may not have their own theme.

## Herdr facts this relies on (0.9.3)

- Themes, sidebar layout and other presentation settings come from the
  **client's local config**, including when viewing an SSH machine. Pane
  defaults, integrations and custom commands belong to the server.
  (herdr.dev/docs/configuration, "reload config")
- The UI `reload_config` action (default `prefix+shift+r`) reloads the client's
  local settings and the selected server's config. `herdr server reload-config`
  reloads the server.
- Plugin panes/actions/startup hooks run where panes run (the server). Herdr
  injects `HERDR_ENV=1` into **every** pane, not only plugin panes.
- `PluginInvocationContext` has no client ID. There is no per-client theme API.
  The only server→client channels are `client.window_title.*` and notifications
  (both target the "foreground client").
- Config path: `HERDR_CONFIG_PATH`, else `$XDG_CONFIG_HOME/herdr`, else
  `%APPDATA%\herdr` / `%USERPROFILE%\AppData\Roaming\herdr` on Windows, else
  `~/.config/herdr` (macOS included). Source: `herdr v0.9.3 src/config/io.rs`.
- Built-in `theme.name = "terminal"` follows host terminal colors;
  `auto_switch` layers `[theme.custom.light]` / `[theme.custom.dark]` on top.

## Behaviour matrix

| Case | Bash 0.8.1 | HEAD (`94da441`) | Working tree (0.10.0) |
|---|---|---|---|
| Local `herdr`, `prefix+t` | works | works | broken: popup shows instructions only |
| SSH into host, then `herdr` | works (OSC to first herdr TTY) | works | broken: blocked by `HERDR_ENV` |
| `herdr --remote host`, popup on server | writes server config, not the client's | same | refused; no working path from inside Herdr |
| Client with its own terminal palette | overwritten on apply | overwritten on every reconnect | untouched |
| Client with no theme / no selection | nothing written | nothing written | nothing written |
| Other servers' clients / `--remote` processes on the same box | first TTY found | all recolored | untouched |
| Multiple named servers | n/a | one shared daemon lock; daemon dies with the wrong server | moot (daemon removed) |
| Windows | unsupported | supported in design; not natively verified | same |

## Findings

### Working-tree changes

1. **Blocker – picker disabled inside Herdr.** `RequireLocalInvocation()`
   rejects any process with `HERDR_ENV`, `HERDR_PLUGIN_ID` or
   `HERDR_PLUGIN_CONTEXT_JSON`. Herdr sets `HERDR_ENV` in every pane, so
   `apply`/`picker` fail even from a plain shell pane. Reproduced:
   `Error: theme changes must run on the client machine outside a Herdr pane…`.
   Tests hide this because `TestMain` unsets those variables.
2. **Setup popup points at a binary that is not on PATH.** README states plugin
   install does not add it to PATH.
3. **Features removed instead of scoped:** OSC terminal sync (PR #2), the opt-in
   Ghostty fragment (`~/.config/ghostty/herdr-theme` only written if the user
   created it), and automatic reload after apply. `sync --client <pid>` needs a
   manual PID lookup and is lost on every reconnect.
4. Old global `applied` marker is no longer read; upgraders lose the current
   theme marker.
5. `min_herdr_version` raised 0.8.0 → 0.9.3 without an identified dependency.
6. Dead code: `RunHelper` ancestor branch in `sync_windows.go` is unreachable.

Worth keeping from the working tree:

- `herdr session attach <name>` recognised as an interactive client.
- Config precedence aligned with Herdr (verified against upstream).
- Missing config is created instead of failing.
- Bash download cache (`${XDG_CACHE_HOME:-~/.cache}/herdr-theme-picker/<slug>`)
  reused offline.
- Legacy Unix theme names stay usable; bad index entries are skipped and
  preserved; one broken user theme no longer disables the picker.
- Terminal sync refuses a selection that no longer matches the config.

### Present in both HEAD and working tree

7. **`[theme.custom.light]` / `[theme.custom.dark]` are deleted on apply.**
   `replaceCustomTokens` drops every table under `theme.custom`, including the
   `auto_switch` layers, and leaves blank lines behind. Reproduced with a config
   containing both layers: both disappear (only the daily backup keeps them).
   With `auto_switch = true` those layers would otherwise override the picked
   tokens, so this needs an explicit policy, not silent deletion.
8. OSC writes go straight to the client TTY while the client is also writing
   frames; a write landing mid-escape-sequence could garble a frame. Low
   probability, worth a native check.
9. Herdr treats an empty `XDG_CONFIG_HOME` as set; the plugin treats it as unset.
   Cosmetic.

### Original HEAD findings (from the earlier review), status

| Finding | Status in working tree |
|---|---|
| Daemon recolors unrelated clients (incl. `--remote`) | fixed by removing the daemon |
| Server-side picker cannot reach a `--remote` client | "fixed" by disabling the picker everywhere |
| `session attach` excluded | fixed |
| Multiple named servers lose the daemon | moot |
| Bash cache not migrated; legacy names break index | fixed |
| Config path differs from Herdr | fixed |
| Reload failure leaves conflicting markers | moot (no reload) |

## Validation

- `go vet ./...`, `go test ./...` pass on macOS.
- Builds pass for linux/amd64, linux/arm64, windows/amd64, darwin/amd64,
  darwin/arm64.
- Not verified: native Windows/Linux terminal behaviour, real SSH and
  `--remote` round trips, and whether `herdr server reload-config` alone
  refreshes an attached local client's theme (not run, to avoid reloading the
  live session).

## Direction: client-only theming

Goal: theme changes never involve the server. A client with the plugin gets the
picked colors; a client without it gets Herdr's default (or its own config).

Herdr already stores UI themes this way: the client reads its own
`[theme.custom]`. The open problems are where the picker process runs, how the
client reloads, and how the terminal palette is handled.

Answered (2026-10-04, Windows laptop attached to the Mac with `herdr --remote`):
`prefix+t` on the laptop showed the "Themes belong to your client machine"
popup. That text exists only in the Mac's uncommitted working-tree build, so
**plugin popups invoked by a `--remote` client run on the server**. On the
server, a remote client appears only as
`herdr remote-client-bridge --idle-timeout-v1` under `sshd-session …@notty`
(no TTY). A local client appears as `herdr` with a TTY. The old daemon
therefore never targeted remote clients, and a server-side popup cannot change
a remote client's theme.

Open questions to verify before implementing:

1. (Answered above.)
2. Can a local process trigger a client-side config reload, or must the user
   press `prefix+shift+r`?
3. Does `herdr server reload-config` refresh a local client's theme?
4. Policy for `auto_switch` light/dark layers (finding 7).
5. Terminal ANSI palette: drop it, keep it opt-in via OSC to local client
   processes, or persist through emulator config (Ghostty fragment).

## Decision and implementation (2026-10-04)

Follow the Bash behaviour, tightened. Implemented on `go-rewrite` (uncommitted):

- The picker works inside Herdr again (`RequireLocalInvocation` removed). Apply
  writes this machine's config, records `<state>/applied` (Bash location),
  recolors this machine's interactive Herdr clients, refreshes an existing
  Ghostty fragment, and reloads. Sync and reload failures are warnings; a failed
  reload points to `prefix+shift+r`.
- `prefix+t` shows the "Themes belong to your client machine" screen only when
  every attached client is remote (`remote-client-bridge` present, no local
  interactive client). With both attached it opens the picker.
- No daemon, startup hooks, or `Tracker`. `sync [--client <pid>]` re-sends colors
  on demand.
- Recolor targets: local, `--session`, `session attach`, and outbound
  `--remote` clients on this machine. Never bridges, servers, CLI commands, or
  other users' TTYs.
- Finding 7 fixed: auto-switch layers kept in every TOML spelling. Repeated
  applies are byte-stable.
- Tests run under an isolated temporary home/config/state with a missing
  `herdr` binary and an invalid client PID.
