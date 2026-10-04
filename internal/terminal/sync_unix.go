//go:build !windows

package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func Processes() (map[int]Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,ppid,tty,comm").Output()
	if err != nil {
		return nil, fmt.Errorf("locate terminal client: %w", err)
	}
	processes := make(map[int]Client)
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
		p := Client{PID: pid, Parent: parent, TTY: tty, Name: strings.Join(fields[3:], " ")}
		// Remote bridges have no TTY, but their arguments are still needed to
		// tell whether every attached client lives on another machine.
		if IsHerdr(p.Name) {
			if args, err := processArgs(pid); err == nil {
				classify(&p, args)
			}
		}
		processes[pid] = p
	}
	return processes, nil
}

func EmitClient(client Client, payload string) error {
	if client.TTY == "" {
		return ErrUnavailable
	}
	// Revalidate immediately before opening the TTY; discovery may have raced
	// with a detach or with a different process taking over that terminal.
	processes, err := Processes()
	if err != nil {
		return err
	}
	p, ok := processes[client.PID]
	if !ok || !p.isInteractive() || p.TTY != client.TTY {
		return ErrUnavailable
	}
	path := filepath.Clean(filepath.Join("/dev", client.TTY))
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
	f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrPermission) {
		// Another account's client: not ours to recolor.
		return ErrUnavailable
	}
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(HostPayload(payload))
	return err
}

func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func RunHelper(string) error { return fmt.Errorf("console helper is Windows-only") }
