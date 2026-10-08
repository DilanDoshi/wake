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
