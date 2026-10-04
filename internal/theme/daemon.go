package theme

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"herdr-theme-picker/internal/instance"
	"herdr-theme-picker/internal/terminal"
)

func daemonLog(format string, args ...any) {
	logPath := filepath.Join(StateDir(), "sync.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	timestamp := time.Now().Format("2006-01-02T15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s %s\n", timestamp, msg)
}

// DaemonPIDFile returns the path to the informational daemon PID marker.
func DaemonPIDFile() string {
	return filepath.Join(StateDir(), "sync.pid")
}

// IsDaemonRunning checks the OS-held lock, not a PID that could have been reused.
func IsDaemonRunning() bool {
	path := filepath.Join(StateDir(), "sync.lock")
	if _, err := os.Stat(path); err != nil {
		return false
	}
	lock, err := instance.Acquire(path)
	if err == nil {
		lock.Close()
	}
	return errors.Is(err, instance.ErrLocked)
}

// A new server can start its hooks before the old daemon observes shutdown.
// Wait for that daemon's lock to be released instead of treating its imminent
// exit as evidence that the new server already has a persistent sync loop.
func acquireDaemonLock(ctx context.Context, serverPID int) (*os.File, error) {
	path := filepath.Join(StateDir(), "sync.lock")
	ownerPath := filepath.Join(StateDir(), "sync.server")
	for {
		lock, err := instance.Acquire(path)
		if !errors.Is(err, instance.ErrLocked) {
			return lock, err
		}
		data, readErr := os.ReadFile(ownerPath)
		owner, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if readErr != nil || parseErr != nil || serverPID <= 1 || owner <= 1 || owner == serverPID || terminal.IsProcessAlive(owner) {
			return nil, instance.ErrLocked
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// RunDaemon runs the persistent terminal color sync daemon.
// It maintains terminal colors for the applied theme across client connects and restarts.
func RunDaemon(ctx context.Context) error {
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	monitoredPID := HerdrServerPID()
	lock, err := acquireDaemonLock(ctx, monitoredPID)
	if errors.Is(err, instance.ErrLocked) {
		return nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("lock sync daemon: %w", err)
	}
	defer lock.Close()
	ownerPath := filepath.Join(StateDir(), "sync.server")
	if err := os.WriteFile(ownerPath, []byte(strconv.Itoa(monitoredPID)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write daemon server marker: %w", err)
	}
	defer os.Remove(ownerPath)

	pidPath := DaemonPIDFile()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return fmt.Errorf("write daemon pid file: %w", err)
	}
	defer os.Remove(pidPath)

	daemonLog("started sync daemon pid=%d, monitoring server pid=%d", os.Getpid(), monitoredPID)

	// Windows process discovery uses a snapshot rather than spawning ps, so
	// poll more often there to shorten the default-color flash on reattach.
	interval := 500 * time.Millisecond
	if runtime.GOOS == "windows" {
		interval = 100 * time.Millisecond
	}
	// Sync on startup before waiting for the first discovery interval.
	timer := time.NewTimer(0)
	defer timer.Stop()

	var currentSlug string
	var currentPayload string
	var tracker terminal.Tracker
	var previousError string
	reportError := func(err error) {
		message := ""
		if err != nil {
			message = err.Error()
		}
		if message != "" && message != previousError {
			daemonLog("terminal sync: %s", message)
		}
		previousError = message
	}

	for {
		select {
		case <-ctx.Done():
			daemonLog("daemon received stop signal, exiting")
			return nil
		case <-timer.C:
			timer.Reset(interval)
			// If started by or bound to Herdr server, exit when the server exits.
			if monitoredPID > 1 && !terminal.IsProcessAlive(monitoredPID) {
				daemonLog("monitored server pid %d exited, daemon stopping", monitoredPID)
				return nil
			}

			// Read active applied theme slug
			appliedPath := AppliedFile()
			data, err := os.ReadFile(appliedPath)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					reportError(err)
				}
				continue
			}
			slug := strings.TrimSpace(string(data))
			if slug == "" {
				continue
			}
			// Load the palette even when the slug is unchanged: editing a custom
			// theme or replacing a cached palette must also reach future clients.
			palettePath, err := ResolvePalette(slug)
			if err != nil {
				reportError(err)
				continue
			}
			pal, err := ParsePaletteFile(palettePath)
			if err != nil {
				reportError(err)
				continue
			}
			payload := PaletteOSCPayload(pal)
			if slug != currentSlug || payload != currentPayload {
				daemonLog("active theme changed to %q", slug)
				currentSlug, currentPayload = slug, payload
			}
			clients, err := terminal.ActiveClients(monitoredPID, os.Getenv("HERDR_THEME_CLIENT_PID"))
			if err == nil {
				err = tracker.Sync(currentPayload, clients, func(client terminal.Client, payload string) error {
					err := terminal.EmitClient(client, payload)
					if err == nil {
						daemonLog("synced client pid=%d tty=%q", client.PID, client.TTY)
					}
					return err
				})
			}
			reportError(err)
		}
	}
}
