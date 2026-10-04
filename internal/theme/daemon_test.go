package theme

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPaletteOSCPayload(t *testing.T) {
	if payload := PaletteOSCPayload(nil); payload != "" {
		t.Fatalf("expected empty payload for nil palette, got %q", payload)
	}

	pal := &Palette{
		Background:    "#282a36",
		Foreground:    "#f8f8f2",
		PaletteColors: make(map[int]string),
	}
	pal.PaletteColors[0] = "#21222c"
	pal.PaletteColors[15] = "#ffffff"

	payload := PaletteOSCPayload(pal)
	if !strings.Contains(payload, "\033]10;#f8f8f2\007") {
		t.Errorf("missing foreground in payload: %q", payload)
	}
	if !strings.Contains(payload, "\033]11;#282a36\007") {
		t.Errorf("missing background in payload: %q", payload)
	}
	if !strings.Contains(payload, "\033]4;0;#21222c\007") {
		t.Errorf("missing color 0 in payload: %q", payload)
	}
	if !strings.Contains(payload, "\033]4;15;#ffffff\007") {
		t.Errorf("missing color 15 in payload: %q", payload)
	}
}

func TestSyncAppliedThemeMissing(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	if err := SyncAppliedTheme(); err != nil {
		t.Fatalf("expected nil when applied file does not exist, got: %v", err)
	}

	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AppliedFile(), []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SyncAppliedTheme(); err != nil {
		t.Fatalf("expected nil when applied file is empty/whitespace, got: %v", err)
	}
}

func TestDaemonLifecycleAndPIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	if IsDaemonRunning() {
		t.Fatal("expected IsDaemonRunning to be false before daemon starts")
	}

	// Create a dummy dead PID file
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// PID 999999 is extraordinarily unlikely to exist
	if err := os.WriteFile(DaemonPIDFile(), []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsDaemonRunning() {
		t.Fatal("expected IsDaemonRunning to be false for dead PID")
	}

	// RunDaemon with a short-lived context
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- RunDaemon(ctx)
	}()

	// Wait briefly for daemon to write PID file
	time.Sleep(50 * time.Millisecond)
	data, err := os.ReadFile(DaemonPIDFile())
	if err == nil {
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if pid != os.Getpid() {
			t.Errorf("expected PID %d in pid file, got %d", os.Getpid(), pid)
		}
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunDaemon returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunDaemon did not exit within timeout")
	}

	// After exit, pid file must be removed
	if _, err := os.Stat(DaemonPIDFile()); !os.IsNotExist(err) {
		t.Fatalf("expected PID file to be removed after daemon exit, but it still exists")
	}
}

func TestIsProcessAliveReal(t *testing.T) {
	t.Logf("self alive %d: %v", os.Getpid(), isProcessAlive(os.Getpid()))
	t.Logf("ppid alive %d: %v", os.Getppid(), isProcessAlive(os.Getppid()))
	serverPID := findHerdrServerPID()
	t.Logf("herdr server PID %d alive: %v", serverPID, isProcessAlive(serverPID))
}
