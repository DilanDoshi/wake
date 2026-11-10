package ui

// The stored layout lags what View draws - chrome moves with no resize, and View
// re-lays only a throwaway - so every move of the reader settles it first, and the
// answer being written is the window's last rows. See followbanner.go.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// drawnOf is d as View lays it out: the stored layout, re-laid on a copy when its
// chrome has moved since.
func drawnOf(d DM) DM {
	if d.chromeHeight() != d.chrome {
		d = d.SetSize(d.width, d.height)
	}
	return d
}

// behindNewest is how many rows the foot of the drawn window is behind its newest
// row, the answer's when one is being written: 0 for a reader following.
func behindNewest(d DM) int {
	tr := drawnOf(d).tr
	top := min(max(tr.scroll, tr.first()), tr.bottom())
	return tr.end() - 1 - (top + tr.height - 1)
}

func showsBanner(d DM) bool { return slices.ContainsFunc(frameRows(d), hasText(followBannerText)) }

func hasText(s string) func(string) bool {
	return func(l string) bool { return strings.Contains(l, s) }
}

func landed(d DM, text string) DM {
	return d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: text})
}

// A wheel notch moves what the reader sees by the notch, whatever the answer has
// done since the last resize: three up is three rows back, and three down is back
// at the newest row - not six, and not three early.
func TestAWheelNotchMovesTheDrawnBottomLineByExactlyTheNotch(t *testing.T) {
	d := pushDM(t, pushH)
	room := previewRoom(d)
	d, text := streamRows(d, "", room+2) // the stored layout is still the one from before the answer
	if got := behindNewest(d); got != 0 {
		t.Fatalf("the follower's foot is %d lines behind the newest", got)
	}

	for i, step := range []struct{ wheel, behind int }{{3, 3}, {3, 6}, {-2, 4}, {-4, 0}} {
		d = d.ScrollUp(step.wheel)
		if got := behindNewest(d); got != step.behind {
			t.Fatalf("step %d, wheel %+d: the drawn bottom line is %d behind the newest, want %d", i, step.wheel, got, step.behind)
		}
		if got := showsBanner(d); got != (step.behind > 0) {
			t.Errorf("step %d: banner drawn = %v with the reader %d lines back", i, got, step.behind)
		}
		if got := len(frameRows(d)); got != pushH {
			t.Errorf("step %d: the pane drew %d rows, want %d", i, got, pushH)
		}
	}

	// Following resumed exactly at the newest row, so the answer pushes the pane up.
	d, text = streamRows(d, text, len(wrappedRows(text))+room+2)
	assertFollowsAnswer(t, "back at the newest row", d, text)
}

// The production order of an old symptom: a resize mid-answer, the block lands,
// the reader wheels up, and the next answer streams under them. Every notch moves
// them by the notch and the banner says when they have left the newest row.
func TestAResizeMidAnswerThenALandingLeavesTheWheelExact(t *testing.T) {
	d := pushDM(t, pushH)
	d, _ = streamRows(d, "", previewRoom(d)+2)
	d = d.SetSize(pushW, pushH) // a resize mid-answer
	d = landed(d, "the finished answer")
	if d.chromeHeight() != d.chrome {
		t.Errorf("the block landed and the stored layout still holds the preview's chrome (%d, drawn %d)", d.chrome, d.chromeHeight())
	}

	d = d.ScrollUp(3)
	if got := behindNewest(d); got != 3 || !showsBanner(d) {
		t.Fatalf("after one notch the reader is %d lines back with the banner = %v, want 3 and a banner", got, showsBanner(d))
	}
	d, _ = streamRows(d, "", minPreviewRows+2)
	behind := behindNewest(d) // the new answer's rows are under the window
	if got := behindNewest(d.ScrollUp(-3)); got != behind-3 {
		t.Errorf("a notch down left the reader %d lines behind, want %d", got, behind-3)
	}
	if d = d.ScrollUp(-behind); showsBanner(d) || behindNewest(d) != 0 {
		t.Errorf("wheeling back down left the reader %d lines behind with the banner = %v", behindNewest(d), showsBanner(d))
	}
}

// A subagent's view is the conversation's transcript swapped under the same
// chrome. The parent's answer is not what the reader opened, so none of it is
// drawn there, and it is back under the conversation when they return.
func TestAParentTalkingDoesNotSqueezeASubagentView(t *testing.T) {
	d := pushDM(t, pushH)
	for i := range 30 {
		d = d.Append(spoke("dispatch-1", fmt.Sprintf("subagent line %02d", i)))
	}
	d = d.Viewing("dispatch-1")
	d, _ = streamRows(d, "", previewRoom(d)+2)

	frame := frameRows(d)
	if slices.ContainsFunc(frame, hasText("Sentence")) {
		t.Errorf("the parent's answer is drawn in a subagent's view:\n%s", strings.Join(frame, "\n"))
	}
	if got := len(slices.DeleteFunc(slices.Clone(frame), func(l string) bool { return !strings.Contains(l, "subagent line") })); got < 5 {
		t.Errorf("the subagent's view shows %d lines, squeezed by the parent's answer:\n%s", got, strings.Join(frame, "\n"))
	}
	if back := frameRows(d.Viewing("")); !slices.ContainsFunc(back, hasText("Sentence")) {
		t.Errorf("back in the conversation, the parent's answer is not drawn:\n%s", strings.Join(back, "\n"))
	}
}

// Folding a long open result brings the newest row onto the screen under a reader
// who had scrolled into it: they are following the answer again, though no wheel
// or key said so.
func TestFoldingALongResultOverAStreamingAnswerFollowsItAgain(t *testing.T) {
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	d = withRunOpen(d.SetSize(pushW, pushH), "t1")
	for i := range 20 {
		d = landed(d, fmt.Sprintf("earlier line %03d", i))
	}
	d = d.Append(bashCall("t1")).Append(result("t1", strings.Join(repeatLines("out", 60), "\n"), false))
	d = landed(d, "closing words")
	room := previewRoom(d)

	head, ok := d.tr.headLine("t1")
	if !ok {
		t.Fatal("the call was never drawn")
	}
	d, _ = d.openTool(head) // open the 60-line result
	d = d.ScrollUp(40)      // and read back into it
	d, text := streamRows(d, "", minPreviewRows+2)
	if !showsBanner(d) {
		t.Fatal("scrolled back into the result, the reader is not told they left the newest row")
	}

	d, _ = d.openTool(head) // fold it again: the newest row is on screen under the reader
	if showsBanner(d) {
		t.Fatal("the fold left the banner up over a reader at the newest line")
	}
	d, text = streamRows(d, text, len(wrappedRows(text))+room+2)
	assertFollowsAnswer(t, "after the fold", d, text)
}

// A fold decides whether the reader reached the newest line, and it must decide
// on the layout drawn: with a preview's rows not yet in the stored one it would
// call a reader two lines back "at the bottom" and yank them there.
func TestFoldingAResultDoesNotYankAReaderTwoLinesBackUnderAStreamingAnswer(t *testing.T) {
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	d = withRunOpen(d.SetSize(pushW, pushH), "t1")
	for i := range 20 {
		d = landed(d, fmt.Sprintf("earlier line %03d", i))
	}
	d = d.Append(bashCall("t1")).Append(result("t1", strings.Join(repeatLines("out", 8), "\n"), false))
	d = landed(d, "closing words")
	head, _ := d.tr.headLine("t1")
	d, _ = d.openTool(head)
	d = d.ScrollUp(2)
	d, _ = streamRows(d, "", minPreviewRows)

	d, _ = d.openTool(head) // folds three lines
	if !showsBanner(d) || behindNewest(d) == 0 {
		t.Errorf("the fold left the reader %d lines back with the banner = %v: they were never at the newest line", behindNewest(d), showsBanner(d))
	}
}

// A draft sent mid-answer gives its rows back to the window, which the answer
// fills under a reader following it: a stored layout squeezed by the tall box would
// leave the window short.
func TestAComposerThatShrinksGivesTheWindowItsRowsBack(t *testing.T) {
	var draft []string
	for i := range 10 {
		draft = append(draft, fmt.Sprintf("draft line %d", i))
	}
	d := withDraft(t, pushDM(t, pushH), draft...)
	squeezed := len(windowOf(d))
	d, _ = streamRows(d, "", previewRoom(d)+2)
	d = landed(d, "a block that landed under a tall draft")

	d = d.WithComposer(d.Composer().Reset()) // sent
	if room := len(windowOf(d)); room < squeezed+4 {
		t.Fatalf("the one-row box leaves a %d-row window against %d under the draft: too close to tell", room, squeezed)
	}
	d, text := streamRows(d, "", previewRoom(d)+2)
	assertFollowsAnswer(t, "after the draft was sent", d, text)
	if frame := frameRows(d); len(frame) != pushH {
		t.Errorf("the pane drew %d rows, want %d", len(frame), pushH)
	}
}

// A drag held at a pane's edge scrolls it, and a follower's preview gives its
// rows back the moment the reader leaves the bottom - so the window the drag was
// taken in is not the window drawn after the first pull. The pointer must map
// into the pane as it is now.
func TestADragThatPullsAFollowerBackMapsThePointerIntoTheNewWindow(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 4)
	col := a.columnOf("s1")
	a = a.refocus("s1")
	d := *a.dms["s1"]
	for i := range 80 {
		d = landed(d, fmt.Sprintf("earlier line %03d", i))
	}
	d = tokens(d, strings.TrimSuffix(strings.Repeat("a line of the streamed answer\n", 12), "\n"))
	a = a.withDM("s1", d)

	x, w, h := midOf(a.regions(), col), a.regions().Cols[col], a.paneHeight()
	a, _ = a.mouse(pressAt(x, 8))
	was := a.transcriptIn("s1").scroll
	a, _ = a.mouse(motion(x, 0)) // the top edge: pulls back, leaving the bottom
	if a.transcriptIn("s1").scroll >= was {
		t.Fatal("the pull did not scroll the pane, so this proves nothing about the window after it")
	}
	rows := strings.Split(ansi.Strip(a.dmPane("s1", w, h)), "\n")
	y := len(rows) - 1
	for y > 0 && !strings.Contains(rows[y], "earlier line") {
		y-- // the deepest transcript row, far below where the drag was taken
	}
	scroll := a.transcriptIn("s1").scroll
	a, _ = a.mouse(motion(x, y))

	if got := a.transcriptIn("s1").scroll; got != scroll {
		t.Errorf("a pointer inside the window pulled the pane from line %d to %d: the edge is where the drag was taken, not where the pane is", scroll, got)
	}
	line := a.sel.head.line
	want := strings.TrimSpace(rows[y])
	if got := strings.TrimSpace(ansi.Strip(a.transcriptIn("s1").lines.slice(line, line+1)[0])); got != want {
		t.Errorf("the pointer on row %d (%q) selected line %d (%q)", y, want, line, got)
	}
}

// A block that finishes is laid out again as the landed one will be, and a list
// written with inline markup can come out shorter than its plain rows. A reader
// scrolled back into it keeps reading: the rows shrinking under them never make
// them a follower the next token pushes.
func TestAFinishingBlockThatShrinksLeavesAReaderReading(t *testing.T) {
	d := messageStarted(pushDM(t, pushH))
	var list strings.Builder
	for i := range 30 {
		fmt.Fprintf(&list, "- **item %02d** says `x` and *a* few **more** words here to wrap\n", i)
	}
	d = streamed(d, list.String())
	open := d.partial.rows.len()
	d = d.ScrollUp(6)
	if d.tr.atBottom() {
		t.Fatal("the reader is not scrolled back")
	}
	d = streamed(d, "\nThe next paragraph")
	if d.partial.rows.len() >= open {
		t.Fatalf("the finished list drew %d rows against %d plain: this fixture does not shrink", d.partial.rows.len(), open)
	}
	if d.tr.atBottom() || !showsBanner(d) {
		t.Errorf("the list shrinking under a reader reading back made them a follower (banner = %v)", showsBanner(d))
	}
	reading := frameRows(d)
	d = streamed(d, " goes on for a while.")
	if got := frameRows(d); !slices.Equal(got[:len(windowOf(d))], reading[:len(windowOf(d))]) {
		t.Errorf("the next tokens moved the reader:\n%s\nwas\n%s", strings.Join(got, "\n"), strings.Join(reading, "\n"))
	}
}

// A subagent's words land between two of the agent's tokens and end none of the
// agent's: the answer being written stays, and so does a reader scrolled back into
// it - its rows are not taken away under them, and the agent's next tokens move
// nothing they read.
func TestASubagentSpeakingMidAnswerLeavesTheAnswerAndItsReader(t *testing.T) {
	d := messageStarted(pushDM(t, pushH))
	d, text := streamRows(d, "", previewRoom(d)+8)
	d = d.ScrollUp(9)
	reading := frameRows(d)

	d = d.Append(spoke("dispatch-1", "the subagent reports in"))
	if got := frameRows(d); !slices.Equal(got[:len(windowOf(d))], reading[:len(windowOf(d))]) {
		t.Fatalf("a subagent speaking moved the reader:\n%s\nwas\n%s", strings.Join(got, "\n"), strings.Join(reading, "\n"))
	}
	d, _ = streamRows(d, text, len(wrappedRows(text))+2)
	if d.tr.atBottom() {
		t.Fatal("the agent's next tokens found the reader following")
	}
	if got := frameRows(d); !slices.Equal(got[:len(windowOf(d))], reading[:len(windowOf(d))]) {
		t.Errorf("the agent's next tokens moved the reader:\n%s\nwas\n%s", strings.Join(got, "\n"), strings.Join(reading, "\n"))
	}
}
