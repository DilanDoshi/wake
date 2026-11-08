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

// The answer is the window's rows, never chrome, so a token leaves the stored
// layout as drawn and the frames between changes re-lay nothing: View compares the
// stored chrome with the drawn one, and a stale one re-lays a copy of the pane
// every frame. That holds for a working agent, one scrolled back over it, and one
// still reading idle under its done line when the next answer starts - the line
// the first token hides.
func TestAStreamedTokenLeavesTheStoredLayoutAsDrawn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		scrolled bool
		agent    Agent
	}{
		{"working", false, Agent{ID: "s1", State: rpc.StateWorking}},
		{"scrolled back", true, Agent{ID: "s1", State: rpc.StateWorking}},
		{"idle under its done line", false, Agent{ID: "s1", State: rpc.StateIdle, doneAt: clock()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := fullTranscriptDM()
			d.Agent = tc.agent
			d = messageStarted(d.SetSize(benchPaneWidth, 40))
			if tc.scrolled {
				d = d.ScrollUp(9)
			}
			for i, tok := range streamedBlock(400) {
				d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: tok})
				if got := d.chromeHeight(); got != d.chrome {
					t.Fatalf("after token %d the pane draws %d rows of chrome and stored %d: every frame until the next settle re-lays it", i, got, d.chrome)
				}
			}
			d = d.Append(core.Event{Kind: core.KindTurnEnd, SessionID: "s1"})
			if got := d.chromeHeight(); got != d.chrome {
				t.Errorf("after the turn ended the pane draws %d rows of chrome and stored %d", got, d.chrome)
			}
		})
	}
}

// answerMarker names each paragraph of a streamed answer, as turnMarker names
// the history above it.
var answerMarker = regexp.MustCompile(`ANS\d{4}`)

// answerRows is the row each answer marker is drawn on.
func answerRows(rows []string) map[string]int {
	at := map[string]int{}
	for i, r := range rows {
		if m := answerMarker.FindString(ansi.Strip(r)); m != "" {
			at[m] = i
		}
	}
	return at
}

// streamingDM is alex's conversation open beside the room over turns of history,
// alex working, and a message begun.
func streamingDM(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withSize(200, 50).withAgents("alex").openDMWith("s1", "alex")
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateWorking}}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	for i := range 100 {
		a = a.applyFrame(kindFrame("s1", core.KindAssistantText, fmt.Sprintf("Turn %03d. %s", i, benchParagraph)))
	}
	return a.applyFrame(kindFrame("s1", core.KindMessageStart, ""))
}

// answerTokens is an answer of paras paragraphs, a word a token, each paragraph
// opening on its marker.
func answerTokens(paras int) []string {
	var toks []string
	for p := range paras {
		for i, w := range strings.Fields(fmt.Sprintf("ANS%04d %s", p, benchParagraph)) {
			if i == 0 && p > 0 {
				w = "\n\n" + w
			}
			toks = append(toks, w+" ")
		}
	}
	return toks
}

// streams delivers toks to s1 the way the inbox hands tokens over, one a batch.
func streams(a App, toks []string) App {
	for _, tok := range toks {
		m, _ := a.Update(streamMsg{gen: a.gen, batch: batch{frames: []rpc.Frame{tokenFrame("s1", tok)}}})
		a = m.(App)
	}
	return a
}

func dmRows(a App) []string { return paneRowsOf(a, "s1") }

// dmWheel is one notch over the conversation's pane.
func dmWheel(t *testing.T, a App, button tea.MouseButton) App {
	t.Helper()
	for x := range a.layout.Width {
		if region, col := a.layout.Hit(a.frameRegions(a.regions()), x); region == RegionPane {
			if id, _, _, ok := a.paneAt(col, 5); ok && id == "s1" {
				m, _ := a.Update(tea.MouseMsg{X: x, Y: 5, Button: button, Action: tea.MouseActionPress})
				return m.(App)
			}
		}
	}
	t.Fatal("the conversation is not on screen")
	return a
}

// assertAnswerMovedBy is assertMovedBy over the answer's markers and the history's.
func assertAnswerMovedBy(t *testing.T, what string, before, after []string, want int) {
	t.Helper()
	moved := map[int]int{}
	for _, at := range []func([]string) map[string]int{markerRows, answerRows} {
		b, a := at(before), at(after)
		for m, i := range b {
			if j, ok := a[m]; ok {
				moved[j-i]++
			}
		}
	}
	if len(moved) != 1 || moved[want] < 2 {
		t.Errorf("%s: markers moved by %v rows (row delta: count), want every one by %d\nbefore:\n%s\nafter:\n%s",
			what, moved, want, strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

// The streamed answer is the newest part of the conversation, so a notch moves
// it and everything above it by the notch, either way - where the preview used to
// fall to a three-row box on the first notch and swap the pane's content for the
// history above it.
func TestAWheelNotchDuringAStreamMovesEveryRowByTheNotch(t *testing.T) {
	a := streams(streamingDM(t), answerTokens(14))
	if n := len(answerRows(dmRows(a))); n < 2 {
		t.Fatalf("%d of the answer's paragraphs are on screen: this measures nothing about it\n%s", n, strings.Join(dmRows(a), "\n"))
	}
	for i, step := range []struct {
		button tea.MouseButton
		want   int
	}{
		{tea.MouseButtonWheelUp, wheelLines}, {tea.MouseButtonWheelUp, wheelLines}, {tea.MouseButtonWheelUp, wheelLines},
		{tea.MouseButtonWheelDown, -wheelLines}, {tea.MouseButtonWheelDown, -wheelLines}, {tea.MouseButtonWheelDown, -wheelLines},
	} {
		before := dmRows(a)
		a = dmWheel(t, a, step.button)
		assertAnswerMovedBy(t, fmt.Sprintf("notch %d", i), before, dmRows(a), step.want)
	}
}

// A reader scrolled back into the answer reads it standing still: the tokens
// after it, the paragraphs that finish under it and the block landing move none
// of the rows on screen.
func TestNothingAReaderScrolledIntoTheAnswerReadsMoves(t *testing.T) {
	toks := answerTokens(30)
	a := streams(streamingDM(t), toks[:400])
	a = dmWheel(t, dmWheel(t, a, tea.MouseButtonWheelUp), tea.MouseButtonWheelUp)
	reading := dmRows(a)
	if len(answerRows(reading)) < 2 {
		t.Fatalf("the reader is not in the answer:\n%s", strings.Join(reading, "\n"))
	}
	a = streams(a, toks[400:])
	assertAnswerMovedBy(t, "the rest of the answer arriving", reading, dmRows(a), 0)

	a = a.applyFrame(kindFrame("s1", core.KindAssistantText, strings.Join(toks, "")))
	assertAnswerMovedBy(t, "the answer landing", reading, dmRows(a), 0)
}

// The way back down passes through every row of the answer the reader went up
// past, one notch at a time, and ends following the newest line.
func TestTheWayBackDownPassesThroughEveryRowOfTheAnswer(t *testing.T) {
	toks := answerTokens(40)
	a := streams(streamingDM(t), toks[:500])
	seen := map[string]bool{}
	for range 20 {
		a = dmWheel(t, a, tea.MouseButtonWheelUp)
		for m := range answerRows(dmRows(a)) {
			seen[m] = true
		}
	}
	a = streams(a, toks[500:])
	for i := 0; !a.dms["s1"].tr.atBottom(); i++ {
		if i > 200 {
			t.Fatal("the reader never reached the newest line")
		}
		before := dmRows(a)
		a = dmWheel(t, a, tea.MouseButtonWheelDown)
		for m := range answerRows(dmRows(a)) {
			delete(seen, m)
		}
		if !a.dms["s1"].tr.atBottom() {
			assertAnswerMovedBy(t, fmt.Sprintf("notch %d down", i), before, dmRows(a), -wheelLines)
		}
	}
	if len(seen) > 0 {
		t.Errorf("the way down skipped answer rows seen on the way up: %v", seen)
	}
	last := fmt.Sprintf("ANS%04d", 39)
	if _, ok := answerRows(dmRows(a))[last]; !ok {
		t.Errorf("at the bottom the answer's newest paragraph %s is not on screen:\n%s", last, strings.Join(dmRows(a), "\n"))
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
