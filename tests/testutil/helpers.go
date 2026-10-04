package testutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"herdr-theme-picker/internal/terminal"
)

func WriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

type FZFStep struct {
	Selection string
	ExitCode  int
}

// The same test binary acts as a fake fzf or Herdr in subprocess tests. This
// avoids assuming a POSIX shell or using platform-specific executable scripts.
func Main(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "terminal-sync" {
		if err := terminal.RunHelper(os.Args[2], len(os.Args) > 3 && os.Args[3] == "--ancestor"); err != nil {
			if errors.Is(err, terminal.ErrUnavailable) {
				os.Exit(3)
			}
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv("THEME_TEST_CONSOLE_CLIENT") == "1" {
		fmt.Fprintln(os.Stdout, "ready")
		io.Copy(io.Discard, os.Stdin)
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
			var steps []FZFStep
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

func CopyExecutable(t *testing.T, path string) {
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

func FakeFZF(t *testing.T, steps ...FZFStep) string {
	t.Helper()
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	CopyExecutable(t, filepath.Join(dir, "fzf"+suffix))
	CopyExecutable(t, filepath.Join(dir, "herdr"+suffix))
	script := filepath.Join(dir, "steps.json")
	data, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	WriteFile(t, script, string(data))
	t.Setenv("THEME_TEST_FZF_SCRIPT", script)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_BIN_PATH", filepath.Join(dir, "herdr"+suffix))
	return script
}
