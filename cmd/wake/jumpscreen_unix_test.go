//go:build unix

package main

// ⌃K finds an agent by a few letters of its name and opens it, on a real screen:
// the real binary, the kill switch's pump and Bubble Tea's decoder between the
// keys and the box, and the new pane drawn where a click on the sidebar row puts it.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// jumpPaneRows is the screen's rows left of the sidebar: the sidebar draws the same
// agents' names on the same rows as the box.
func jumpPaneRows(rows []string, cols int) []string { return leftOf(rows, cols) }

func TestCtrlKFindsAnAgentAndOpensItOnARealScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	const cols, rows = 200, 40
	s := startWakeInAConversation(t, cols, rows)
	fleetOfNine(t, s) // the room alone; w1..w8 beside the agent bare wake started

	s.send("\x0b") // ⌃K
	s.awaitFrame("the jump box", func(r []string) bool {
		return strings.Contains(ansi.Strip(strings.Join(jumpPaneRows(r, cols), "\n")), "jump to agent")
	})
	s.send("w3")
	got := s.awaitFrame("the box narrowed to w3, the cursor on it", func(r []string) bool {
		for _, ln := range jumpPaneRows(r, cols) {
			if strings.Contains(ln, "› ") && strings.Contains(ln, " w3") {
				return true
			}
		}
		return false
	})
	// The rule sits between the filter line and the results.
	filter := -1
	for y, ln := range jumpPaneRows(got, cols) {
		if strings.Contains(ln, "⌕ w3") {
			filter = y
		}
	}
	if filter < 0 || !strings.Contains(jumpPaneRows(got, cols)[filter+1], strings.Repeat("─", 30)) {
		t.Errorf("no rule under the filter line.\n%s", s.dump())
	}
	if draft := strings.Join(jumpPaneRows(got, cols), "\n"); strings.Contains(draft, "> w3") {
		t.Errorf("typing reached the room's draft.\n%s", s.dump())
	}

	s.send("\r")
	opened := s.awaitFrame("w3 opened beside the room", func(r []string) bool { _, ok := paneNamed(r, cols, "w3"); return ok })
	if strings.Contains(ansi.Strip(strings.Join(jumpPaneRows(opened, cols), "\n")), "jump to agent") {
		t.Errorf("the box stayed up after ↵.\n%s", s.dump())
	}
	w3, _ := paneNamed(opened, cols, "w3")

	// ⌃B: w4 opens below the pane that holds the keys, in the same column.
	s.send("\x0b")
	s.await("jump to agent")
	s.send("w4")
	s.send("\x02") // ⌃B
	below := s.awaitFrame("w4 below w3", func(r []string) bool { _, ok := paneNamed(r, cols, "w4"); return ok })
	w4, _ := paneNamed(below, cols, "w4")
	if w4.x != w3.x || w4.y <= w3.y {
		t.Errorf("w4 is at (%d,%d) and w3 at (%d,%d): ⌃B should open below, in the same column.\n%s", w4.x, w4.y, w3.x, w3.y, s.dump())
	}

	// esc closes it and nothing else: no pane opens, the keys stay where they were.
	s.send("\x0b")
	s.await("jump to agent")
	s.send("\x1b")
	s.awaitGone("jump to agent")
}
