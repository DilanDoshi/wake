package ui

// The room's working line: its own minimal `✻ Sailed for 1m 26s` on the surface
// an operator actually sits on, where the DM keeps Claude's fuller
// `✻ Sprouting… (1m 26s · ↓ 5.5k tokens)`.
//
// The working line has existed since PR #15 but was only ever on DM.View, so the
// room - where somebody supervising a fleet spends their time - said nothing at
// all while three agents worked. The past-tense word and the dropped token
// clause are roomwords.go's.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// working is an agent mid-turn, started ago and having produced tokens.
func working(id, name string, ago time.Duration, tokens int) Agent {
	return Agent{
		ID:         id,
		Name:       name,
		State:      rpc.StateWorking,
		startedAt:  clock().Add(-ago),
		TurnTokens: tokens,
	}
}

// One agent working reads as the room's minimal line, plus the name - a group
// chat has to say whose turn it is - and it drops the token count the DM keeps.
func TestTheRoomsWorkingLineNamesTheAgentAndTheAge(t *testing.T) {
	line := stripANSI(roomWorkingLine([]Agent{
		working("s1", "noah", 86*time.Second, 5_500),
		{ID: "s2", Name: "robin", State: rpc.StateIdle},
	}, 120))

	for _, want := range []string{"noah", "for 1m 26s"} {
		if !strings.Contains(line, want) {
			t.Errorf("the room's working line is %q, want it to carry %q", line, want)
		}
	}
	if strings.Contains(line, "tokens") {
		t.Errorf("the room's working line is %q, want it to drop the token count", line)
	}
}

// Every figure on the row belongs to one named agent, and the rest are a
// count: a fleet-summed token total beside a longest-of-several age would be
// two agents' numbers in one sentence.
func TestTheRoomsWorkingLineNamesTheOldestTurnAndCountsTheRest(t *testing.T) {
	line := stripANSI(roomWorkingLine([]Agent{
		working("s1", "noah", 12*time.Second, 900),
		working("s2", "robin", 124*time.Second, 5_500),
		working("s3", "maya", 40*time.Second, 2_000),
	}, 160))

	if !strings.Contains(line, "robin") {
		t.Errorf("the line is %q, want it to name robin - the oldest running turn", line)
	}
	if strings.Contains(line, "noah") || strings.Contains(line, "maya") {
		t.Errorf("the line is %q, want the other two counted rather than named", line)
	}
	if !strings.Contains(line, "2 more working") {
		t.Errorf("the line is %q, want it to say how many others are working", line)
	}
}

// A fleet with nobody working says nothing, and the row is not reserved.
func TestTheRoomDrawsNoWorkingLineWhenNobodyIsWorking(t *testing.T) {
	if line := roomWorkingLine([]Agent{{ID: "s1", Name: "noah", State: rpc.StateIdle}}, 120); line != "" {
		t.Errorf("the room drew %q with nobody working", line)
	}
}

// One row or none, at every fleet size. A row per agent is thirty rows taken
// from the transcript, and a block of rows that comes and goes changes the
// pane's height at an arbitrary moment - which is the alt-screen bug DM.chrome
// exists for.
func TestTheRoomsWorkingLineIsNeverMoreThanOneRow(t *testing.T) {
	agents := make([]Agent, 0, 30)
	for i := range 30 {
		agents = append(agents, working("s"+string(rune('a'+i)), "agent-"+string(rune('a'+i)), time.Duration(i)*time.Second, i*100))
	}
	for _, width := range []int{20, 40, 80, 120, 200} {
		line := roomWorkingLine(agents, width)
		if strings.Contains(line, "\n") {
			t.Errorf("at width %d the room's working line is more than one row:\n%s", width, line)
		}
		if got := ansi.StringWidth(stripANSI(line)); got > width {
			t.Errorf("at width %d the room's working line is %d columns wide", width, got)
		}
	}
}

// And it is drawn, between the transcript and the composer, where Claude Code
// puts it.
func TestTheRoomDrawsItsWorkingLine(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "noah", State: rpc.StateWorking},
		}},
	})

	// Asserted on the "for 0s" age rather than on the name: at 200 columns the
	// roster already draws "noah", so a Contains over the name is satisfied with
	// this whole feature deleted.
	if view := shown(a); !strings.Contains(view, "for 0s") {
		t.Errorf("the room says nothing about the turn while an agent works:\n%s", view)
	}
}

// The whole frame, at every height, once an agent starts working.
//
// The room's row appears on a *status push* rather than on a resize, so this is
// the case a View guarded on width and height alone cannot see - and a frame
// one row taller than the terminal scrolls the alt screen away on every draw.
func TestAWorkingAgentDoesNotMakeTheFrameTallerThanTheTerminal(t *testing.T) {
	fresh(t)
	push := rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "noah", State: rpc.StateWorking},
			{ID: "s2", Name: "robin", State: rpc.StateWorking},
		}},
	}
	floor := newRoomApp(t).withSize(80, 24).room.minHeight() + noticeHeight + stripHeight
	for _, height := range []int{floor, floor + 1, 14, 24, 40} {
		a := newRoomApp(t).withSize(80, height).applyFrame(push)
		if got := lipgloss.Height(a.View()); got != height {
			t.Errorf("a working agent in a %d-row terminal drew %d rows:\n%s", height, got, shown(a))
		}
	}
}

// The row is chrome the transcript has to account for. A pane drawn one row
// taller than it was given scrolls the alt screen away on every frame, and
// this row appears the moment an agent starts a turn - which is not a resize.
func TestTheRoomStaysInBoundsWhenItsWorkingLineAppears(t *testing.T) {
	r := NewRoom().SetSize(80, 12)
	before := strings.Count(r.View(80, 12), "\n")

	r = r.WithWorking([]Agent{working("s1", "noah", time.Second, 100)})
	if got := strings.Count(r.View(80, 12), "\n"); got != before {
		t.Errorf("the room drew %d newlines with a working line against %d without: it is taller than the pane it was given", got, before)
	}
}
