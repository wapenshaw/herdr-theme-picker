# herdr-theme-picker

Pick any of 112 bundled [terminalcolors.com](https://terminalcolors.com) palettes
from an fzf popup with a live workspace preview. The colors are mapped into
`[theme.custom]` in your Herdr `config.toml`, Herdr reloads, and the terminal
windows running Herdr on the same machine are recolored to match.

<p align="center">
  <img alt="Herdr theme preview" src="https://github.com/user-attachments/assets/5100a157-1ed1-4659-be98-b846fb98681d" width="70%"/>
</p>

## Where themes apply

Herdr reads its UI theme from the **client machine's** config. That includes a
laptop viewing a server through `herdr --remote`. The server never decides a
client's colors. This plugin follows the same rule: it writes the config of the
machine it runs on and recolors only Herdr terminals on that machine.

| Setup | Press `prefix+t` | Result |
| --- | --- | --- |
| Local `herdr` | Picker opens | Theme applied, Herdr reloads, its terminal recolors. |
| SSH into a host, then run `herdr` there | Picker opens on the host | The host's config is the client config; the SSH terminal recolors if it supports OSC colors. |
| `herdr --remote host` from a laptop, nobody attached locally on the host | Setup instructions | Run the picker on the laptop instead (see below). |
| `herdr --remote` client and a local client attached at the same time | Picker opens | Herdr cannot say who pressed the key. Picking changes the **host's** theme only. |
| A client without this plugin, or with no theme picked | — | Nothing is written. It keeps Herdr's default theme or its own config. |

The plugin never runs in the background and never recolors terminals on other
machines.

## Install

### From GitHub (recommended)

```bash
herdr plugin install qintmb/herdr-theme-picker
```

Requires [fzf](https://github.com/junegunn/fzf); Herdr builds the plugin with Go
1.24+. Supported on Windows, Linux, and macOS.

Herdr 0.8.2 does not bind keys from plugin manifests. If `prefix+t` does nothing,
add this to `config.toml` and reload:

```toml
[[keys.command]]
key = "prefix+t"
type = "plugin_action"
command = "herdr-theme-picker.open"
```

```bash
herdr server reload-config
```

Reinstall with the same command to update. Herdr detects a new release by the
manifest `version`.

### Local checkout (development)

```bash
git clone https://github.com/wapenshaw/herdr-theme-picker.git
cd herdr-theme-picker
herdr plugin link "$PWD"
herdr server reload-config
```

A linked checkout is not tracked for updates.

## Using `herdr --remote` from another machine

A `--remote` client takes its theme from its own machine, so pick the theme
there:

```sh
git clone https://github.com/wapenshaw/herdr-theme-picker.git
cd herdr-theme-picker
go build -o herdr-theme-picker ./cmd/herdr-theme-picker
./herdr-theme-picker picker
```

On Windows, build with `-o herdr-theme-picker.exe` and run
`.\herdr-theme-picker.exe picker` from PowerShell or Windows Terminal.

Run it in a terminal **outside** Herdr. A Herdr pane runs on the server, so a
picker started there changes the server's config. Keep the binary beside the
`themes/` directory, or set `HERDR_PLUGIN_ROOT` to the checkout.

The picker writes the laptop's config (creating it if needed) and recolors the
laptop's `herdr --remote` terminal windows. A laptop has no local Herdr server
to reload, so the picker tells you to press Herdr's reload key in the client:
**`prefix+shift+r`**. The server is not involved.

## Picker and custom themes

- Type to search. The preview maps the palette onto a mock Herdr workspace.
- **Enter** applies the theme and closes the popup. **Esc** cancels.
- **Tab** or **+ Add new theme…** opens an editor for a Ghostty-format palette.
- **+ Add from clipboard…** imports a palette from the clipboard and applies it.
- **Ctrl-E** edits a user theme's token overrides. **Save & apply** commits;
  **Esc** discards the scratch edits.
- **Ctrl-D** deletes a user theme after confirmation. Bundled themes are read-only.

The applied theme is marked `✓`; user themes are marked `★`. `VISUAL` and
`EDITOR` are respected, including arguments such as `code --wait`. Fallback
editors are `nvim`, `nano`, then `vi` on Unix or Notepad on Windows.

Palettes need `background`, `foreground`, and `palette` entries 0–15 as six-digit
hex colors. New names become lowercase hyphenated slugs. User themes override
bundled themes with the same name. They live in `<state>/themes/`, indexed by
`<state>/index.txt`, and survive plugin updates.

The preview legend shows how colors become Herdr UI tokens. For example,
`accent` uses palette 4, `red` uses palette 1, and `panel_bg` uses the background.
Per-token edits are stored as `# hpick-override: token=#rrggbb` comments.

Deleting a palette does not change the applied config. Pick another theme, or
remove `[theme.custom]` and reload to return to your built-in theme.

## Terminal colors

The Herdr UI (sidebar, tabs, borders) uses `[theme.custom]`. Text inside panes
uses your terminal emulator's 16 ANSI colors. Applying a theme therefore also
sends OSC 4/10/11 color setters to the terminal of every interactive Herdr
client on the **same machine**, then queries the colors back so Herdr refreshes
its pane colors.

- Targets: local `herdr`, `herdr --session …`, `herdr session attach …`, and
  `herdr --remote …` clients started on this machine. Each reads this machine's
  config, so they share the theme.
- Never targeted: the Herdr server, CLI commands such as `herdr status`, the
  `remote-client-bridge` process serving another machine's client, and
  terminals owned by other user accounts.
- Changes last as long as the terminal window. A new window starts with its
  emulator's own colors. Run `herdr-theme-picker sync` or the **Theme picker:
  sync terminal colors** action to re-send them.
- **Ghostty:** to keep the colors in new windows, create
  `~/.config/ghostty/herdr-theme` (on Windows,
  `%APPDATA%\ghostty\herdr-theme`) and add `config-file = herdr-theme` to your
  Ghostty config. The picker refreshes that file on every apply. It never
  creates the file.

Support varies by emulator. Ghostty, iTerm2, WezTerm, kitty, Alacritty, and
Windows Terminal implement OSC color setters; nested multiplexers can swallow
them.

## Commands

```sh
herdr-theme-picker picker                 # interactive picker
herdr-theme-picker apply dracula-default  # apply by name (fetches unbundled themes)
herdr-theme-picker sync                   # re-send the applied colors to local clients
herdr-theme-picker sync --client 12345    # ... to one client PID only
herdr-theme-picker preview dracula-default
herdr-theme-picker add [--clipboard]
herdr-theme-picker edit my-theme
herdr-theme-picker delete my-theme
```

Bundled palettes work offline. Downloads are validated before they are cached,
and Bash-era downloads in `${XDG_CACHE_HOME:-~/.cache}/herdr-theme-picker` are
reused offline.

## Configuration and state

Config lookup matches Herdr: `HERDR_CONFIG_PATH`, then
`$XDG_CONFIG_HOME/herdr/config.toml`, then `%APPDATA%\herdr\config.toml` on
Windows or `~/.config/herdr/config.toml` elsewhere. A missing config is created.

The picker owns only the base `[theme.custom]` tokens. Everything else is
preserved: unrelated TOML, comments, permissions, symlinks, and
`[theme.custom.light]` / `[theme.custom.dark]` overrides used with
`auto_switch`. With `auto_switch = true` those layers still win over the picked
tokens. The first change each day backs up the original to
`config.toml.bak-YYYYMMDD`. Writes are atomic.

`HERDR_PLUGIN_STATE_DIR` sets the state directory. A standalone run reuses the
installed plugin's state when it exists, so both share user themes and the
`applied` marker.

## Upgrading from 0.9

Version 0.9 started a background daemon at server startup that recolored every
Herdr terminal on the machine, including clients with their own colors. 0.10
removes the daemon and startup hooks; colors change only when you apply or
sync. A daemon that is already running keeps going until the Herdr server
restarts.

Other fixes: `[theme.custom.light]` / `[theme.custom.dark]` are no longer
deleted, repeated applies no longer add blank lines, `herdr session attach`
clients are recognized, config lookup matches Herdr, and old Unix theme names
from the Bash version keep working.

See [theme persistence edge cases](docs/theme-persistence-edge-cases.md) for the
full matrix.

## Development

```sh
go test ./...
go vet ./...
go fmt ./...
go build -o herdr-theme-picker ./cmd/herdr-theme-picker
```

- `cmd/herdr-theme-picker`: CLI routing.
- `internal/theme`: palettes, picker/editor, config writes, apply and sync.
- `internal/terminal`: client discovery and OSC color writes.
- `internal/instance`: cross-platform file locks.
- `tests/`: public API and regression tests; `tests/testutil` isolates every
  run in a temporary home/config/state and fakes `fzf` and `herdr`.

Tests must not recolor live terminals, reload real Herdr sessions, or touch the
real clipboard. Cross-build platform-sensitive changes for Windows, Linux, and
macOS; emulator and SSH behavior needs native checks.

## License

MIT — see [LICENSE](LICENSE).
