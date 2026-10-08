package ui

// The stored layout lags what View draws - a preview grows chrome with no resize,
// and View re-lays only a throwaway - so every move of the reader settles it
// first and keeps the transcript's bottom line where the scroll put it across a
// change of the preview's cap. See followbanner.go.

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

// behindNewest is how many lines the foot of the drawn transcript is behind its
// newest line: 0 for a reader following.
func behindNewest(d DM) int {
	tr := drawnOf(d).tr
	top := min(max(tr.scroll, tr.first()), tr.bottom())
	return tr.lines.len() - 1 - (top + tr.height - 1)
}

func showsBanner(d DM) bool { return slices.ContainsFunc(frameRows(d), hasText(followBannerText)) }

func hasText(s string) func(string) bool {
	return func(l string) bool { return strings.Contains(l, s) }
}

func landed(d DM, text string) DM {
	return d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: text})
}

// A wheel notch moves what the reader sees by the notch, whatever the preview
// has done to the layout since the last resize: three up is three lines back,
// and three down is back at the newest line - not six, and not three early.
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

	// Following resumed exactly at the newest line, so the preview has its room back.
	d, _ = streamRows(d, text, len(wrappedRows(text))+room+2)
	if got := previewUnder(t, frameRows(d), pushNewest); len(got) != room {
		t.Errorf("back at the newest line the preview drew %d rows, want the pane's %d", len(got), room)
	}
}

// The production order of the symptom: a resize stores the layout of a full
// preview, the block lands, and the reader wheels up. The stored layout must
// not outlive the preview, or the wheel measures a one-row transcript and the
// pane draws as following - no banner - over a preview held at three rows.
func TestAStoredFullPreviewLayoutDoesNotOutliveItsPreview(t *testing.T) {
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
	if got := previewUnder(t, frameRows(d), followBannerText); len(got) != minPreviewRows {
		t.Errorf("a scrolled-back reader's preview drew %d rows, want the floor of %d", len(got), minPreviewRows)
	}
	behind := behindNewest(d) // the new answer's rows now cover the foot of the window
	if got := behindNewest(d.ScrollUp(-3)); got != behind-3 {
		t.Errorf("a notch down left the reader %d lines behind, want %d", got, behind-3)
	}
	if d = d.ScrollUp(-behind); showsBanner(d) || behindNewest(d) != 0 {
		t.Errorf("wheeling back down left the reader %d lines behind with the banner = %v", behindNewest(d), showsBanner(d))
	}
}

// A subagent's view is the conversation's transcript swapped under the same
// chrome, and the parent's preview is drawn under it all the same. The preview
// must not take the view's rows: it is not what the reader opened.
func TestAParentTalkingDoesNotSqueezeASubagentView(t *testing.T) {
	d := pushDM(t, pushH)
	for i := range 30 {
		d = d.Append(spoke("dispatch-1", fmt.Sprintf("subagent line %02d", i)))
	}
	d = d.Viewing("dispatch-1")
	d, _ = streamRows(d, "", previewRoom(d)+2)

	frame := frameRows(d)
	if got := previewUnder(t, frame, "subagent line 29"); len(got) != minPreviewRows {
		t.Errorf("the parent's preview drew %d rows under a subagent's view, want the floor of %d", len(got), minPreviewRows)
	}
	if got := len(slices.DeleteFunc(slices.Clone(frame), func(l string) bool { return !strings.Contains(l, "subagent line") })); got < 5 {
		t.Errorf("the subagent's view shows %d lines, squeezed by the parent's preview:\n%s", got, strings.Join(frame, "\n"))
	}
}

// Folding a long open result brings the newest line onto the screen under a
// reader who had scrolled into it: they are following again, and the preview
// has its room, though no wheel or key said so.
func TestFoldingALongResultOverAStreamingAnswerRestoresThePreviewsRoom(t *testing.T) {
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
	if got := previewUnder(t, frameRows(d), followBannerText); len(got) != minPreviewRows {
		t.Fatalf("scrolled back, the preview drew %d rows, want %d", len(got), minPreviewRows)
	}

	d, _ = d.openTool(head) // fold it again: the newest line is on screen under the reader
	if showsBanner(d) {
		t.Fatal("the fold left the banner up over a reader at the newest line")
	}
	d, _ = streamRows(d, text, len(wrappedRows(text))+room+2)
	if got := previewUnder(t, frameRows(d), "closing words"); len(got) != room {
		t.Errorf("after the fold the preview drew %d rows, want the pane's %d", len(got), room)
	}
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

// The stored cap is measured against the composer as it stands, and View
// re-measures only a copy, so a draft that was tall when a block landed would
// leave the stored cap squeezed after it is sent: later tokens would be trimmed
// to it, and the preview would stay short of the pane's room.
func TestAComposerThatShrinksGivesThePreviewItsRoomBack(t *testing.T) {
	var draft []string
	for i := range 10 {
		draft = append(draft, fmt.Sprintf("draft line %d", i))
	}
	d := withDraft(t, pushDM(t, pushH), draft...)
	squeezed := previewRoom(d)
	d, _ = streamRows(d, "", squeezed+2)
	d = landed(d, "a block that landed under a tall draft") // stores the cap the tall box leaves

	d = d.WithComposer(d.Composer().Reset()) // sent
	room := previewRoom(d)
	if room < squeezed+4 {
		t.Fatalf("the one-row box leaves %d rows against %d under the draft: too close to tell a stale cap", room, squeezed)
	}
	d, text := streamRows(d, "", room+2)

	frame := frameRows(d)
	got := previewUnder(t, frame, "a block that landed")
	if len(got) != room {
		t.Errorf("the preview drew %d rows after the draft was sent, want the pane's %d", len(got), room)
	}
	if last := got[len(got)-1]; !strings.HasSuffix(last, "answer.") || !strings.Contains(strings.Join(got, "\n"), fmt.Sprintf("Sentence %02d", strings.Count(text, "Sentence "))) {
		t.Errorf("the preview is not the answer's newest words: %q", got)
	}
	if len(frame) != pushH {
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
