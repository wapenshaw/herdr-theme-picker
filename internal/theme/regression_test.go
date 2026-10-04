package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func fixturePalette(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(PluginRoot(), "themes", "dracula-default"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteGuardsAndConfirmation(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	writeTestFile(t, filepath.Join(StateDir(), "keep.txt"), "keep")
	for _, slug := range []string{"../keep.txt", "dracula-default", "CON", "con", "com1", "-flag", strings.Repeat("a", 121)} {
		if err := deleteTheme(slug, strings.NewReader("y\n")); err == nil {
			t.Errorf("delete accepted %q", slug)
		}
		if err := EditTheme(slug); err == nil {
			t.Errorf("edit accepted %q", slug)
		}
	}
	if _, err := os.Stat(filepath.Join(StateDir(), "keep.txt")); err != nil {
		t.Fatal("guard removed unrelated file", err)
	}
	if err := saveUserTheme("custom", fixturePalette(t)); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, AppliedFile(), "custom\n")
	for _, answer := range []string{"n\n", "\n", ""} {
		if err := deleteTheme("custom", strings.NewReader(answer)); err != nil {
			t.Fatal(err)
		}
		if !IsUserTheme("custom") {
			t.Fatalf("deleted theme without affirmative confirmation %q", answer)
		}
	}
	if err := deleteTheme("custom", strings.NewReader("y\n")); err != nil {
		t.Fatal(err)
	}
	if IsUserTheme("custom") {
		t.Fatal("theme remains indexed")
	}
	if _, err := os.Stat(AppliedFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active marker remains: %v", err)
	}
}

func TestUserThemeSymlinkGuard(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	outside := filepath.Join(t.TempDir(), "outside")
	writeTestFile(t, outside, fixturePalette(t))
	if err := os.MkdirAll(UserThemesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(UserThemesDir(), "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeTestFile(t, UserIndexFile(), "linked\n")
	if err := deleteTheme("linked", strings.NewReader("y\n")); err == nil {
		t.Fatal("followed linked theme")
	}
	if err := saveUserTheme("linked", fixturePalette(t)); err == nil {
		t.Fatal("overwrote linked theme")
	}
	if _, err := ResolvePalette("linked"); err == nil {
		t.Fatal("resolved linked user theme")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal(err)
	}
}

func TestCustomThemeOverridesBundledName(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	body := strings.Replace(fixturePalette(t), "background = #282a36", "background = #123456", 1)
	if err := saveUserTheme("dracula-default", body); err != nil {
		t.Fatal(err)
	}
	path, err := ResolvePalette("dracula-default")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(UserThemesDir(), "dracula-default") {
		t.Fatal("bundled theme shadowed custom theme")
	}
	pal, err := ParsePaletteFile(path)
	if err != nil || pal.Background != "#123456" {
		t.Fatalf("wrong custom palette: %v", err)
	}
}

func TestConfigPreservesTOMLSyntax(t *testing.T) {
	cases := []string{
		"# keep\n[ui]\nmessage = '''\n[theme.custom]\nkeep this message\n'''\n",
		"onboarding = false\n[theme]\nname = 'catppuccin'\n[ 'theme' . \"custom\" ] # old tokens\naccent = '#ffffff'\n[ui]\nitems = [\n  '[theme.custom]',\n]\n",
		"theme.custom.accent = '#ffffff'\nonboarding = false\n",
		"[theme]\nname = 'catppuccin'\ncustom = { accent = '#ffffff' }\n[ui]\nwidth = 30\n",
		"theme = { name = 'catppuccin', custom = { accent = '#ffffff' } }\nonboarding = false\n[ui]\nwidth = 30\n",
		"# CRLF\r\n[theme.custom]\r\naccent = '#ffffff'\r\n[ui]\r\nwidth = 30\r\n",
	}
	for i, initial := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			cfg := filepath.Join(t.TempDir(), "config.toml")
			writeTestFile(t, cfg, initial)
			var before map[string]any
			if err := toml.Unmarshal([]byte(initial), &before); err != nil {
				t.Fatal(err)
			}
			for j := 0; j < 2; j++ {
				if err := WriteCustomBlock(cfg, map[string]string{"accent": "#123456"}); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(cfg)
				if err != nil {
					t.Fatal(err)
				}
				var after map[string]any
				if err := toml.Unmarshal(data, &after); err != nil {
					t.Fatal(err)
				}
				if after["theme"].(map[string]any)["custom"].(map[string]any)["accent"] != "#123456" {
					t.Fatal("token did not update")
				}
				delete(after["theme"].(map[string]any), "custom")
				if theme, ok := before["theme"].(map[string]any); ok {
					delete(theme, "custom")
				} else {
					before["theme"] = map[string]any{}
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("unrelated TOML changed: before=%#v after=%#v", before, after)
				}
				if strings.Contains(initial, "# keep") && !bytes.Contains(data, []byte("# keep")) {
					t.Fatal("comment lost")
				}
				if strings.Contains(initial, "\r\n") && bytes.Contains(bytes.ReplaceAll(data, []byte("\r\n"), nil), []byte("\n")) {
					t.Fatal("CRLF changed")
				}
			}
			backup, err := os.ReadFile(cfg + ".bak-" + time.Now().Format("20060102"))
			if err != nil || string(backup) != initial {
				t.Fatalf("original backup not preserved: %v", err)
			}
		})
	}
}

func TestConfigRejectsUnsafeWrites(t *testing.T) {
	for _, kind := range []string{"invalid-toml", "invalid-token", "invalid-color", "backup-directory"} {
		t.Run(kind, func(t *testing.T) {
			cfg := filepath.Join(t.TempDir(), "config.toml")
			body := "[ui]\nwidth = 30\n"
			tokens := map[string]string{"accent": "#123456"}
			if kind == "invalid-toml" {
				body = "[ui\n"
			}
			if kind == "invalid-token" {
				tokens["unknown"] = "#123456"
			}
			if kind == "invalid-color" {
				tokens["accent"] = "#123"
			}
			writeTestFile(t, cfg, body)
			if kind == "backup-directory" {
				if err := os.Mkdir(cfg+".bak-"+time.Now().Format("20060102"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := WriteCustomBlock(cfg, tokens); err == nil {
				t.Fatal("expected failure")
			}
			got, err := os.ReadFile(cfg)
			if err != nil || string(got) != body {
				t.Fatal("failed operation modified config")
			}
		})
	}
}

func TestPaletteRejectsMalformedInput(t *testing.T) {
	valid := fixturePalette(t)
	cases := []string{
		"palette = 0=#123\n",
		strings.Replace(valid, "background = #282a36\n", "", 1),
		strings.Replace(valid, "foreground = #f8f8f2\n", "", 1),
		strings.Replace(valid, "palette = 15=#ffffff\n", "", 1),
		strings.Replace(valid, "#282a36", "#123xyz", 1),
		valid + "palette = 16=#123456\n",
		valid + "palette = 999999999999999999999=#123456\n",
		valid + "# hpick-override: unknown=#123456\n",
		valid + "# hpick-override: accent=#123456junk\n",
		strings.Repeat("x", maxPaletteBytes+1),
	}
	for i, input := range cases {
		if _, err := ParsePaletteContent(input); err == nil {
			t.Errorf("accepted malformed case %d", i)
		}
	}
	pal, err := ParsePaletteContent("\ufeff" + strings.ReplaceAll(valid, "#282a36", "282A36"))
	if err != nil || pal.Background != "#282a36" {
		t.Fatalf("normalization failed: %v", err)
	}
	if _, err := PaletteToTokens(nil); err == nil {
		t.Fatal("nil palette accepted")
	}
}

func TestDownloadValidationAndCacheRecovery(t *testing.T) {
	valid := fixturePalette(t)
	for _, kind := range []string{"valid", "invalid", "oversize", "not-found", "partial-cache"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/test-remote" {
					t.Errorf("unexpected URL: %s", r.URL.Path)
				}
				switch kind {
				case "invalid":
					fmt.Fprint(w, "palette = 0=#123456\n")
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", maxPaletteBytes+1))
				case "not-found":
					http.NotFound(w, r)
				default:
					fmt.Fprint(w, valid)
				}
			}))
			defer server.Close()
			cache := filepath.Join(CacheDir(), "test-remote")
			if kind == "partial-cache" {
				writeTestFile(t, cache, "palette = 0=#123456\n")
			}
			path, err := resolvePalette("test-remote", server.URL, server.Client())
			if kind != "valid" && kind != "partial-cache" {
				if err == nil {
					t.Fatal("bad download accepted")
				}
				if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("bad download cached")
				}
				return
			}
			if err != nil || path != cache {
				t.Fatalf("download failed: %v", err)
			}
			if _, err := ParsePaletteFile(path); err != nil {
				t.Fatal(err)
			}
			if _, err := resolvePalette("test-remote", server.URL, server.Client()); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("valid cache downloaded again: %d calls", calls)
			}
		})
	}
}

func TestTerminalClientSelection(t *testing.T) {
	processes := map[int]processInfo{
		10: {pid: 10, parent: 1, name: "herdr.exe", tty: "console"},
		20: {pid: 20, parent: 10, name: "sh"},
		30: {pid: 30, parent: 1, name: "/usr/bin/herdr", tty: "pts/9"},
		40: {pid: 40, parent: 1, name: "herdr"},
		50: {pid: 50, parent: 50, name: "cycle"},
	}
	client, err := selectTerminalClient(processes, 20, "")
	if err != nil || client.pid != 10 {
		t.Fatalf("wrong ancestor: %v %v", client, err)
	}
	for _, parent := range []int{1, 40, 50} {
		if _, err := selectTerminalClient(processes, parent, ""); !errors.Is(err, errNoOuterTerminal) {
			t.Fatalf("chose unrelated client from %d: %v", parent, err)
		}
	}
	client, err = selectTerminalClient(processes, 20, "30")
	if err != nil || client.pid != 30 {
		t.Fatal("explicit target ignored", err)
	}
	for _, pid := range []string{"invalid", "0", "-1", "20", "999"} {
		if _, err := selectTerminalClient(processes, 20, pid); err == nil {
			t.Errorf("accepted PID %q", pid)
		}
	}
}

func TestEditorArguments(t *testing.T) {
	for command, want := range map[string][]string{
		"code --wait": {"code", "--wait"},
		`"C:\Program Files\Editor\edit.exe" --wait`: {`C:\Program Files\Editor\edit.exe`, "--wait"},
		`editor --flag 'two words'`:                 {"editor", "--flag", "two words"},
		`editor --flag ""`:                          {"editor", "--flag", ""},
	} {
		got, err := splitEditor(command)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, err=%v", command, got, err)
		}
	}
	if _, err := splitEditor(`editor "unclosed`); err == nil {
		t.Fatal("unclosed quote accepted")
	}
	t.Setenv("VISUAL", "code --wait")
	if PickEditor() != "code --wait" {
		t.Fatal("explicit editor preference ignored")
	}
}

func TestOverrideValidation(t *testing.T) {
	body := fixturePalette(t) + "# hpick-override: accent=#abcdef\n"
	updated, err := setOverride(body, "accent", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(updated, "hpick-override: accent=") != 1 {
		t.Fatal("duplicate override")
	}
	pal, err := ParsePaletteContent(updated)
	if err != nil || pal.Overrides["accent"] != "#123456" {
		t.Fatal("override did not replace", err)
	}
	for _, color := range []string{"#123", "#123456junk", "red"} {
		if _, err := setOverride(body, "accent", color); err == nil {
			t.Error("invalid color accepted", color)
		}
	}
}

func TestOutputBound(t *testing.T) {
	var b limitedBuffer
	if _, err := io.Copy(&b, strings.NewReader(strings.Repeat("x", maxPaletteBytes+1))); err == nil {
		t.Fatal("unbounded subprocess output accepted")
	}
}

type fzfStep struct {
	Selection string
	ExitCode  int
}

// The same test binary acts as a fake fzf or Herdr in subprocess tests. This
// avoids assuming a POSIX shell or using platform-specific executable scripts.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "terminal-sync" {
		if err := RunTerminalSyncHelper(os.Args[2]); err != nil {
			if IsOuterTerminalUnavailable(err) {
				os.Exit(3)
			}
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv("THEME_TEST_ARGV") == "1" {
		json.NewEncoder(os.Stdout).Encode(os.Args[1:])
		os.Exit(0)
	}
	if script := os.Getenv("THEME_TEST_FZF_SCRIPT"); script != "" {
		exe, _ := os.Executable()
		if strings.TrimSuffix(filepath.Base(exe), ".exe") == "fzf" {
			data, err := os.ReadFile(script)
			if err != nil {
				os.Exit(2)
			}
			var steps []fzfStep
			if json.Unmarshal(data, &steps) != nil {
				os.Exit(2)
			}
			counter, _ := os.ReadFile(script + ".count")
			n, _ := strconv.Atoi(string(counter))
			if n >= len(steps) {
				os.Exit(2)
			}
			if os.WriteFile(script+".count", []byte(strconv.Itoa(n+1)), 0o600) != nil {
				os.Exit(2)
			}
			if steps[n].ExitCode != 0 {
				os.Exit(steps[n].ExitCode)
			}
			fmt.Print(steps[n].Selection)
			os.Exit(0)
		}
		if strings.TrimSuffix(filepath.Base(exe), ".exe") == "herdr" {
			fmt.Fprintln(os.Stderr, "simulated reload failure")
			os.Exit(2)
		}
	}
	os.Exit(m.Run())
}

func copyTestExecutable(t *testing.T, path string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func fakeFZF(t *testing.T, steps ...fzfStep) string {
	t.Helper()
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	copyTestExecutable(t, filepath.Join(dir, "fzf"+suffix))
	copyTestExecutable(t, filepath.Join(dir, "herdr"+suffix))
	script := filepath.Join(dir, "steps.json")
	data, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, script, string(data))
	t.Setenv("THEME_TEST_FZF_SCRIPT", script)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_BIN_PATH", filepath.Join(dir, "herdr"+suffix))
	return script
}

func TestFZFCancellationAndFailures(t *testing.T) {
	for _, code := range []int{1, 130, 2} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			fakeFZF(t, fzfStep{ExitCode: code})
			_, _, cancelled, err := runFZF("one", "", "", "esc", "preview")
			if code == 2 {
				if err == nil {
					t.Fatal("fzf error swallowed")
				}
			} else if err != nil || !cancelled {
				t.Fatal("cancel was treated as failure", err)
			}
		})
	}
	for _, output := range []string{"esc\none\n", "broken"} {
		t.Run(output, func(t *testing.T) {
			fakeFZF(t, fzfStep{Selection: output})
			_, _, cancelled, err := runFZF("one", "", "", "esc", "preview")
			if output == "broken" {
				if err == nil {
					t.Fatal("malformed output accepted")
				}
			} else if !cancelled || err != nil {
				t.Fatal("explicit Escape ignored")
			}
		})
	}
}

func TestEditorDiscardLeavesOriginal(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	body := fixturePalette(t)
	if err := saveUserTheme("editable", body); err != nil {
		t.Fatal(err)
	}
	fakeFZF(t, fzfStep{Selection: "\naccent #123456\n"}, fzfStep{Selection: "esc\naccent #123456\n"})
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if _, err := stdin.WriteString("#654321\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	originalStdin := os.Stdin
	os.Stdin = stdin
	t.Cleanup(func() { os.Stdin = originalStdin })
	if err := EditTheme("editable"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(UserThemesDir(), "editable"))
	if err != nil || string(got) != body {
		t.Fatal("discard changed original theme")
	}
}

func TestEditorSaveCommitsAndReportsApplyFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := saveUserTheme("editable", fixturePalette(t)); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	writeTestFile(t, cfg, "[ui]\nwidth = 30\n")
	t.Setenv("HERDR_CONFIG_PATH", cfg)
	t.Setenv("HERDR_THEME_CLIENT_PID", "invalid")
	fakeFZF(t, fzfStep{Selection: "\naccent #123456\n"}, fzfStep{Selection: "\n" + saveRow + "\n"})
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if _, err := stdin.WriteString("#654321\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	originalStdin, originalStdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdin, stdout
	t.Cleanup(func() { os.Stdin, os.Stdout = originalStdin, originalStdout })
	if err := EditTheme("editable"); err == nil || !strings.Contains(err.Error(), "simulated reload failure") {
		t.Fatalf("apply failure swallowed: %v", err)
	}
	pal, err := ParsePaletteFile(filepath.Join(UserThemesDir(), "editable"))
	if err != nil || pal.Overrides["accent"] != "#654321" {
		t.Fatal("Save did not commit override", err)
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document["theme"].(map[string]any)["custom"].(map[string]any)["accent"] != "#654321" {
		t.Fatal("Save did not apply updated tokens")
	}
}

func TestWindowsEditorShim(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows editor shim")
	}
	path := filepath.Join(t.TempDir(), "editor with spaces.cmd")
	writeTestFile(t, path, "@echo off\r\necho %~1\r\necho %~2\r\n")
	t.Setenv("VISUAL", fmt.Sprintf("%q --wait", path))
	file := filepath.Join(t.TempDir(), "palette with spaces.txt")
	cmd, err := editorCommand(file)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("editor shim failed: %v %s", err, out)
	}
	if strings.TrimSpace(strings.ReplaceAll(string(out), "\r\n", "\n")) != "--wait\n"+file {
		t.Fatalf("editor args changed: %q", out)
	}
}

func TestApplyReportsReloadFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	cfg := filepath.Join(t.TempDir(), "config.toml")
	writeTestFile(t, cfg, "[ui]\nwidth = 30\n")
	t.Setenv("HERDR_CONFIG_PATH", cfg)
	t.Setenv("HERDR_THEME_CLIENT_PID", "invalid") // Never emit into a real terminal during tests.
	fakeFZF(t)
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	originalStdout := os.Stdout
	os.Stdout = stdout
	t.Cleanup(func() { os.Stdout = originalStdout })
	err = ApplyTheme("dracula-default")
	if err == nil || !strings.Contains(err.Error(), "simulated reload failure") {
		t.Fatalf("reload error swallowed: %v", err)
	}
	if _, err := os.Stat(AppliedFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed reload marked theme as applied")
	}
}

func TestPreviewCommandExecutesQuotedPaths(t *testing.T) {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	exe := filepath.Join(t.TempDir(), "preview 'quoted' $ path"+suffix)
	copyTestExecutable(t, exe)
	path := filepath.Join(t.TempDir(), "scratch 'quoted' $ file")
	command, shell := previewCommand(exe, "preview-file", path)
	command = strings.Replace(command, "{}", "'picked row'", 1)
	args := strings.Fields(shell)
	cmd := exec.Command(args[0], append(args[1:], command)...)
	cmd.Env = append(os.Environ(), "THEME_TEST_ARGV=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("preview did not execute: %v %s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err, string(out))
	}
	want := []string{"preview-file", path, "picked row"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("preview quoting changed arguments: got %q, want %q", got, want)
	}
}
