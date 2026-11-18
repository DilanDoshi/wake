package ui

// A `!cmd` the room sent draws as its message, with what it printed folded under
// it: peerFoldRows rows and a pointer, opened by ⌃E or a click, on the line it
// was sent on and again when a reopened room reads it back off the transcript.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// printRows is a made-up command printing n rows, out-00 first.
func printRows(n int) string {
	nums := make([]string, n)
	for i := range nums {
		nums[i] = fmt.Sprint(i)
	}
	return "printf 'out-%02d\\n' " + strings.Join(nums, " ")
}

// roomBanged is a room that sent `@alex !cmd`, with the command run.
func roomBanged(t *testing.T, cmd string) App {
	t.Helper()
	a := newRoomApp(t).withSize(200, 40).withAgents("alex", "john")
	return loopEnter(a.withDraft("@alex !" + cmd))
}

func bangRoomText(a App) string { return roomShown(a.room, 120, 40) }

func TestARoomBangFoldsItsOutputUnderTheMessage(t *testing.T) {
	room := bangRoomText(roomBanged(t, printRows(6)))
	for _, want := range []string{"@alex !printf", "out-00", "out-02", "+3 lines", expandKey + " or click to expand"} {
		if !strings.Contains(room, want) {
			t.Errorf("the room does not show %q:\n%s", want, room)
		}
	}
	if strings.Contains(room, "out-03") {
		t.Errorf("a six-row output is not folded to %d rows:\n%s", peerFoldRows, room)
	}
}

func TestARoomBangHeadsItsOutputOnRowsOfTheirOwn(t *testing.T) {
	room := bangRoomText(roomBanged(t, printRows(6)))
	for _, row := range strings.Split(room, "\n") {
		if strings.Contains(row, "@alex !printf") && strings.Contains(row, "out-00") {
			t.Errorf("the message and its output run together on one row: %q", row)
		}
	}
}

func TestARoomBangOfThreeRowsDrawsWhole(t *testing.T) {
	room := bangRoomText(roomBanged(t, printRows(peerFoldRows)))
	if !strings.Contains(room, fmt.Sprintf("out-%02d", peerFoldRows-1)) || strings.Contains(room, "to expand") {
		t.Errorf("a %d-row output is not drawn whole and unfolded:\n%s", peerFoldRows, room)
	}
}

// One row past the fold is the first that folds, and it can be clicked open.
func TestARoomBangOneRowPastTheFoldFoldsAndIsAClickTarget(t *testing.T) {
	a := roomBanged(t, printRows(peerFoldRows+1))
	if room := bangRoomText(a); !strings.Contains(room, "+1 line ·") || strings.Contains(room, fmt.Sprintf("out-%02d", peerFoldRows)) {
		t.Errorf("a %d-row output is not folded to %d rows:\n%s", peerFoldRows+1, peerFoldRows, room)
	}
	line, _ := expandableLine(t, a.room)
	opened, hit := a.room.toggleLine(line)
	if got := roomShown(opened, 120, 40); !hit || !strings.Contains(got, fmt.Sprintf("out-%02d", peerFoldRows)) {
		t.Errorf("a click did not open the fold (hit %v):\n%s", hit, got)
	}
}

func TestARoomBangThatPrintedNothingDrawsOnlyItsMessage(t *testing.T) {
	room := bangRoomText(roomBanged(t, "true"))
	if !strings.Contains(room, "@alex !true") || strings.Contains(room, "to expand") {
		t.Errorf("an empty output drew a fold or lost the message:\n%s", room)
	}
}

func TestCtrlEOpensARoomBangsOutputAndAgainFoldsIt(t *testing.T) {
	a, _ := pressKey(roomBanged(t, printRows(6)), tea.KeyMsg{Type: tea.KeyCtrlE})
	if room := bangRoomText(a); !strings.Contains(room, "out-05") || strings.Contains(room, "to expand") {
		t.Errorf("⌃E did not open the fold:\n%s", room)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlE})
	if room := bangRoomText(a); strings.Contains(room, "out-05") {
		t.Errorf("a second ⌃E did not fold it back:\n%s", room)
	}
}

func TestAClickOpensARoomBangsOutputAndLeavesAnotherFolded(t *testing.T) {
	a := roomBanged(t, printRows(6))
	a = loopEnter(a.withDraft("@john !printf 'other-%02d\\n' 0 1 2 3 4 5"))
	line, _ := expandableLine(t, a.room)
	room, hit := a.room.toggleLine(line)
	if !hit {
		t.Fatal("a click on a folded room bang reported no hit")
	}
	got := roomShown(room, 120, 40)
	if !strings.Contains(got, "out-05") || strings.Contains(got, "other-05") {
		t.Errorf("a click opened the wrong fold (or none):\n%s", got)
	}
}

func TestAShortRoomBangIsNoClickTarget(t *testing.T) {
	a := roomBanged(t, printRows(2))
	for _, l := range a.room.said.slice(a.room.said.first(), a.room.said.len()) {
		if roomCollapsible(l.ev, a.room.blockWidth()) {
			t.Errorf("a two-row bang folds: %q", l.ev.Text)
		}
	}
}

// What a failed, cut or timed-out command says about itself is never folded away.
func TestAFoldedRoomBangStillSaysHowItEnded(t *testing.T) {
	room := bangRoomText(roomBanged(t, printRows(6)+"; exit 3"))
	if !strings.Contains(room, fmt.Sprintf(bangExited, 3)) {
		t.Errorf("the folded bang hides its exit status:\n%s", room)
	}
	if strings.Contains(room, "out-05") || !strings.Contains(room, "+3 lines") {
		t.Errorf("a failing bang is not folded like any other:\n%s", room)
	}
}

func TestAFailingRoomBangWithNoOutputShowsItsStatus(t *testing.T) {
	room := bangRoomText(roomBanged(t, "exit 4"))
	if !strings.Contains(room, fmt.Sprintf(bangExited, 4)) {
		t.Errorf("the bang's exit status is missing:\n%s", room)
	}
}

// A bare room bang is addressed to nobody, and draws the same way.
func TestABareRoomBangFoldsToo(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("alex")
	room := bangRoomText(loopEnter(a.withDraft("!" + printRows(6))))
	if !strings.Contains(room, "!printf") || strings.Contains(room, "out-03") || !strings.Contains(room, "+3 lines") {
		t.Errorf("a bare room bang is not folded:\n%s", room)
	}
}

// The conversation keeps drawing the whole block, as before.
func TestTheConversationStillDrawsAllOfARoomBangsOutput(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle},
	).openDMWith("s1", "alex").opened(t).refocus("")
	a = loopEnter(a.withDraft("@alex !" + printRows(6)))
	dm := dmText(a, "s1")
	if !strings.Contains(dm, "out-05") || strings.Contains(dm, "to expand") {
		t.Errorf("the conversation no longer draws the whole output:\n%s", dm)
	}
}

// Whether it folds is read off the rows drawn at this width, as a peer message's
// is: one long row that wraps is a tall output.
func TestARoomBangFoldsOnTheRowsItWrapsTo(t *testing.T) {
	cmd := "printf '" + strings.Repeat("word ", 40) + "\\n'"
	wide := newRoomApp(t).withSize(300, 40).withAgents("alex")
	if room := bangRoomText(loopEnter(wide.withDraft("@alex !" + cmd))); strings.Contains(room, "to expand") {
		t.Errorf("a one-row output folded in a wide room:\n%s", room)
	}
	narrow := loopEnter(newRoomApp(t).withSize(300, 40).withAgents("alex").withDraft("@alex !" + cmd))
	narrow.room = narrow.room.SetSize(40, 40)
	if room := roomShown(narrow.room, 40, 40); !strings.Contains(room, "to expand") {
		t.Errorf("an output wrapped past %d rows did not fold in a narrow room:\n%s", peerFoldRows, room)
	}
}

// What a command prints cannot act on the terminal, however it was read.
func TestARoomBangsOutputReachesNoTerminalControl(t *testing.T) {
	room := roomBanged(t, `printf 'a\tb\n\033[31mred\033[0m\n'`).room.View(120, 40)
	if strings.ContainsRune(room, '\t') || strings.Contains(room, "\x1b[31m") {
		t.Errorf("a tab or a command's own colour reached the frame:\n%q", room)
	}
}

func TestARoomBangExpandsATabToItsColumn(t *testing.T) {
	room := bangRoomText(roomBanged(t, `printf 'abc\td\n'`))
	if !strings.Contains(room, "abc d") {
		t.Errorf("the tab did not expand to its stop:\n%s", room)
	}
}

// --- a reopened room --------------------------------------------------------

// recordOf is a `!cmd`'s record as the transcript reads back, stamped as a room
// send aimed at one agent, or as a conversation's.
func recordOf(id, cmd, out string, at time.Time, aimed bool) core.Event {
	return core.Event{
		Kind: core.KindUserText, SessionID: id, At: at, Echoed: true, Shell: true,
		MessageID: newShellSend(aimed), Text: core.ShellBlock(cmd, out),
	}
}

func TestAReopenedRoomDrawsTheBangItSentFolded(t *testing.T) {
	r := restored([]core.Event{recordOf("s1", "ls -a", "out-00\nout-01\nout-02\nout-03\nout-04\nout-05", base, true)})
	room := roomShown(r, 100, 24)
	for _, want := range []string{"@agent-s1 !ls -a", "out-00", "out-02", "+3 lines"} {
		if !strings.Contains(room, want) {
			t.Errorf("the reopened room does not show %q:\n%s", want, room)
		}
	}
	if strings.Contains(room, "out-03") || strings.Contains(room, "```") {
		t.Errorf("the reopened bang is unfolded or shows its fence:\n%s", room)
	}
	r = r.toggleExpandAll()
	if !strings.Contains(roomShown(r, 100, 24), "out-05") {
		t.Error("⌃E did not open the reopened fold")
	}
}

func TestAReopenedBangKeepsHowItEnded(t *testing.T) {
	out := "out-00\nout-01\nout-02\nout-03\nout-04\n" + fmt.Sprintf(bangExited, 2)
	room := roomShown(restored([]core.Event{recordOf("s1", "false", out, base, true)}), 100, 24)
	if !strings.Contains(room, fmt.Sprintf(bangExited, 2)) || strings.Contains(room, "out-04") {
		t.Errorf("the reopened fold hides its status or is not folded:\n%s", room)
	}
}

// A bang typed into a conversation is that conversation's alone.
func TestAReopenedRoomDoesNotDrawABangTypedInAConversation(t *testing.T) {
	r := restored([]core.Event{recordOf("s1", "ls -a", "out-00", base, false)})
	if room := roomShown(r, 100, 24); strings.Contains(room, "out-00") || strings.Contains(room, "ls -a") {
		t.Errorf("a conversation's bang came back in the room:\n%s", room)
	}
}

// Two transcripts holding one record (a fork's copy) are one line.
func TestAReopenedBangInTwoTranscriptsIsDrawnOnce(t *testing.T) {
	rec := recordOf("s1", "ls -a", "out-00", base, true)
	copied := rec
	copied.SessionID = "s2"
	room := roomShown(restored([]core.Event{rec}, []core.Event{copied}), 100, 24)
	if n := strings.Count(room, "!ls -a"); n != 1 {
		t.Errorf("the bang is drawn %d times, want once:\n%s", n, room)
	}
}

// A window that drew the bang live is not asked for the transcript it was
// written to: the session already has a line there.
func TestARoomThatDrewABangLiveDoesNotAskForItAgain(t *testing.T) {
	a := roomBanged(t, "echo once-only")
	a = a.askRoomHistory(idOfAgentNamed(t, a, "alex"))
	if len(a.roomAsk.pending) != 0 {
		t.Errorf("the room asked for the history of a session it already drew a line for: %v", a.roomAsk.pending)
	}
	n := 0
	for _, row := range strings.Split(bangRoomText(a), "\n") {
		if strings.TrimSpace(row) == "once-only" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the output is drawn %d times, want once", n)
	}
}

// The record is a file: a control character in its output is neutralised where
// the transcript is read, and never reaches the frame.
func TestAReopenedBangsOutputReachesNoTerminalControl(t *testing.T) {
	text := "<bash-input>ls</bash-input><bash-stdout>a\tb\n\x1b[31mred\x1b[0m</bash-stdout><bash-stderr></bash-stderr>"
	line, err := json.Marshal(map[string]any{
		"type": "user", "uuid": newShellSend(true), "timestamp": base.Format(time.RFC3339Nano),
		"message": map[string]any{"role": "user", "content": text},
	})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := core.DecodeTranscriptLine(line)
	if err != nil || len(evs) != 1 || !evs[0].Shell {
		t.Fatalf("decoded %d events (err %v), want one shell record", len(evs), err)
	}
	evs[0].SessionID = "s1"
	room := restored(evs).View(100, 24)
	if !strings.Contains(stripANSI(room), "!ls") {
		t.Fatalf("the reopened room does not draw the record at all:\n%s", room)
	}
	if strings.Contains(room, "\x1b[31m") || strings.ContainsRune(room, '\t') {
		t.Errorf("a command's own colour or a tab reached the frame:\n%q", room)
	}
}
