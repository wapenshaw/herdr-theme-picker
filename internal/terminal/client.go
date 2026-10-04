// Package terminal discovers Herdr host terminals and restores their colors.
package terminal

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrUnavailable = errors.New("owning host terminal unavailable")

const maxPayloadBytes = 1 << 20

// Client identifies a process and its terminal. PID keeps a reattach on the
// same Unix TTY distinct from the previous client.
type Client struct {
	PID, Parent int
	Name, TTY   string
	Interactive bool
}

func (c Client) Key() string { return fmt.Sprintf("%d:%s", c.PID, c.TTY) }

func IsHerdr(name string) bool {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(name, "/")
	base := parts[len(parts)-1]
	return base == "herdr" || strings.EqualFold(base, "herdr.exe")
}

// IsInteractiveCommand recognizes client launches, including the executable
// in args[0]. CLI commands such as server stop, status, and plugin actions do
// not consume host color replies and must never receive queries. Unknown or
// unreadable arguments are rejected rather than guessed from the binary name.
func IsInteractiveCommand(args []string) bool {
	if len(args) == 0 || !IsHerdr(args[0]) {
		return false
	}
	args = args[1:]
	if len(args) > 0 && args[0] == "client" {
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		flag, value, inline := strings.Cut(args[i], "=")
		switch flag {
		case "--handoff":
			if inline {
				return false
			}
		case "--session", "--remote", "--remote-keybindings":
			if !inline {
				i++
				if i >= len(args) {
					return false
				}
				value = args[i]
			}
			if value == "" || strings.HasPrefix(value, "-") {
				return false
			}
			if flag == "--remote-keybindings" && value != "local" && value != "server" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (c Client) isInteractive() bool {
	return IsHerdr(c.Name) && c.TTY != "" && c.Interactive
}

// SelectClient chooses an explicit client or an interactive ancestor.
// It never falls back to an unrelated process by name.
func SelectClient(processes map[int]Client, parent int, explicit string) (Client, error) {
	if explicit != "" {
		pid, err := strconv.Atoi(explicit)
		if err != nil || pid <= 0 {
			return Client{}, fmt.Errorf("invalid HERDR_THEME_CLIENT_PID %q", explicit)
		}
		p, ok := processes[pid]
		if !ok || !p.isInteractive() {
			return Client{}, fmt.Errorf("PID %d is not a Herdr client", pid)
		}
		return p, nil
	}
	seen := make(map[int]bool)
	for parent > 0 && !seen[parent] {
		seen[parent] = true
		p, ok := processes[parent]
		if !ok {
			break
		}
		if p.isInteractive() {
			return p, nil
		}
		parent = p.Parent
	}
	return Client{}, ErrUnavailable
}

// ActiveClients returns interactive local Herdr clients, excluding CLI commands
// and the owning server even when they share the executable and have a TTY.
// An explicit target limits both immediate and background synchronization.
func ActiveClients(serverPID int, explicit string) ([]Client, error) {
	processes, err := Processes()
	if err != nil {
		return nil, err
	}
	if explicit != "" {
		client, err := SelectClient(processes, 0, explicit)
		if err != nil {
			return nil, err
		}
		if client.PID == serverPID {
			return nil, ErrUnavailable
		}
		return []Client{client}, nil
	}
	var clients []Client
	for _, p := range processes {
		if p.PID != serverPID && p.isInteractive() {
			clients = append(clients, p)
		}
	}
	return clients, nil
}

// Tracker remembers successful writes per client process and payload. Failed
// writes are retried, and a palette edit invalidates every previous success.
type Tracker struct {
	payload string
	synced  map[string]bool
}

func (t *Tracker) Sync(payload string, clients []Client, emit func(Client, string) error) error {
	if payload != t.payload || t.synced == nil {
		t.payload = payload
		t.synced = make(map[string]bool)
	}
	active := make(map[string]bool)
	var errs []error
	for _, c := range clients {
		key := c.Key()
		active[key] = true
		if payload == "" || t.synced[key] {
			continue
		}
		if err := emit(c, payload); err != nil {
			if !errors.Is(err, ErrUnavailable) {
				errs = append(errs, fmt.Errorf("client %d: %w", c.PID, err))
			}
		} else {
			t.synced[key] = true
		}
	}
	for key := range t.synced {
		if !active[key] {
			delete(t.synced, key)
		}
	}
	return errors.Join(errs...)
}
