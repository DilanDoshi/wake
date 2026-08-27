//go:build unix

// The awareness strip, on a real screen.
//
// internal/ui holds what it says; this is the half that matters to a person:
// is it there in every view, including the one you disappear into for an hour.

package main

import (
	"strings"
	"testing"
)

// stripRow is the strip as drawn: the row above the notice row.
func stripRow(s *screen) string {
	lines := s.lines()
	if len(lines) < 2 {
		return ""
	}
	return lines[len(lines)-2]
}

// The strip is on screen in the room, in a conversation, and at a width where
// both sidebars are gone.
//
// Spec §8's "in every view including solo" is the whole point: the roster
// answers the same question and is the first thing a narrow window drops.
func TestTheStripIsInEveryView(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()
	if got := stripRow(s); !strings.Contains(got, "idle") && !strings.Contains(got, "working") {
		t.Fatalf("no strip in the conversation view: %q\n%s", got, s.dump())
	}

	s.send("\x17") // ⌃W: the room
	s.await("group chat")
	s.settle()
	if got := stripRow(s); !strings.Contains(got, "idle") && !strings.Contains(got, "working") {
		t.Fatalf("no strip in the room: %q\n%s", got, s.dump())
	}

	// Below the width where the sidebars exist at all, which is the view the
	// strip is for.
	s.resize(90, 20)
	s.settle()
	if got := stripRow(s); !strings.Contains(got, "idle") && !strings.Contains(got, "working") {
		t.Fatalf("no strip at 90 columns: %q\n%s", got, s.dump())
	}
}

// It counts what the fleet is doing, and it moves.
func TestTheStripCountsTheFleetAndFollowsIt(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 120, 30)
	s.await("ready")
	s.send("\x17") // ⌃W
	s.await("group chat")

	s.send("/new robin\r")
	s.await("@robin started")
	s.send("\x17")
	s.await("group chat")
	// Three, not two: the room seats a manager, and the strip counts the fleet
	// rather than the agents somebody made. That is the count an operator reads
	// to answer "what is my fleet doing", and the service is part of it - it
	// holds a process, it costs money, and it can be the one that is working.
	s.await("3 idle")

	// Parking one moves it out of idle and into its own count. The cursor is on
	// robin, where /new left it; nothing moves it, because ↑ wraps and the row
	// it would wrap onto is the manager.
	s.send("\x03") // ⌃C
	s.await("1 parked")
	s.settle()
	if got := stripRow(s); !strings.Contains(got, "2 idle") {
		t.Fatalf("parking one of three left the strip reading %q\n%s", got, s.dump())
	}
}

// A blocked agent is what the strip leads with.
func TestTheStripLeadsWithAnAgentThatNeedsYou(t *testing.T) {
	withScriptedAgent(t, scriptAsks)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 120, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("@" + name + " write the file\r")
	s.await("1 need you")

	got := stripRow(s)
	if need, idle := strings.Index(got, "need you"), strings.Index(got, "idle"); idle >= 0 && need > idle {
		t.Errorf("the strip reads %q: what needs you goes first", got)
	}
}

// The strip names the workspace of the conversation you are in.
func TestTheStripNamesTheWorkspaceYouAreIn(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 120, 30)
	s.await("ready")
	s.settle()

	// The agent runs where the test binary runs, so the workspace is this
	// worktree's own directory name.
	if got := stripRow(s); !strings.Contains(got, "#") {
		t.Fatalf("a conversation's strip names no workspace: %q\n%s", got, s.dump())
	}

	s.send("\x17") // ⌃W: the room is not one workspace
	s.await("group chat")
	s.settle()
	if got := stripRow(s); strings.Contains(got, "#") {
		t.Fatalf("the room's strip names a workspace: %q\n%s", got, s.dump())
	}
}
