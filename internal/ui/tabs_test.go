package ui

// No tab reaches the terminal through the streamed preview. ansi.StringWidth
// counts a tab as no cell and a terminal moves to the next eight-column stop
// without erasing what it skips: a row holding one shows the frame before it
// through the gap, overruns its pane and, past the terminal's width, wraps and
// shifts every row below.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A Go answer streams tab-indented, and a tab can fall in a later token than the
// text before it on the line: it still runs to the line's next four-column stop.
func TestAStreamedPreviewDrawsNoTab(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents("alex").WithOpenDM("s1", "alex").withSize(120, 40)
	for _, chunk := range []string{"```go\nfunc main() {\n", "\tvar h Harbor\n\tx := 1", "\t// aligned\n\th.Dock()\n}\n```\n"} {
		ev := core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: chunk}
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
	}
	view := a.View()
	for i, row := range strings.Split(view, "\n") {
		if strings.ContainsRune(row, '\t') {
			t.Errorf("frame row %d holds a tab: %q", i, stripANSI(row))
		}
	}
	if want := "    x := 1  // aligned"; !strings.Contains(stripANSI(view), want) {
		t.Errorf("the tab after `x := 1` did not run to the line's next stop of four (%q):\n%s", want, stripANSI(view))
	}
}

// The preview keeps only its tail, so the line a tab lands on may have lost its
// start; the tab still runs to the stop its whole line puts it at. One cell past a
// multiple of four, the line's tab is three spaces wide, whatever was cut.
func TestATabAfterTheTrimRunsToItsLinesStop(t *testing.T) {
	p := partial{width: minBlockWidth, cap: minPreviewRows}
	keep := previewChars(p.width, p.cap)
	head := strings.Repeat("a", (keep/4+1)*4+1) // a line longer than the tail, one past a stop
	p = p.add(head)
	if len(p.text) >= len(head) {
		t.Fatalf("the tail kept %d of %d bytes: the line's start was never cut, so this asserts nothing", len(p.text), len(head))
	}
	p = p.add("\tb")
	if !strings.HasSuffix(p.text, "a   b") {
		t.Errorf("the tab after a trimmed line ran to the wrong stop: the tail ends %q, want three spaces", p.text[len(p.text)-8:])
	}
}

// A block that lands takes its line with it: the next block's first tab starts
// from column zero, not from where the last one's line stopped.
func TestAClearedPreviewStartsItsColumnAtZero(t *testing.T) {
	p := partial{width: minBlockWidth, cap: minPreviewRows}.add("ab").cleared().add("\tx")
	if p.text != "    x" {
		t.Errorf("the next block's tab ran from the last block's column: %q, want four spaces", p.text)
	}
}
