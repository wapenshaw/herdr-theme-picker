package theme_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"herdr-theme-picker/internal/theme"
)

func TestStandaloneUsesInstalledPluginState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", root)
	managed := filepath.Join(root, "herdr", "plugins", "herdr-theme-picker")
	writeTestFile(t, filepath.Join(managed, "applied"), "ayu-dark\n")
	if got := theme.StateDir(); got != managed {
		t.Fatalf("standalone sync uses %q instead of plugin state %q", got, managed)
	}
	explicit := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", explicit)
	if got := theme.StateDir(); got != explicit {
		t.Fatalf("explicit state directory ignored: %q", got)
	}
}

func TestStandaloneStateWindowsCompatibility(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows standalone and installed state locations")
	}
	root := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("LOCALAPPDATA", root)
	legacy := filepath.Join(root, "herdr-theme-picker")
	writeTestFile(t, filepath.Join(legacy, "applied"), "dracula-default\n")
	if got := theme.StateDir(); got != legacy {
		t.Fatalf("uninstalled standalone state moved unexpectedly: %q", got)
	}
	managed := filepath.Join(root, "herdr", "plugins", "herdr-theme-picker")
	writeTestFile(t, filepath.Join(managed, "applied"), "ayu-dark\n")
	if got := theme.StateDir(); got != managed {
		t.Fatalf("installed state did not take precedence: %q", got)
	}
	data, err := os.ReadFile(theme.AppliedFile())
	if err != nil || string(data) != "ayu-dark\n" {
		t.Fatalf("standalone would restore the stale selection: %q %v", data, err)
	}
}
