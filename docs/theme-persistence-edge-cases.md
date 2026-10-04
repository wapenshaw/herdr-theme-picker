# Theme persistence and terminal sync edge cases

Recorded during the 2026-10-04 review of `03f2325` through `4ee8cbd` and the
subsequent fixes in this checkout. This note covers the main terminal pane,
client discovery, daemon lifecycle, and differences from the Bash implementation.

## Main terminal pane and host color readback

Herdr's `[theme.custom]` settings control its UI colors. The main terminal pane
also depends on the host terminal's foreground, background, and ANSI palette.
A sidebar or tab bar retaining its theme does not prove the terminal pane has
retained its colors.

The plugin sets the host colors with OSC 10, OSC 11, and OSC 4, then queries the
foreground, background, and ANSI slots 0–15. Herdr consumes the replies to
refresh its terminal-pane colors. Setting colors without readback can leave
Herdr using its previous host-color cache.

Color queries generate input from the emulator. They must reach an interactive
Herdr client that can consume the replies. Sending them to a terminal running
a CLI command can leave text such as `10;rgb:...`, `11;rgb:...`, and `4;0;rgb:...`
at the shell prompt. A shell may then interpret the fragments as commands.
This failure is independent of the shell or terminal emulator brand.

## Client selection and unrelated windows

| Case | Required behavior / current handling |
| --- | --- |
| `herdr server stop` in another terminal window | Exclude the CLI process. It must receive neither color settings nor color queries. |
| `server reload-config`, `status`, plugin actions, pane commands, update, help, or version output | Exclude these noninteractive invocations even when they have a console or TTY. |
| A headless server with the same executable name | Exclude it from client discovery; a process name alone cannot identify an interactive client. |
| Ordinary `herdr` or hidden `herdr client` launch | Recognize the interactive launch and restore the selected colors. |
| Named sessions, including names containing spaces or the word `server` | Preserve argument boundaries; an option value is not a subcommand. |
| Executable paths containing spaces | Read real process arguments rather than splitting a formatted command line on whitespace. |
| Unknown launch flags, unreadable arguments, or an exited process | Skip the candidate instead of guessing its role from the executable name. Future Herdr launch options may require updating the shared classifier. |
| No interactive client in the picker's ancestry | Immediate host sync reports the host as unavailable. It must not select an unrelated process by name. UI config and optional Ghostty persistence can still update. |
| Explicit `HERDR_THEME_CLIENT_PID` | Require a positive PID belonging to an eligible interactive Herdr client. Invalid, missing, headless, and CLI targets are rejected. |
| Exact target exits before the helper writes | Revalidate the target and report unavailable; never fall back to another Herdr process or an inherited PID override. |

Immediate sync uses an explicit target or an interactive ancestor. Background
sync has a broader, deliberate scope: without `HERDR_THEME_CLIENT_PID`, it
discovers eligible local Herdr clients and applies the globally selected theme.
It excludes the owning server but does not group clients by session or associate
every discovered client with that server. Multiple local clients can therefore
all receive the selected theme. Use the explicit PID to limit that scope.

Discovery is local to the machine running the plugin. A server-side daemon
cannot discover the terminal of a client running on another machine.

## Reconnects, palette edits, and the initial flash

- A detach followed by reattach creates a new client process. Tracking uses
  PID plus TTY, so a new PID on the same TTY receives a fresh sync even when
  the daemon never observes an intervening poll with no clients.
- Successful writes are remembered per client and payload. Failed or
  unavailable writes are retried; they must not mark the client as synced or
  prevent another client from receiving its colors.
- Palettes are reloaded even when the selected slug stays the same. Editing a
  custom theme or replacing its palette changes the payload and triggers
  another sync for connected clients.
- A missing or empty `applied` marker causes the daemon to wait without emitting
  colors. An unreadable marker or invalid/unavailable palette is recorded in
  the daemon log and prevents that sync attempt.
- The first daemon scan runs immediately. Subsequent scans are scheduled every
  100 ms on Windows and 500 ms on macOS/Linux. These intervals are not a total
  latency guarantee: discovery, palette loading, helper startup, and readback
  also take time.
- A client's first frame can show the original host colors before discovery
  and readback finish. The faster Windows scan reduces this flash; the plugin
  does not guarantee restoration before the first frame.

## Persisted state and daemon lifecycle

| Case | Required behavior / current handling |
| --- | --- |
| Picker and standalone commands use different state directories | Prefer the existing Herdr-managed plugin state directory so standalone sync restores the same `applied` selection. An explicit `HERDR_PLUGIN_STATE_DIR` takes precedence. |
| Plugin has not been installed or linked | Keep the legacy standalone state location as a fallback. |
| Stale `sync.pid` refers to a reused, live PID | Treat the PID marker as informational. Only the OS-held lock proves that a daemon is running. |
| Concurrent startup hooks or repeated start actions | Acquire an exclusive `sync.lock`; an existing owner makes another startup a no-op. Keep the lock file in place to avoid locking different files at the same path. |
| Fast server stop/start while the previous daemon still holds its lock | Use `sync.server` to identify a dead previous owner and wait for its daemon to release the lock, rather than skipping the new server's startup. |
| Owning server exits | A daemon bound to that server exits after observing its shutdown. Do not bind lifetime monitoring to an arbitrary parent shell. |
| Windows `herdr.sock` | Read the `PID:timestamp` metadata to identify the server. |
| macOS/Linux `herdr.sock` | Treat it as a socket, not a file containing a PID; startup hooks identify their Herdr ancestor. |
| Plugin is linked or updated while the server is already running | Startup hooks do not rerun automatically. Ensure a daemon is started; replacing the executable also requires restarting an already-running daemon to use the new code. |

Herdr-managed state is normally under
`%LOCALAPPDATA%/herdr/plugins/herdr-theme-picker` on Windows and
`~/.local/state/herdr/plugins/herdr-theme-picker` on macOS/Linux.
`XDG_STATE_HOME` is honored. Legacy standalone state is normally under
`%LOCALAPPDATA%/herdr-theme-picker` or
`${XDG_CACHE_HOME:-~/.cache}/herdr-theme-picker`.
Different explicit state directories create separate selections and locks.

The action below ensures a daemon exists; despite its historical action ID,
it does not replace an already-running daemon:

```sh
herdr plugin action invoke restart --plugin herdr-theme-picker
```

`herdr-theme-picker sync` and `startup --once` perform a single restore and do
not provide ongoing reconnect detection. Logs are in the resolved state
directory's `sync.log`; `sync.pid` and `sync.server` are diagnostic markers.

## Platform and emulator boundaries

The OSC payload and interactive-launch classifier are shared across platforms.
PowerShell and Windows Terminal are not dependencies of the plugin's sync path.

| Platform | Process arguments | Host write path |
| --- | --- | --- |
| Windows | Native process command-line query and Windows argument parsing | An isolated helper attaches to the selected client's console and writes to `CONOUT$`. |
| macOS | `kern.procargs2`, with argument count, executable path, padding, and NUL-separated argv decoded | Revalidate the client's PID and TTY, then write to its outer `/dev/...` terminal device. |
| Linux | NUL-separated `/proc/<pid>/cmdline` arguments | Revalidate the client's PID and TTY, then write to its outer `/dev/...` terminal device. |

On Windows, attachment and detachment must stay inside the short-lived helper.
Changing the picker's or daemon's console ownership can disturb input, reload
commands, and console signal handling. `CONOUT$` is opened with read/write access
so console mode queries and VT output setup work; its output mode is restored
after writing.

On Unix, writing to the picker's own `/dev/tty` can be intercepted by Herdr's
internal pane. Host sync therefore targets the interactive client's outer TTY,
checks that it is a character device, and uses a nonblocking write.

The emulator must support the relevant OSC color setters and queries for
readback to work. Support should be verified in the actual emulator; using
Windows console APIs or a Unix TTY does not itself guarantee OSC support.
Live OSC changes address the current terminal session. They do not rewrite
every emulator's defaults for future windows.

## What the original Bash implementation did

The last Bash implementation before the Go rewrite can be inspected with:

```sh
git show 0595122:bin/apply.sh
git show 0595122:herdr-plugin.toml
```

Its `sync_terminal_colors` function selected the first matching `herdr` TTY
using `ps -eo tty,comm`, then wrote only OSC color-setting sequences to that
device when a theme was applied. It did not inspect launch arguments, send
color queries, or run a reconnect daemon. Its first-match selection was also
ambiguous when multiple Herdr processes existed.

If `~/.config/ghostty/herdr-theme` already existed, Bash copied the palette
there. With the documented Ghostty include, that persisted colors for future
Ghostty windows. The Go implementation retains this opt-in behavior. It is
separate from the emulator-independent live OSC path.

The new daemon exposed the broad process-name selection, and the added
readback queries made its consequences visible as shell input. Restoring the
Bash selection rule would retain that ambiguity and lose reconnect handling.

## Regression coverage and manual verification

| Coverage | Location |
| --- | --- |
| Client ancestry, explicit PID rejection, CLI versus interactive launch arguments | [Client selection tests](../tests/terminal/client_test.go) |
| Console-owning fake `herdr.exe server stop`, reload, plugin action, status, and help processes rejected by discovery and the exact helper; normal clients still writable | [Windows helper integration tests](../tests/terminal/helper_windows_test.go) |
| macOS argv boundaries, spaces, padding, environment exclusion, and malformed buffers | [macOS argument decoder tests](../internal/terminal/process_args_test.go) |
| Same-TTY reconnect, palette changes, disconnects, failed-write retries | [Sync tracker tests](../tests/terminal/tracker_test.go) |
| Color setting followed by foreground/background and all 16 ANSI queries | [OSC payload tests](../tests/terminal/payload_test.go) |
| Daemon locking, stale markers, palette edits, server restart handoff | [Daemon tests](../tests/theme/daemon_test.go), [instance lock tests](../tests/instance/lock_test.go) |
| Managed versus legacy state and explicit state overrides | [State path tests](../tests/theme/paths_test.go) |

Review validation passed `go test ./...` and `go vet ./...` on Windows, including
isolated console integration tests. Linux, macOS Intel, and macOS Apple Silicon
builds passed. The macOS argument decoder fixtures run on Windows too; those
fixtures and cross-builds do not constitute a native macOS runtime test.

The user observed successful Windows detach/reattach restoration with a brief
initial flash, then reported the separate-window `server stop` regression.
The CLI-selection fix was subsequently tested with isolated subprocesses and
installed; its live log confirmed a restore to the existing interactive client.
The real separate-window stop workflow still needs a follow-up manual check.

When validating a release on each supported OS and emulator, check:

1. Apply a theme and verify the main terminal pane as well as the sidebar/tabs.
2. Detach and reattach in the same terminal, then attach from another terminal.
   Confirm the selected theme restores and record any initial flash.
3. Run `herdr server stop` from a separate terminal. That terminal's colors
   and input must stay unchanged. Start Herdr again and verify restoration.
4. Run other Herdr CLI commands in a separate terminal while the daemon is
   active; confirm no recoloring or OSC replies appear at its prompt.
5. Exercise named sessions and executable paths containing spaces, multiple
   local clients, explicit PID targeting, and a fast server restart.
6. Edit the active palette without changing its slug and verify another sync.
   Confirm picker and standalone commands read the same state directory.

Keep native OS/emulator results separate from cross-build and fixture results.
