package ui

// A wheel notch moves what is on screen by the notch, and a streamed token
// leaves no layout for the next frame to redo.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// turnMarker names each history turn, so a row can be followed across frames.
var turnMarker = regexp.MustCompile(`Turn \d{3}`)

// markerRows is the row each turn marker is drawn on in a pane's rows.
func markerRows(rows []string) map[string]int {
	at := map[string]int{}
	for i, r := range rows {
		if m := turnMarker.FindString(ansi.Strip(r)); m != "" {
			at[m] = i
		}
	}
	return at
}

// assertMovedBy fails unless every marker drawn in both frames moved by want rows.
func assertMovedBy(t *testing.T, what string, before, after []string, want int) {
	t.Helper()
	b, a := markerRows(before), markerRows(after)
	moved := map[int]int{}
	for m, i := range b {
		if j, ok := a[m]; ok {
			moved[j-i]++
		}
	}
	if len(moved) != 1 || moved[want] < 2 {
		t.Errorf("%s: turn markers moved by %v rows (row delta: count), want every one by %d\nbefore:\n%s\nafter:\n%s",
			what, moved, want, strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

// roomOverHistory is a room over turns of alex's history with alex working, so the
// room draws its working line.
func roomOverHistory(t *testing.T, turns int) App {
	t.Helper()
	a := newRoomApp(t).withSize(160, 45).withAgents("alex")
	for i := range turns {
		a = a.applyFrame(kindFrame("s1", core.KindAssistantText, fmt.Sprintf("Turn %03d. %s", i, benchParagraph)))
	}
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateWorking}}}
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
}

func roomRows(a App) []string {
	w, h, _ := a.paneSize("")
	return strings.Split(a.roomPane(w, h), "\n")
}

func roomWheel(a App, button tea.MouseButton) App {
	m, _ := a.Update(tea.MouseMsg{X: 2, Y: 3, Button: button, Action: tea.MouseActionPress})
	return m.(App)
}

// The working line is chrome the room gains for the draw. A notch measured on
// a stored room without it moved the room five rows rather than three, leaving
// the newest line and coming back to it.
func TestARoomNotchWithAnAgentWorkingMovesTheRoomByTheNotch(t *testing.T) {
	a := roomOverHistory(t, 60)
	following := roomRows(a)
	if !strings.Contains(strings.Join(following, "\n"), agentPrefix+"alex ") {
		t.Fatalf("the room draws no working line for alex, so this measures nothing:\n%s", strings.Join(following, "\n"))
	}

	a = roomWheel(a, tea.MouseButtonWheelUp)
	back := roomRows(a)
	assertMovedBy(t, "the first notch up", following, back, wheelLines)

	a = roomWheel(a, tea.MouseButtonWheelDown)
	assertMovedBy(t, "the notch back down", back, roomRows(a), -wheelLines)
}

// The working line ending under a reader scrolled back leaves what they read
// where it was, and the stored layout follows it.
func TestTheWorkingLineEndingLeavesAScrolledReaderWhereTheyWere(t *testing.T) {
	a := roomWheel(roomWheel(roomOverHistory(t, 60), tea.MouseButtonWheelUp), tea.MouseButtonWheelUp)
	reading := roomRows(a)
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	assertMovedBy(t, "the working line ending", reading, roomRows(a), 0)

	// Back to the newest line (the last notch is clamped), then a notch up and
	// down from there: a stored room still holding the ended line sat two rows
	// past the bottom drawn, so that notch up moved one row.
	for range 3 {
		a = roomWheel(a, tea.MouseButtonWheelDown)
	}
	for _, step := range []struct {
		button tea.MouseButton
		want   int
	}{{tea.MouseButtonWheelUp, wheelLines}, {tea.MouseButtonWheelDown, -wheelLines}} {
		before := roomRows(a)
		a = roomWheel(a, step.button)
		assertMovedBy(t, fmt.Sprintf("a notch from the newest line after it ended (%d)", step.want), before, roomRows(a), step.want)
	}
}

// A token that grows the preview a row settles the stored layout then, so the
// frames between row changes re-lay nothing: View compares the stored chrome
// with the drawn one, and a stale one re-laid a copy of the pane every frame
// for as long as the answer streamed.
func TestAStreamedTokenLeavesTheStoredLayoutAsDrawn(t *testing.T) {
	for _, scrolled := range []bool{false, true} {
		t.Run(fmt.Sprintf("scrolled=%v", scrolled), func(t *testing.T) {
			d := messageStarted(fullTranscriptDM())
			if scrolled {
				d = d.ScrollUp(9)
			}
			grew := 0
			for i, tok := range streamedBlock(400) {
				rows := d.partial.rows()
				d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: tok})
				if d.partial.rows() != rows {
					grew++
				}
				if got := d.chromeHeight(); got != d.chrome {
					t.Fatalf("after token %d the pane draws %d rows of chrome and stored %d: every frame until the next settle re-lays it", i, got, d.chrome)
				}
			}
			if grew < 2 {
				t.Fatalf("the preview changed rows %d times: this streamed nothing worth settling", grew)
			}
		})
	}
}

// A channel whose members are idle while another team works draws no working
// line, and picking it takes the line off the stored room too, or the first
// notch there moved the room one row.
func TestTheFirstNotchInAChannelWithNobodyWorkingMovesByTheNotch(t *testing.T) {
	sessions := channelSessions()
	sessions[0].State = rpc.StateWorking // sydney, on backend
	a := newRoomApp(t).withSize(160, 45).applyStatus(channelReport(sessions...))
	for i := range 60 {
		a = a.applyFrame(kindFrame("s3", core.KindAssistantText, fmt.Sprintf("Turn %03d. %s", i, benchParagraph)))
	}
	working := agentPrefix + "sydney "
	if !strings.Contains(strings.Join(roomRows(a), "\n"), working) {
		t.Fatalf("the room draws no working line for sydney, so this measures nothing:\n%s", strings.Join(roomRows(a), "\n"))
	}
	a = a.selectChannel("frontend")
	before := roomRows(a)
	if strings.Contains(strings.Join(before, "\n"), working) {
		t.Fatalf("frontend's channel still draws sydney working:\n%s", strings.Join(before, "\n"))
	}
	a = roomWheel(a, tea.MouseButtonWheelUp)
	assertMovedBy(t, "the first notch in frontend's channel", before, roomRows(a), wheelLines)
}
