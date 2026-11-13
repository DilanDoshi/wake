package ui

// A /clear while a background shell runs, as claude 2.1.322 recorded it
// (testdata/stream/clear-keeps-background.jsonl): the shell goes on, and its
// ending arrives after the clear under the new conversation. The recording's
// ui_invalidate frame is left out, a frame this decoder does not read yet, and
// its init frames' placeholder command lists are cut to the allowlist.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestAShellRunningAcrossAClearKeepsItsRowAndItsEndingIsNamed(t *testing.T) {
	evs := eventsOf(t, fixtureLines(t, "clear-keeps-background.jsonl"))
	reset := -1
	for i, ev := range evs {
		if ev.Kind == core.KindSessionReset {
			reset = i
			break
		}
	}
	if reset < 0 {
		t.Fatal("the recording holds no /clear")
	}

	f := NewFleet()
	for _, ev := range evs[:reset+1] {
		f, _ = f.Observe(ev, "s1")
	}
	rows := f.RunningTasks("s1")
	if len(rows) != 1 || rows[0].Kind != core.TaskShell {
		t.Fatalf("running after the clear = %+v, want the background shell it did not kill", rows)
	}

	var ending core.Event
	for _, ev := range evs[reset+1:] {
		if ev.Task != nil && ev.Task.Phase == core.TaskEnded {
			ending = f.named("s1", ev)
		}
		f, _ = f.Observe(ev, "s1")
	}
	if ending.Task == nil {
		t.Fatal("the recording holds no ending after the clear")
	}
	if !strings.Contains(ending.Task.Label, "ping") {
		t.Errorf("the ending is named %q, want the shell's own description: its row was dropped at the clear", ending.Task.Label)
	}
	if got := f.RunningTasks("s1"); len(got) != 0 {
		t.Errorf("rows still running after the shell ended: %+v", got)
	}
}
