//go:build windows

package theme

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	consoleDLL    = windows.NewLazySystemDLL("kernel32.dll")
	freeConsole   = consoleDLL.NewProc("FreeConsole")
	attachConsole = consoleDLL.NewProc("AttachConsole")
)

func windowsProcesses() (map[int]processInfo, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	processes := make(map[int]processInfo)
	for {
		pid := int(entry.ProcessID)
		processes[pid] = processInfo{pid: pid, parent: int(entry.ParentProcessID), name: windows.UTF16ToString(entry.ExeFile[:]), tty: "console"}
		err := windows.Process32Next(snapshot, &entry)
		if err == windows.ERROR_NO_MORE_FILES {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return processes, nil
}

func canAttachConsole(pid int) bool {
	freeConsole.Call()
	ret, _, _ := attachConsole.Call(uintptr(pid))
	if ret != 0 {
		freeConsole.Call()
		return true
	}
	return false
}

func emitToOuterTerminal(payload string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Console ownership changes only in this short-lived process. The picker keeps
	// its original handles for prompts, error reporting and the reload command.
	cmd := exec.CommandContext(ctx, exe, "terminal-sync", strconv.Itoa(os.Getppid()))
	cmd.Stdin = strings.NewReader(payload)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 3 {
			return errNoOuterTerminal
		}
		return fmt.Errorf("sync terminal: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func RunTerminalSyncHelper(pidText string) error {
	processes, err := windowsProcesses()
	if err != nil {
		return err
	}
	var targetPID int
	if explicit := os.Getenv("HERDR_THEME_CLIENT_PID"); explicit != "" {
		if pid, err := strconv.Atoi(explicit); err == nil && pid > 0 {
			if p, ok := processes[pid]; ok && isHerdrProcess(p.name) && canAttachConsole(pid) {
				targetPID = pid
			}
		}
	}
	if targetPID == 0 {
		parent, _ := strconv.Atoi(pidText)
		seen := make(map[int]bool)
		for parent > 0 && !seen[parent] {
			seen[parent] = true
			p, ok := processes[parent]
			if !ok {
				break
			}
			if isHerdrProcess(p.name) && canAttachConsole(p.pid) {
				targetPID = p.pid
				break
			}
			parent = p.parent
		}
	}
	if targetPID == 0 {
		for _, p := range processes {
			if isHerdrProcess(p.name) && canAttachConsole(p.pid) {
				targetPID = p.pid
				break
			}
		}
	}
	if targetPID == 0 {
		return errNoOuterTerminal
	}

	data, err := io.ReadAll(io.LimitReader(os.Stdin, maxPaletteBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxPaletteBytes {
		return fmt.Errorf("terminal payload is too large")
	}
	if ret, _, err := freeConsole.Call(); ret == 0 {
		return fmt.Errorf("detach helper console: %w", err)
	}
	if ret, _, err := attachConsole.Call(uintptr(targetPID)); ret == 0 {
		if errors.Is(err, windows.ERROR_INVALID_HANDLE) {
			return errNoOuterTerminal
		}
		return fmt.Errorf("attach client console: %w", err)
	}
	defer freeConsole.Call()
	f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(f.Fd()), &mode); err != nil {
		return err
	}
	if err := windows.SetConsoleMode(windows.Handle(f.Fd()), mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return err
	}
	defer windows.SetConsoleMode(windows.Handle(f.Fd()), mode)
	_, err = f.Write(data)
	return err
}
