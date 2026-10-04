//go:build !windows

package theme

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func emitToOuterTerminal(payload string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,ppid,tty,comm").Output()
	if err != nil {
		return fmt.Errorf("locate terminal client: %w", err)
	}
	processes := make(map[int]processInfo)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		tty := fields[2]
		if tty == "?" || tty == "??" || tty == "-" {
			tty = ""
		}
		processes[pid] = processInfo{pid: pid, parent: parent, tty: tty, name: strings.Join(fields[3:], " ")}
	}
	client, err := selectTerminalClient(processes, os.Getppid(), os.Getenv("HERDR_THEME_CLIENT_PID"))
	if err != nil {
		return err
	}
	if client.tty == "" {
		return errNoOuterTerminal
	}
	path := filepath.Clean(filepath.Join("/dev", client.tty))
	if !strings.HasPrefix(path, "/dev/") {
		return fmt.Errorf("invalid client terminal path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("client terminal is not a character device")
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(payload)
	return err
}

func RunTerminalSyncHelper(string) error { return fmt.Errorf("console helper is Windows-only") }
