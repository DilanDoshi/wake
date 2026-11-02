package ui

// A card opens on its foot whatever the pane pins above it - the task board, the
// working line, a streamed preview - or stacks under it, a picker: the foot is
// read off the page's own layout, never assumed from the transcript's rows.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// pinned is what a conversation can hold beside its transcript when an ask arrives.
var pinned = []string{"nothing", "board", "working", "preview", "picker"}

// askingUnder is askingPage with pin in the pane: the board, the working line and
// the preview before the ask arrives, the picker after it.
func askingUnder(t *testing.T, height int, pin string) App {
	t.Helper()
	a := newRoomApp(t).withSize(narrowColumns, height).withAgents("john")
	a = pick(a, "s1").openDMWith("s1", "john").applyGeometry()
	for i := range pageHistory {
		a = a.applyFrame(rpcEvent("s1", &core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("history line %02d", i)}))
	}
	switch pin {
	case "board":
		for i := range 3 {
			ev := taskCreate(fmt.Sprintf("task %d", i+1), fmt.Sprintf("doing task %d", i+1))
			a = a.applyFrame(rpcEvent("s1", &ev))
		}
	case "working":
		st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "john", State: rpc.StateWorking}}}
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	case "preview":
		a = a.applyFrame(rpcEvent("s1", &core.Event{Kind: core.KindPartialText, Text: "streamed one\nstreamed two\nstreamed three"}))
	}
	a = a.applyFrame(rpcEvent("s1", questionAsk(t)))
	if pin == "picker" {
		a = a.openPicker(effortCommand, []string{"s1"})
	}
	return a
}

// Whatever is pinned, a card the pane can hold is answerable at rest, and one it
// cannot opens with its key line - which says the keys wait - in view.
func TestACardOpensOnItsFootUnderWhatThePanePins(t *testing.T) {
	for _, pin := range pinned {
		var sawFit, sawCut bool
		for height := 7; height <= 22; height++ {
			a := askingUnder(t, height, pin)
			width, pane, _ := a.focusedPane()
			fits := lipgloss.Height(a.cardBlock("s1", width, false)) <= pane
			rows := pageRows(a)
			if got := a.cardFullyDrawn(); got != fits {
				t.Errorf("%s pinned, %d-row pane: answerable=%v, want %v:\n%s", pin, pane, got, fits, strings.Join(rows, "\n"))
			}
			if fits && !cardWholeOn(rows, lipgloss.Height(a.cardBlock("s1", width, false))) {
				t.Errorf("%s pinned, %d-row pane: answerable, but a row of the card is off screen:\n%s", pin, pane, strings.Join(rows, "\n"))
			}
			if !fits && !hasRow(rows, cardKeysCut) {
				t.Errorf("%s pinned, %d-row pane: the card's foot is not in view:\n%s", pin, pane, strings.Join(rows, "\n"))
			}
			sawFit, sawCut = sawFit || fits, sawCut || !fits
		}
		if !sawFit || !sawCut {
			t.Fatalf("%s pinned: the sweep never crossed the card's height (fit=%v cut=%v)", pin, sawFit, sawCut)
		}
	}
}
