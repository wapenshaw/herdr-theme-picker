//go:build windows

package terminal

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

func Processes() (map[int]Client, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	processes := make(map[int]Client)
	for {
		pid := int(entry.ProcessID)
		p := Client{PID: pid, Parent: int(entry.ParentProcessID), Name: windows.UTF16ToString(entry.ExeFile[:]), TTY: "console"}
		if IsHerdr(p.Name) {
			args, err := processArgs(pid)
			p.Interactive = err == nil && IsInteractiveCommand(args)
		}
		processes[pid] = p
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

// Emit uses an explicit target or walks ancestors inside an isolated helper,
// where attaching and detaching cannot disturb the picker or daemon console.
func Emit(payload string) error {
	if explicit := os.Getenv("HERDR_THEME_CLIENT_PID"); explicit != "" {
		processes, err := Processes()
		if err != nil {
			return err
		}
		client, err := SelectClient(processes, 0, explicit)
		if err != nil {
			return err
		}
		return EmitClient(client, payload)
	}
	return runHelper(HostPayload(payload), strconv.Itoa(os.Getppid()), "--ancestor")
}

// EmitClient always writes to the requested client. Environment overrides and
// unrelated processes must not turn a failed write into a false success.
func EmitClient(client Client, payload string) error {
	return runHelper(HostPayload(payload), strconv.Itoa(client.PID))
}

func runHelper(payload string, args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, append([]string{"terminal-sync"}, args...)...)
	cmd.Stdin = strings.NewReader(payload)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 3 {
			return ErrUnavailable
		}
		return fmt.Errorf("sync terminal: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	event, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}

// RunHelper must be called only by the short-lived terminal-sync subprocess.
// Exact targets never fall back; ancestor mode skips headless Herdr processes.
func RunHelper(pidText string, ancestor bool) error {
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid client PID %q", pidText)
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, maxPayloadBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxPayloadBytes {
		return fmt.Errorf("terminal payload is too large")
	}
	processes, err := Processes()
	if err != nil {
		return err
	}
	if ret, _, err := freeConsole.Call(); ret == 0 {
		return fmt.Errorf("detach helper console: %w", err)
	}
	seen := make(map[int]bool)
	attached := false
	for pid > 0 && !seen[pid] {
		seen[pid] = true
		p, ok := processes[pid]
		if !ok {
			break
		}
		if p.isInteractive() {
			if ret, _, attachErr := attachConsole.Call(uintptr(pid)); ret != 0 {
				attached = true
				break
			} else if !ancestor && !errors.Is(attachErr, windows.ERROR_INVALID_HANDLE) && !errors.Is(attachErr, windows.ERROR_INVALID_PARAMETER) {
				return fmt.Errorf("attach client console: %w", attachErr)
			}
		}
		if !ancestor {
			break
		}
		pid = p.Parent
	}
	if !attached {
		return ErrUnavailable
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
