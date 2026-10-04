package theme

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

// DaemonPIDFile returns the path to the daemon PID lockfile.
func DaemonPIDFile() string {
	return filepath.Join(StateDir(), "sync.pid")
}

// IsDaemonRunning checks whether another sync daemon instance is currently alive.
func IsDaemonRunning() bool {
	pidPath := DaemonPIDFile()
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	return isProcessAlive(pid)
}

// RunDaemon runs the persistent terminal color sync daemon.
// It maintains terminal colors for the applied theme across client connects and restarts.
func RunDaemon(ctx context.Context) error {
	if IsDaemonRunning() {
		return nil
	}

	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	pidPath := DaemonPIDFile()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return fmt.Errorf("write daemon pid file: %w", err)
	}
	defer os.Remove(pidPath)

	monitoredPID := HerdrServerPID()
	daemonLog("started sync daemon pid=%d, monitoring server pid=%d", os.Getpid(), monitoredPID)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var currentSlug string
	var currentPayload string
	syncedTargets := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			daemonLog("daemon received stop signal, exiting")
			return nil
		case <-ticker.C:
			// If started by or bound to Herdr server, exit when the server exits.
			if monitoredPID > 1 && !isProcessAlive(monitoredPID) {
				daemonLog("monitored server pid %d exited, daemon stopping", monitoredPID)
				return nil
			}

			// Read active applied theme slug
			appliedPath := AppliedFile()
			data, err := os.ReadFile(appliedPath)
			if err != nil {
				continue
			}
			slug := strings.TrimSpace(string(data))
			if slug == "" {
				continue
			}

			if slug != currentSlug {
				palettePath, err := ResolvePalette(slug)
				if err != nil {
					continue
				}
				pal, err := ParsePaletteFile(palettePath)
				if err != nil {
					continue
				}
				currentSlug = slug
				currentPayload = PaletteOSCPayload(pal)
				syncedTargets = make(map[string]bool)
				daemonLog("active theme changed to %q", slug)
			}

			if currentPayload != "" {
				syncActiveClients(currentPayload, syncedTargets)
			}
		}
	}
}
