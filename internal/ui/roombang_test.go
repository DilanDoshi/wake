package ui

// A `!cmd` the room sent draws as its message, with what it printed folded under
// it: peerFoldRows rows and a pointer, opened by ⌃E or a click, on the line it
// was sent on and again when a reopened room reads it back off the transcript.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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

// The message is the operator's own shaded row and what the command printed is the
// fold's Muted body: the two read apart, as a peer's message does from a reply.
func TestARoomBangDrawsItsOutputMutedAndItsMessageNot(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor, or there is no styling to tell apart
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	muted := strings.SplitN(HintStyle.Render("x"), "x", 2)[0]

	var head, out string
	for _, row := range strings.Split(roomBanged(t, printRows(6)).room.View(120, 40), "\n") {
		switch plain := ansi.Strip(row); {
		case strings.Contains(plain, "@alex !printf"):
			head = row
		case strings.Contains(plain, "out-00"):
			out = row
		}
	}
	if head == "" || out == "" {
		t.Fatalf("the room drew no message row (%q) or output row (%q)", head, out)
	}
	if strings.Contains(head, muted) || !strings.Contains(out, muted) {
		t.Errorf("the message is muted or the output is not:\nmessage %q\noutput  %q", head, out)
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

// What a failed, cut or timed-out command says about itself is never folded away,
// and it is drawn under the pointer, after what was kept of the output.
func TestAFoldedRoomBangStillSaysHowItEnded(t *testing.T) {
	room := bangRoomText(roomBanged(t, printRows(6)+"; exit 3"))
	pointer, status := bangRowOf(room, "to expand"), bangRowOf(room, fmt.Sprintf(bangExited, 3))
	if status < 0 || pointer < 0 || status < pointer {
		t.Errorf("the exit status (row %d) is not drawn under the pointer (row %d):\n%s", status, pointer, room)
	}
	if strings.Contains(room, "out-05") || !strings.Contains(room, "+3 lines") {
		t.Errorf("a failing bang is not folded like any other:\n%s", room)
	}
	whole := bangRoomText(roomBanged(t, printRows(2)+"; exit 3"))
	if out, status := bangRowOf(whole, "out-01"), bangRowOf(whole, fmt.Sprintf(bangExited, 3)); out < 0 || status != out+1 {
		t.Errorf("an unfolded bang does not end on its status (output row %d, status row %d):\n%s", out, status, whole)
	}
}

// bangRowOf is the first row of a drawn room holding needle, or -1.
func bangRowOf(room, needle string) int {
	for i, row := range strings.Split(room, "\n") {
		if strings.Contains(row, needle) {
			return i
		}
	}
	return -1
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

// Column output keeps the indent its first row opens with.
func TestARoomBangKeepsTheIndentOfItsFirstRow(t *testing.T) {
	want := strings.Repeat(" ", bodyIndent) + " M a.go"
	room := bangRoomText(roomBanged(t, `printf ' M a.go\n M b.go\n?? c.go\n'`))
	if bangRowOf(room, want) < 0 || !strings.Contains(room, strings.Repeat(" ", bodyIndent)+" M b.go") {
		t.Errorf("the first row lost its indent (want a row of %q):\n%s", want, room)
	}
	r := restored([]core.Event{recordOf("s1", "git status -s", " M a.go\n M b.go\n?? c.go", base, true)})
	if got := roomShown(r, 100, 24); bangRowOf(got, want) < 0 {
		t.Errorf("the reopened bang lost the first row's indent:\n%s", got)
	}
}

// Output rows that open the way Wake's notes do are output, past the few notes a
// command can leave, so they fold like any other.
func TestARoomBangWhoseRowsLookLikeNotesStillFolds(t *testing.T) {
	room := bangRoomText(roomBanged(t, "printf '… %02d\\n' 0 1 2 3 4 5 6 7 8 9"))
	if !strings.Contains(room, "to expand") || strings.Contains(room, "… 05") {
		t.Errorf("a bang printing rows of %q is not folded:\n%s", bangNoteLead, room)
	}
}

// bangNoteMax is the most note rows bangRun can leave under an output, whichever
// way the command ended.
func TestABangLeavesNoMoreNoteRowsThanTheRoomKeeps(t *testing.T) {
	exited := exec.Command("sh", "-c", "exit 3").Run()
	most := 0
	for _, ending := range []struct {
		err      error
		timedOut bool
	}{{nil, false}, {exited, false}, {errors.New("signal"), false}, {exec.ErrWaitDelay, false}, {exited, true}} {
		for _, group := range []string{"", bangReclaimed, fmt.Sprintf(bangCleanupFailed, "gone")} {
			for _, cut := range []bool{false, true} {
				text := bangText("out", cut, ending.err, ending.timedOut, time.Second, time.Second)
				if group != "" {
					text = bangNote(text, group)
				}
				most = max(most, strings.Count(text, "\n"+bangNoteLead))
			}
		}
	}
	if most != bangNoteMax {
		t.Errorf("a command leaves at most %d note rows, bangNoteMax says %d", most, bangNoteMax)
	}
}

// A copy of a room bang rejoins what the pane wrapped, so a long command pastes
// back as the one line it was - not as rows with the pane's indent on them.
func TestCopyingARoomBangRejoinsItsWraps(t *testing.T) {
	fresh(t)
	cmd := "echo " + strings.TrimSpace(strings.Repeat("longword ", 9)) // isBang trims a command
	out := strings.Repeat("output ", 12)
	room := bangRoomApp(t, bangResultMsg{Cmd: cmd, Text: out})
	tr := room.transcriptIn("")
	if rows := strings.Count(strings.Join(tr.lines.slice(tr.lines.first(), tr.lines.len()), "\n"), "longword"); rows < 2 {
		t.Fatalf("the command did not wrap, so this proves nothing")
	}
	got := copyOf(t, room, "", lineHolding(t, tr, "!echo"), 0, tr.lines.len()-1, tr.width-1)
	if want := "!" + cmd + "\n" + strings.TrimSpace(out); got != want {
		t.Errorf("the room copied\n%q\nwant\n%q", got, want)
	}
}

// Folded, the pointer ends what a copy can rejoin, but the message above it still
// pastes back whole.
func TestCopyingAFoldedRoomBangStillRejoinsItsMessage(t *testing.T) {
	fresh(t)
	cmd := "echo " + strings.TrimSpace(strings.Repeat("longword ", 9)) // isBang trims a command
	room := bangRoomApp(t, bangResultMsg{Cmd: cmd, Text: strings.Join(repeatLines("row", 6), "\n")})
	tr := room.transcriptIn("")
	if !strings.Contains(strings.Join(tr.lines.slice(tr.lines.first(), tr.lines.len()), "\n"), "to expand") {
		t.Fatal("the output is not folded, so this proves nothing")
	}
	head := lineHolding(t, tr, "!echo")
	got := copyOf(t, room, "", head, 0, lineHolding(t, tr, "row-00")-1, tr.width-1)
	if want := "!" + cmd; got != want {
		t.Errorf("the room copied\n%q\nwant\n%q", got, want)
	}
}

// bangRoomApp is a 50-column room that drew one finished bang.
func bangRoomApp(t *testing.T, m bangResultMsg) App {
	t.Helper()
	room := NewRoomApp(nil, Stream{}, nil)
	room.layout.ShowGroups, room.layout.ShowRoster = false, false
	room = resized(t, room, 50, 40)
	room, _ = room.bangResult(m)
	return room
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

// A reopened bang is in its agent's thread, as the live one was: a room narrowed
// to another agent does not draw it.
func TestAReopenedBangIsInItsAgentsThreadOnly(t *testing.T) {
	r := restored([]core.Event{recordOf("s1", "ls -a", "out-00", base, true)})
	if got := roomShown(r.WithFocus("s1", "agent-s1"), 100, 24); !strings.Contains(got, "!ls -a") {
		t.Errorf("a room narrowed to the agent does not draw its bang:\n%s", got)
	}
	if got := roomShown(r.WithFocus("s2", "agent-s2"), 100, 24); strings.Contains(got, "!ls -a") {
		t.Errorf("a room narrowed to another agent draws the bang:\n%s", got)
	}
}
