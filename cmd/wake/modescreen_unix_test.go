//go:build unix

package main

import (
	"strings"
	"testing"
)

// docs/notes/bugs.md BUG-1, at the surface it was reported on.
//
// The owner cycled the permission mode in a conversation pane and nothing on
// screen moved: the mode's only home was the legend's tail, which is the first
// entry a narrow pane cuts, and the whole legend is withheld from a blurred
// composer. Neither is a broken mechanism - the frame was written and the
// receipt arrived - so nothing but a rendered screen could see it.
//
// Wide enough that the bar is not truncated: it is one right-cut line and the
// mode is its last segment, so a pane too narrow for the whole bar loses the
// mode the way the legend does. That is the ordering working - the path is what
// this line is most read for - and it is why this test is not run at 80.
func TestAConversationsBarNamesItsPermissionMode(t *testing.T) {
	withScriptedAgent(t, scriptModes)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 260, 40)
	s.await("ready")

	// auto is what this build spawns every session in, and it is on screen
	// before the agent has reported anything about itself.
	s.await("permissions: auto")
	if row := s.rowOf("permissions: auto"); row < 0 || !strings.Contains(s.lines()[row], "~/") {
		t.Fatalf("the mode is not on the bar under the directory.\n%s", s.dump())
	}

	// ⇧⇥, and the label moves - on the receipt, which is why this test needs an
	// agent that answers one.
	s.send("\x1b[Z")
	s.await("permissions: default")
}
