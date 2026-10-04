package theme_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"herdr-theme-picker/internal/instance"
	"herdr-theme-picker/internal/terminal"
	. "herdr-theme-picker/internal/theme"
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
	t.Setenv("HERDR_PLUGIN_STATE_DIR", tmpDir)
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
	t.Setenv("HERDR_PLUGIN_STATE_DIR", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	if IsDaemonRunning() {
		t.Fatal("expected IsDaemonRunning to be false before daemon starts")
	}

	// A stale marker can refer to a reused, live PID. It must not prevent startup.
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DaemonPIDFile(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsDaemonRunning() {
		t.Fatal("a live PID without a held lock is not a daemon")
	}

	// RunDaemon with a short-lived context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- RunDaemon(ctx)
	}()

	// Observe the lock instead of relying on a fixed sleep or swallowing a
	// missing PID file. No applied marker exists, so no real terminal is touched.
	deadline := time.Now().Add(2 * time.Second)
	for !IsDaemonRunning() {
		if time.Now().After(deadline) {
			t.Fatal("daemon never acquired its lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A concurrent startup is a no-op and must not remove the first PID marker.
	if err := RunDaemon(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(DaemonPIDFile())
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if pid != os.Getpid() {
		t.Errorf("expected PID %d in pid file, got %d", os.Getpid(), pid)
	}
	cancel()

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
	if IsDaemonRunning() {
		t.Fatal("daemon retained its lock after cancellation")
	}
}

func TestIsProcessAliveReal(t *testing.T) {
	if !terminal.IsProcessAlive(os.Getpid()) {
		t.Fatal("current process was reported dead")
	}
	if terminal.IsProcessAlive(0) || terminal.IsProcessAlive(-1) {
		t.Fatal("invalid PID was reported alive")
	}
}

func TestHerdrSocketPID(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PID metadata; Unix uses a socket")
	}
	tmpDir := t.TempDir()
	sockPath := tmpDir + "/test.sock"
	if err := os.WriteFile(sockPath, []byte("12345:9876543210\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_SOCKET_PATH", sockPath)
	if pid := HerdrServerPID(); pid != 12345 {
		t.Errorf("expected server PID 12345, got %d", pid)
	}
}

func TestDaemonNoticesPaletteEditWithoutSlugChange(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_THEME_CLIENT_PID", "invalid")
	body := fixturePalette(t)
	if err := saveFixtureTheme(t, "editable", body); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, AppliedFile(), "editable\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunDaemon(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("daemon: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("daemon failed to stop")
		}
	})
	waitForReload := func(count int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			log, _ := os.ReadFile(filepath.Join(StateDir(), "sync.log"))
			if strings.Count(string(log), `active theme changed to "editable"`) >= count {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("daemon did not observe palette update")
	}
	waitForReload(1)
	updated := strings.Replace(body, "background = #282a36", "background = #123456", 1)
	if updated == body {
		t.Fatal("palette fixture does not contain the expected background")
	}
	writeTestFile(t, filepath.Join(UserThemesDir(), "editable"), updated)
	waitForReload(2)
}

func TestServerRestartWaitsForPreviousDaemonLock(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows server PID metadata fixture")
	}
	state := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	socket := filepath.Join(state, "herdr.sock")
	writeTestFile(t, socket, strconv.Itoa(os.Getpid())+":123\n")
	t.Setenv("HERDR_SOCKET_PATH", socket)
	oldLock, err := instance.Acquire(filepath.Join(state, "sync.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oldLock.Close() })
	deadPID := 2147483647
	if terminal.IsProcessAlive(deadPID) {
		t.Fatal("dead server fixture unexpectedly exists")
	}
	writeTestFile(t, filepath.Join(state, "sync.server"), strconv.Itoa(deadPID)+"\n")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunDaemon(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("new server incorrectly skipped startup: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	oldLock.Close()
	deadline := time.Now().Add(time.Second)
	for {
		if data, err := os.ReadFile(DaemonPIDFile()); err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(os.Getpid()) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("new daemon did not start after previous lock was released")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("new daemon failed to stop")
	}
}
