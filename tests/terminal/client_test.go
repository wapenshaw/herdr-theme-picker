package terminal_test

import (
	"errors"
	"testing"

	. "herdr-theme-picker/internal/terminal"
)

func TestTerminalClientSelection(t *testing.T) {
	processes := map[int]Client{
		10: {PID: 10, Parent: 1, Name: "herdr.exe", TTY: "console", Interactive: true},
		20: {PID: 20, Parent: 10, Name: "sh"},
		30: {PID: 30, Parent: 1, Name: "/usr/bin/herdr", TTY: "pts/9", Interactive: true},
		40: {PID: 40, Parent: 1, Name: "herdr"},
		50: {PID: 50, Parent: 50, Name: "cycle"},
		60: {PID: 60, Parent: 1, Name: "herdr.exe", TTY: "console"}, // server stop
	}
	client, err := SelectClient(processes, 20, "")
	if err != nil || client.PID != 10 {
		t.Fatalf("wrong ancestor: %v %v", client, err)
	}
	for _, parent := range []int{1, 40, 50, 60} {
		if _, err := SelectClient(processes, parent, ""); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("chose unrelated client from %d: %v", parent, err)
		}
	}
	client, err = SelectClient(processes, 20, "30")
	if err != nil || client.PID != 30 {
		t.Fatal("explicit target ignored", err)
	}
	for _, pid := range []string{"invalid", "0", "-1", "20", "40", "60", "999"} {
		if _, err := SelectClient(processes, 20, pid); err == nil {
			t.Errorf("accepted PID %q", pid)
		}
	}
}

func TestInteractiveLaunchModes(t *testing.T) {
	for _, args := range [][]string{
		{"herdr"},
		{"/usr/local/bin/herdr", "client"},
		{`C:\Program Files\Herdr\herdr.exe`, "--session", "server"},
		{"herdr", "--session=work projects"},
		{"herdr", "--remote", "dev-host", "--remote-keybindings", "server"},
		{"herdr", "--remote=dev-host", "--handoff"},
	} {
		if !IsInteractiveCommand(args) {
			t.Errorf("rejected interactive launch: %q", args)
		}
	}
	for _, args := range [][]string{
		nil, {}, {""}, {"pwsh"},
		{"herdr", "server"},
		{"herdr", "server", "stop"},
		{"herdr", "server", "reload-config"},
		{"herdr", "--session", "work", "server", "stop"},
		{"herdr", "status"},
		{"herdr", "plugin", "action", "invoke", "sync"},
		{"herdr", "pane", "list"},
		{"herdr", "update"},
		{"herdr", "remote-client-bridge"},
		{"herdr", "--machine", "dev", "status"},
		{"herdr", "--help"},
		{"herdr", "--version"},
		{"herdr", "--session"},
		{"herdr", "--session="},
		{"herdr", "--session", "--help"},
		{"herdr", "--remote-keybindings", "unknown"},
		{"herdr", "--unknown"},
	} {
		if IsInteractiveCommand(args) {
			t.Errorf("accepted CLI or unknown launch: %q", args)
		}
	}
}
