package ui

// A peer's cross-session message folds at three rows, in the room and in a
// conversation, and one gesture opens it: ⌃E for every fold, a click for one.
// The fold is one colour - the body's Muted, the pointer's too - so a long
// message from another session never reads as a wall of the agent's own words.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// peerMessage is a cross-session message of n short rows, peer-00 first.
func peerMessage(n int) core.Event {
	return core.Event{
		Kind: core.KindCrossSession, SessionID: "s1", FromName: "planner",
		Text: strings.Join(repeatLines("peer", n), "\n"),
	}
}

// peerRow is the transcript line a DM drew needle on, or fails the test.
func peerRow(t *testing.T, d DM, needle string) int {
	t.Helper()
	for line := d.tr.lines.first(); line < d.tr.lines.len(); line++ {
		if strings.Contains(ansi.Strip(d.tr.lines.at(line)), needle) {
			return line
		}
	}
	t.Fatalf("the DM drew no row holding %q", needle)
	return 0
}

func peerDM(n int) DM {
	return NewDM("s1", "sydney").SetSize(60, 40).Append(peerMessage(n))
}

// --- the DM ----------------------------------------------------------------

func TestADMDrawsAPeerMessageOfThreeRowsWhole(t *testing.T) {
	d := peerDM(peerFoldRows)
	assertShows(t, d, 60, 40, "peer-02")
	assertHides(t, d, 60, 40, "to expand")
}

func TestADMFoldsAPeerMessageToThreeRowsAndSaysHowToOpenIt(t *testing.T) {
	d := peerDM(peerFoldRows + 3)
	assertShows(t, d, 60, 40, "peer-02")
	assertHides(t, d, 60, 40, "peer-03")
	assertShows(t, d, 60, 40, "+3 lines")
	assertShows(t, d, 60, 40, expandKey+" or click to expand")
}

func TestClickingAFoldedPeerMessageOpensItAndAgainFoldsIt(t *testing.T) {
	d := peerDM(peerFoldRows + 3)
	line := peerRow(t, d, "peer-00")

	opened, hit := d.openTool(line)
	if !hit {
		t.Fatal("a click on a folded peer message reported no hit")
	}
	assertShows(t, opened, 60, 40, "peer-05")
	assertHides(t, opened, 60, 40, "to expand")

	folded, hit := opened.openTool(peerRow(t, opened, "peer-00"))
	if !hit {
		t.Fatal("the opened peer message stopped being clickable")
	}
	assertHides(t, folded, 60, 40, "peer-05")
}

func TestClickingAShortPeerMessageOpensNothing(t *testing.T) {
	d := peerDM(peerFoldRows)
	for line := d.tr.lines.first(); line < d.tr.lines.len(); line++ {
		if _, hit := d.openTool(line); hit {
			t.Errorf("line %d of a peer message that never folded is a click target; a click there would rewrap the whole transcript for nothing", line)
		}
	}
}

func TestCtrlEOpensEveryFoldedPeerMessageAndAgainFoldsThem(t *testing.T) {
	d := peerDM(peerFoldRows + 3).Append(peerMessage(peerFoldRows + 4))
	assertHides(t, d, 60, 40, "peer-05")

	opened := d.toggleExpanded()
	assertShows(t, opened, 60, 40, "peer-05")
	assertShows(t, opened, 60, 40, "peer-06")

	assertHides(t, opened.toggleExpanded(), 60, 40, "peer-05")
}

// The same words twice are one message to read, so opening one opens both; what
// must not happen is a different message opening with them.
func TestOpeningAPeerMessageLeavesAnotherFolded(t *testing.T) {
	other := peerMessage(peerFoldRows + 3)
	other.Text = strings.ReplaceAll(other.Text, "peer", "other")
	d := peerDM(peerFoldRows + 3).Append(other)

	opened, _ := d.openTool(peerRow(t, d, "peer-00"))
	assertShows(t, opened, 60, 40, "peer-05")
	assertHides(t, opened, 60, 40, "other-05")
}

// withFoldedPeer is a window whose focused conversation, s1, holds one folded
// peer message - and so does the room beside it, which folds the same message.
func withFoldedPeer(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a.roster.Selected = "s1"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if a.focus != "s1" {
		t.Fatalf("focus is %q, want the conversation under test", a.focus)
	}
	ev := peerMessage(peerFoldRows + 3)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
	if strings.Contains(shown(a), "peer-05") {
		t.Fatalf("the peer message is not folded, so this test proves nothing:\n%s", shown(a))
	}
	return a
}

// A click arrives as a press and release on one cell; endSelection routes an
// empty selection to clickedTool, the wiring the DM-level tests above skip.
func TestAClickOpensAFoldedPeerMessageThroughTheApp(t *testing.T) {
	a := withFoldedPeer(t)
	line := peerRow(t, *a.dms["s1"], "peer-00")
	a.sel = selection{pane: "s1", anchor: point{line: line}, head: point{line: line}}
	a.selecting = true
	a, _ = a.endSelection()

	if !strings.Contains(shown(a), "peer-05") {
		t.Errorf("clicking a folded peer message did not open it:\n%s", shown(a))
	}
}

func TestCtrlEOpensAFoldedPeerMessageThroughTheApp(t *testing.T) {
	a, _ := pressKey(withFoldedPeer(t), tea.KeyMsg{Type: tea.KeyCtrlE})
	if !strings.Contains(shown(a), "peer-05") {
		t.Errorf("⌃E did not open a folded peer message:\n%s", shown(a))
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlE})
	if strings.Contains(shown(a), "peer-05") {
		t.Errorf("a second ⌃E did not fold it back:\n%s", shown(a))
	}
}

// A peer's brief is usually one long paragraph, so only wrapping makes it tall:
// it folds on the rows it draws at this width, never on its newlines, and a width
// change flips it between whole and folded in both places.
func TestAPeerParagraphFoldsByItsWrappedRowsAndFollowsTheWidth(t *testing.T) {
	ev := peerMessage(1)
	ev.Text = strings.TrimSpace(strings.Repeat("alpha beta gamma delta ", 12))
	const wide, narrow = 200, 40
	if rows := renderedRows(crossSessionBody(ev.Text, wide)); rows > peerFoldRows {
		t.Fatalf("the fixture is %d rows at %d columns, so it cannot be whole there", rows, wide)
	}
	if rows := renderedRows(crossSessionBody(ev.Text, narrow)); rows <= peerFoldRows {
		t.Fatalf("the fixture is %d rows at %d columns, so it cannot fold there", rows, narrow)
	}

	by, to := Agent{Name: "planner"}, Agent{Name: "sydney"}
	for w, folds := range map[int]bool{wide: false, narrow: true} {
		drawn := strings.Contains(roomBlock(ev, by, to, w, false).text, "to expand")
		if drawn != folds || roomCollapsible(ev, w) != folds {
			t.Errorf("room at %d columns: pointer drawn = %v, collapsible = %v, want both %v", w, drawn, roomCollapsible(ev, w), folds)
		}
	}

	d := NewDM("s1", "sydney").SetSize(wide+10, 40).Append(ev)
	assertHides(t, d, wide+10, 40, "to expand")
	d = d.SetSize(narrow, 40)
	assertShows(t, d, narrow, 40, "to expand")
	opened, hit := d.openTool(peerRow(t, d, "alpha"))
	if !hit {
		t.Fatal("a re-wrap that folded the message did not make it a click target")
	}
	assertHides(t, opened, narrow, 40, "to expand")
	widened := opened.SetSize(wide+10, 40)
	for line := widened.tr.lines.first(); line < widened.tr.lines.len(); line++ {
		if _, hit := widened.openTool(line); hit {
			t.Errorf("line %d is still a click target after the message stopped folding", line)
		}
	}
}

// Every row of a fold is a click target, the head and the pointer included.
func TestEveryRowOfAFoldedPeerMessageIsClickable(t *testing.T) {
	d := peerDM(peerFoldRows + 3)
	first, last := peerRow(t, d, "↪ planner"), peerRow(t, d, "to expand")
	if last-first != peerFoldRows+1 {
		t.Fatalf("a fold is a head, %d body rows and a pointer, got rows %d to %d", peerFoldRows, first, last)
	}
	for line := first; line <= last; line++ {
		if _, hit := d.openTool(line); !hit {
			t.Errorf("row %d of the fold is not a click target", line)
		}
	}
}

// One hidden row is a line, not lines.
func TestAPeerFoldHidingOneRowSaysLine(t *testing.T) {
	d := peerDM(peerFoldRows + 1)
	assertShows(t, d, 60, 40, "+1 line ·")
	assertHides(t, d, 60, 40, "+1 lines")
}

// The key is the sender's words, not the words alone: the same text from two
// sessions is two messages, and opening one leaves the other folded.
func TestTheSameWordsFromTwoSendersAreTwoMessages(t *testing.T) {
	first := peerMessage(peerFoldRows + 3)
	second := first
	second.FromName = "reviewer"
	d := NewDM("s1", "sydney").SetSize(60, 80).Append(first).Append(second)

	opened, hit := d.openTool(peerRow(t, d, "peer-00"))
	if !hit {
		t.Fatal("a click on the first folded message reported no hit")
	}
	if got := strings.Count(visible(opened, 60, 80), "peer-05"); got != 1 {
		t.Errorf("opening one sender's message left %d copies open, want exactly 1:\n%s", got, visible(opened, 60, 80))
	}
}

// A subagent's block is drawn a gutter narrower than the pane, so a message that
// just fits three rows at the pane's width folds inside it. The click key follows
// what is drawn: a pointer is always a click target, or it promises a click that
// does nothing.
func TestAGutteredPeerMessageIsClickableExactlyWhenItDrawsAPointer(t *testing.T) {
	d := NewDM("s1", "sydney").SetSize(60, 40)
	ev := peerMessage(1)
	ev.Text = strings.Repeat("x", 3*(d.blockWidth()-bodyIndent)) // three rows at the pane's width, four a gutter in
	ev.Subagent = &core.Subagent{Dispatch: "t1", Agent: "a1"}

	if peerFolds(ev.Text, d.blockWidth()) {
		t.Fatal("the fixture folds at the pane's width, so it cannot tell the two widths apart")
	}
	b := d.renderEvent(ev)
	if !strings.Contains(b.text, "to expand") {
		t.Fatalf("the fixture does not fold inside the gutter, so this test proves nothing:\n%s", b.text)
	}
	if b.tool == "" {
		t.Errorf("a peer message drawn with a pointer inside a gutter is not a click target:\n%s", b.text)
	}
}

// --- the room --------------------------------------------------------------

func TestTheRoomFoldsAPeerMessageAtTheSameThreeRows(t *testing.T) {
	to := Agent{Name: "sydney"}
	whole := roomBlock(peerMessage(peerFoldRows), Agent{Name: "planner"}, to, roomWidth, false).text
	if strings.Contains(whole, "to expand") || !strings.Contains(ansi.Strip(whole), "peer-02") {
		t.Errorf("a peer message of %d rows did not draw whole:\n%s", peerFoldRows, whole)
	}

	folded := ansi.Strip(roomBlock(peerMessage(peerFoldRows+3), Agent{Name: "planner"}, to, roomWidth, false).text)
	if !strings.Contains(folded, "peer-02") || strings.Contains(folded, "peer-03") {
		t.Errorf("a peer message past %d rows did not keep exactly its first %d:\n%s", peerFoldRows, peerFoldRows, folded)
	}
	for _, want := range []string{"+3 lines", expandKey + " or click to expand"} {
		if !strings.Contains(folded, want) {
			t.Errorf("the room's pointer lacks %q:\n%s", want, folded)
		}
	}
	if strings.Contains(folded, openDMHint) {
		t.Errorf("a peer message's pointer names the DM, not the expand:\n%s", folded)
	}
}

func TestExpandedDrawsAWholePeerMessageInTheRoom(t *testing.T) {
	b := roomBlock(peerMessage(peerFoldRows+3), Agent{Name: "planner"}, Agent{Name: "sydney"}, roomWidth, true).text
	if out := ansi.Strip(b); !strings.Contains(out, "peer-05") || strings.Contains(out, "to expand") {
		t.Errorf("an expanded peer message is not whole:\n%s", out)
	}
}

func TestRoomCollapsibleHoldsAPeerMessageToTheSameThreeRows(t *testing.T) {
	for _, c := range []struct {
		rows int
		want bool
	}{{peerFoldRows, false}, {peerFoldRows + 1, true}} {
		ev := peerMessage(c.rows)
		got := roomCollapsible(ev, roomWidth)
		drawn := strings.Contains(roomBlock(ev, Agent{Name: "planner"}, Agent{Name: "sydney"}, roomWidth, false).text, "to expand")
		if got != c.want || drawn != c.want {
			t.Errorf("%d rows: roomCollapsible = %v, pointer drawn = %v, want both %v", c.rows, got, drawn, c.want)
		}
	}
}

func TestAClickOpensOneFoldedPeerMessageInTheRoom(t *testing.T) {
	r := NewRoom().SetSize(80, 200)
	r = r.Append(peerMessage(peerFoldRows+3), Agent{Name: "planner"})
	line, _ := expandableLine(t, r)

	next, hit := r.toggleLine(line)
	if !hit {
		t.Fatal("a click on a folded peer message in the room reported no hit")
	}
	if out := roomShown(next, 80, 200); !strings.Contains(out, "peer-05") {
		t.Errorf("the clicked peer message did not expand:\n%s", out)
	}
	if out := roomShown(next.toggleExpandAll().toggleExpandAll(), 80, 200); strings.Contains(out, "peer-05") {
		t.Errorf("⌃E twice did not fold the room's peer messages back:\n%s", out)
	}
}

// --- both -------------------------------------------------------------------

// The head carries the sender's own colour; everything under it is the body's
// Muted, the pointer included, so a fold is one colour from its first row to
// the pointer that opens it.
func TestAFoldedPeerMessageIsOneColourUnderItsHead(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	muted := fgEscape(t, Muted)

	ev := peerMessage(peerFoldRows + 3)
	surfaces := map[string]string{
		"the DM":   crossSessionBlock(ev, 60, false),
		"the room": roomBlock(ev, Agent{Name: "planner"}, Agent{Name: "sydney"}, roomWidth, false).text,
	}
	for name, block := range surfaces {
		rows := strings.Split(block, "\n")
		if len(rows) != 1+peerFoldRows+1 {
			t.Errorf("%s: a head, %d body rows and a pointer is the whole of a fold, got %d rows:\n%s", name, peerFoldRows, len(rows), block)
			continue
		}
		for i, row := range rows[1:] {
			if !strings.Contains(row, muted) {
				t.Errorf("%s: row %d under the head is not Muted: %q", name, i+1, row)
			}
		}
	}
}

func TestAPeerFoldNeverExceedsItsWidth(t *testing.T) {
	body := strings.Repeat("an incoming peer message long enough to wrap at any column width ", 6)
	for _, w := range []int{1, 2, 3, 8, 20, 40} {
		for i, row := range strings.Split(peerBody(body, w, false), "\n") {
			if got := lipgloss.Width(row); got > w {
				t.Errorf("width %d: row %d is %d cells, wider than the column: %q", w, i, got, row)
			}
		}
	}
}
