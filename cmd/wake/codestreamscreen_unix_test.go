//go:build unix

package main

// A tab-indented answer, streamed and then landed, through the real binary on a
// real screen. A tab that reaches the terminal is measured as no cell and drawn
// as up to eight without erasing what it skips: the frame before shows through,
// the row overruns its pane, and a wrap shifts every row below.

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// scriptCodeStream streams a tab-indented Go fence as text deltas, holds the
// preview, then lands it; scriptCodeStreamSpaces is the same answer indented with
// spaces, the frame a tab-free build must draw.
const (
	scriptCodeStream       = "code-stream"
	scriptCodeStreamSpaces = "code-stream-spaces"
)

func fakeAgentCodeStream(sid, indent string) int {
	sayText(sid, "ready")
	sayResult(sid)
	chunks := []string{
		"Here is the helper:\n\n```go\npackage harbor\n\nfunc main() {\n",
		"\tvar h Harbor\n\th.Dock()\n",
		"\tif h.Full() {\n\t\th.Wait()\n\t}\n}\n```\n",
	}
	for line := range agentStdin() {
		if _, ok := userTextOf(line); !ok {
			continue
		}
		var whole strings.Builder
		for _, c := range chunks {
			c = strings.ReplaceAll(c, "\t", indent)
			whole.WriteString(c)
			fmt.Printf(`{"type":"stream_event","session_id":%q,"parent_tool_use_id":null,"event":{"type":"content_block_delta","index":0,`+
				`"delta":{"type":"text_delta","text":%q}}}`+"\n", sid, c)
			time.Sleep(60 * time.Millisecond)
		}
		time.Sleep(2 * time.Second) // the preview stands until the block lands
		sayText(sid, whole.String())
		sayResult(sid)
	}
	return 0
}

func TestAStreamedGoAnswerLeavesAWholeFrameOnARealScreen(t *testing.T) {
	for _, script := range []string{scriptCodeStream, scriptCodeStreamSpaces} {
		t.Run(script, func(t *testing.T) {
			withScriptedAgent(t, script)
			t.Setenv("WAKE_SOCKET", tempSocket(t))
			s := startWakeInAConversation(t, 120, 36)
			s.await("ready")
			s.settle()
			rules := dividerColumns(s)
			if len(rules) == 0 {
				t.Fatalf("no pane divider on a clean frame, so nothing to hold the frame to\n%s", s.dump())
			}
			s.send("write it\r")
			s.await("h.Dock()")
			s.settle()
			wholeFrame(t, s, "while the preview stands", rules)
			s.await("· done ")
			s.settle()
			wholeFrame(t, s, "after the block landed", rules)
			s.awaitCount("package harbor", 1)
			s.awaitCount("h.Wait()", 1)
		})
	}
}

// wholeFrame holds a frame to the one drawn before the answer: the same dividers,
// every code line once.
func wholeFrame(t *testing.T, s *screen, when string, rules []int) {
	t.Helper()
	if got := dividerColumns(s); !slices.Equal(got, rules) {
		t.Errorf("%s: the dividers stand at %v, want %v as before the answer\n%s", when, got, rules, s.dump())
	}
	for _, line := range []string{"var h Harbor", "h.Dock()"} {
		if n := strings.Count(s.text(), line); n != 1 {
			t.Errorf("%s: %q is on screen %d times, want once\n%s", when, line, n, s.dump())
		}
	}
}
