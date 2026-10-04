package terminal_test

import (
	"errors"
	"reflect"
	"testing"

	"herdr-theme-picker/internal/terminal"
)

func TestReattachOnSameTTYBetweenPolls(t *testing.T) {
	var tracker terminal.Tracker
	var writes []int
	emit := func(c terminal.Client, _ string) error { writes = append(writes, c.PID); return nil }
	old := terminal.Client{PID: 10, TTY: "pts/1"}
	newClient := terminal.Client{PID: 20, TTY: "pts/1"}
	for _, clients := range [][]terminal.Client{{old}, {old}, {newClient}, {newClient}} {
		if err := tracker.Sync("theme", clients, emit); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(writes, []int{10, 20}) {
		t.Fatalf("reattach did not receive theme: %v", writes)
	}
}

func TestPaletteEditAndDisconnect(t *testing.T) {
	var tracker terminal.Tracker
	client := terminal.Client{PID: 10, TTY: "console"}
	var writes []string
	emit := func(_ terminal.Client, payload string) error { writes = append(writes, payload); return nil }
	for _, step := range []struct {
		payload string
		clients []terminal.Client
	}{
		{"old", []terminal.Client{client}},
		{"edited", []terminal.Client{client}},
		{"edited", nil},
		{"edited", []terminal.Client{client}},
	} {
		if err := tracker.Sync(step.payload, step.clients, emit); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(writes, []string{"old", "edited", "edited"}) {
		t.Fatalf("stale palette or connection state: %v", writes)
	}
}

func TestFailedWriteIsRetriedIndependently(t *testing.T) {
	var tracker terminal.Tracker
	clients := []terminal.Client{{PID: 10}, {PID: 20}}
	attempts := make(map[int]int)
	emit := func(c terminal.Client, _ string) error {
		attempts[c.PID]++
		if c.PID == 10 && attempts[c.PID] == 1 {
			return terminal.ErrUnavailable
		}
		return nil
	}
	for i := 0; i < 3; i++ {
		if err := tracker.Sync("theme", clients, emit); err != nil {
			t.Fatal(err)
		}
	}
	if attempts[10] != 2 || attempts[20] != 1 {
		t.Fatalf("failed target marked successful or affected another target: %v", attempts)
	}
}

func TestWriteErrorsAreReported(t *testing.T) {
	var tracker terminal.Tracker
	failure := errors.New("write failed")
	err := tracker.Sync("theme", []terminal.Client{{PID: 10}}, func(terminal.Client, string) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("lost write failure: %v", err)
	}
}
