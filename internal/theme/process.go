package theme

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type limitedBuffer struct{ buffer bytes.Buffer }

func (b *limitedBuffer) String() string { return b.buffer.String() }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > maxPaletteBytes {
		return 0, fmt.Errorf("output exceeds %d bytes", maxPaletteBytes)
	}
	return b.buffer.Write(p)
}

var errNoOuterTerminal = errors.New("owning host terminal unavailable")

// IsOuterTerminalUnavailable lets the helper CLI distinguish unavailable host
// consoles from write failures without matching error strings.
func IsOuterTerminalUnavailable(err error) bool { return errors.Is(err, errNoOuterTerminal) }

type processInfo struct {
	pid, parent int
	name, tty   string
}

// Never fall back to choosing a process by name from another session.
func selectTerminalClient(processes map[int]processInfo, parent int, explicit string) (processInfo, error) {
	if explicit != "" {
		pid, err := strconv.Atoi(explicit)
		if err != nil || pid <= 0 {
			return processInfo{}, fmt.Errorf("invalid HERDR_THEME_CLIENT_PID %q", explicit)
		}
		p, ok := processes[pid]
		if !ok || !isHerdrProcess(p.name) {
			return processInfo{}, fmt.Errorf("PID %d is not a Herdr client", pid)
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
		if isHerdrProcess(p.name) && p.tty != "" {
			return p, nil
		}
		parent = p.parent
	}
	return processInfo{}, errNoOuterTerminal
}

func isHerdrProcess(name string) bool {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(name, "/")
	base := parts[len(parts)-1]
	return base == "herdr" || strings.EqualFold(base, "herdr.exe")
}

// fzf runs previews in a shell. Explicitly select one so inherited SHELL and
// FZF_DEFAULT_OPTS cannot change how our executable path is interpreted.
func previewCommand(exe string, args ...string) (string, string) {
	shell := "sh -c"
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	prefix := ""
	if runtime.GOOS == "windows" {
		shell = "powershell.exe -NoProfile -NonInteractive -Command"
		if _, err := exec.LookPath("pwsh.exe"); err == nil {
			shell = "pwsh.exe -NoProfile -NonInteractive -Command"
		}
		quote = func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
		prefix = "& "
	}
	parts := []string{prefix + quote(exe)}
	for _, arg := range args {
		parts = append(parts, quote(arg))
	}
	return strings.Join(parts, " ") + " {}", shell
}

func runFZF(input, prompt, header, expect string, previewArgs ...string) (string, string, bool, error) {
	fzf, err := exec.LookPath("fzf")
	if err != nil {
		return "", "", false, fmt.Errorf("fzf is not installed: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", "", false, err
	}
	preview, shell := previewCommand(strings.TrimPrefix(exe, `\\?\`), previewArgs...)
	cmd := exec.Command(fzf, "--layout=reverse", "--prompt="+prompt, "--header="+header,
		"--info=inline", "--height=100%", "--expect="+expect, "--with-shell="+shell,
		"--preview="+preview, "--preview-window=right,62%,border-left")
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr
	var output limitedBuffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 130 || exit.ExitCode() == 1) {
			return "", "", true, nil
		}
		return "", "", false, fmt.Errorf("fzf failed: %w", err)
	}
	lines := strings.Split(strings.TrimRight(output.String(), "\r\n"), "\n")
	if len(lines) < 2 {
		return "", "", false, fmt.Errorf("fzf returned malformed selection")
	}
	key := strings.TrimSpace(lines[0])
	return key, strings.TrimSpace(lines[1]), key == "esc", nil
}
