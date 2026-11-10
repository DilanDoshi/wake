package ui

// A streamed answer pushes the conversation up a row at a time while the reader
// follows the newest line, and moves nothing for one who has scrolled back: it is
// the window's last rows (transcript.tail). Everything here reads it off the frame
// the pane draws.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	pushW, pushH = 60, 24
	// pushNewest is the transcript's newest line in pushDM, the row every
	// measurement below hangs off.
	pushNewest = "earlier line 099"
)

// pushDM is a working agent over a transcript that fills the pane, following.
func pushDM(t *testing.T, h int) DM {
	t.Helper()
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	d = d.SetSize(pushW, h)
	for i := range 100 {
		d = d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: fmt.Sprintf("earlier line %03d", i)})
	}
	if !d.tr.atBottom() || d.tr.lines.len() <= h {
		t.Fatalf("the fixture is not a full, following transcript: %d lines in a %d-row pane, at bottom = %v", d.tr.lines.len(), h, d.tr.atBottom())
	}
	return d
}

// sentence is the i'th sentence of a streamed answer: ordinary words, about a
// row each at pushW columns.
func sentence(i int) string {
	return fmt.Sprintf("Sentence %02d adds a few ordinary words to the answer. ", i)
}

// wrappedRows is the answer as the preview lays it out, one string per row.
func wrappedRows(text string) []string {
	if text == "" {
		return nil
	}
	rows := strings.Split(ansi.Wrap(text, max(pushW, minBlockWidth), ""), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	return rows
}

// streamRows streams sentences into d until the answer wraps to at least rows
// rows, returning the DM and the text so far. Each sentence is one partial.
func streamRows(d DM, text string, rows int) (DM, string) {
	for len(wrappedRows(text)) < rows {
		s := sentence(strings.Count(text, "Sentence ") + 1)
		d, text = tokens(d, s), text+s
	}
	return d, text
}

// frameRows is what the pane draws, one string per row.
func frameRows(d DM) []string { return strings.Split(visible(d, pushW, d.height), "\n") }

// previewRoom is about a window of rows in d's pane: the transcript's rows less its
// one-row floor, read off the layout of a pane with no answer in it.
func previewRoom(d DM) int { return d.SetSize(pushW, d.height).tr.height - minTranscriptHeight }

// previewUnder is the run of non-blank rows under the anchor row - the
// transcript's newest line, or the follow banner for a reader who has scrolled
// back. The row after the preview is the working line's blank gap.
func previewUnder(t *testing.T, frame []string, anchor string) []string {
	t.Helper()
	i := slices.IndexFunc(frame, func(l string) bool { return strings.Contains(l, anchor) })
	if i < 0 {
		t.Fatalf("%q is not on screen:\n%s", anchor, strings.Join(frame, "\n"))
	}
	var rows []string
	for _, l := range frame[i+1:] {
		if l == "" {
			break
		}
		rows = append(rows, l)
	}
	return rows
}

// composerRowsDrawn is the box and everything under it, from its top border.
func composerRowsDrawn(t *testing.T, frame []string) int {
	t.Helper()
	i := slices.IndexFunc(frame, func(l string) bool { return strings.Contains(l, "╭") })
	if i < 0 {
		t.Fatalf("no composer box on screen:\n%s", strings.Join(frame, "\n"))
	}
	return len(frame) - i
}

// withDraft types lines into d's composer, a newline (⌃J) before each one after
// the first - and before the first too when the draft already has text.
func withDraft(t *testing.T, d DM, lines ...string) DM {
	t.Helper()
	c := d.Composer()
	for i, l := range lines {
		if i > 0 || c.Value() != "" {
			c, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
		}
		c = typeInto(t, c, l)
	}
	return d.WithComposer(c)
}

// windowOf is the rows of d's frame its transcript window draws, the answer's
// rows at their foot.
func windowOf(d DM) []string {
	return frameRows(d)[:d.laidOut(pushW, d.height).tr.height]
}

// assertFollowsAnswer fails unless d's window ends on the answer's newest rows,
// as many of them as it holds: the newest one on the window's last row.
func assertFollowsAnswer(t *testing.T, what string, d DM, text string) {
	t.Helper()
	window, wrapped := windowOf(d), wrappedRows(text)
	n := min(len(wrapped), len(window))
	if got, want := window[len(window)-n:], wrapped[len(wrapped)-n:]; !slices.Equal(got, want) {
		t.Errorf("%s: the window ends\n%s\nwant the answer's newest %d rows\n%s", what, strings.Join(got, "\n"), n, strings.Join(want, "\n"))
	}
}

// A reader who follows the newest line is shown the answer as it is written, and
// every row of it pushes the conversation up one row - Claude Code's own
// behaviour - the answer under the conversation's last line with the blank row
// a block keeps from the one above it.
func TestAStreamedAnswerPushesTheTranscriptUpAsItGrows(t *testing.T) {
	d := pushDM(t, pushH)
	newestAt := slices.IndexFunc(frameRows(d), func(l string) bool { return strings.Contains(l, pushNewest) })
	if newestAt < 0 {
		t.Fatal("the transcript's newest line is not on screen before the answer starts")
	}

	text := ""
	for rows := 1; rows <= len(windowOf(d))+2; rows++ {
		d, text = streamRows(d, text, rows)
		wrapped, frame := wrappedRows(text), frameRows(d)
		if len(frame) != pushH {
			t.Fatalf("a %d-row answer drew %d rows in a %d-row pane", len(wrapped), len(frame), pushH)
		}
		assertFollowsAnswer(t, fmt.Sprintf("a %d-row answer", len(wrapped)), d, text)
		if want := newestAt - len(wrapped) - 1; want >= 0 {
			if at := slices.IndexFunc(frame, func(l string) bool { return strings.Contains(l, pushNewest) }); at != want {
				t.Errorf("a %d-row answer put the transcript's newest line on row %d, want %d: each streamed row pushes it up one", len(wrapped), at, want)
			}
		}
	}
}

// A reader who has scrolled back is reading what is on screen, and the answer
// growing under them moves none of it.
func TestAScrolledBackReaderSeesNothingMoveWhileAnAnswerStreams(t *testing.T) {
	d := pushDM(t, pushH)
	d, text := streamRows(d, "", previewRoom(d)) // following: the answer is large when the reader leaves
	d = d.ScrollUp(3)
	reading := frameRows(d)
	for i := range 8 {
		d, text = streamRows(d, text, len(wrappedRows(text))+1)
		if got := frameRows(d); !slices.Equal(got[:len(windowOf(d))], reading[:len(windowOf(d))]) {
			t.Fatalf("step %d: the window moved under a reader who scrolled back:\n%s\nwas\n%s", i, strings.Join(got, "\n"), strings.Join(reading, "\n"))
		}
	}
}

// Every way back to the newest line follows the answer again, mid-stream: the
// wheel, a click on the banner, ⌃E - which is also a re-render - a subagent's
// view and back, and a restore. The reader leaves while one answer streams, a
// block lands behind them and the next answer starts.
func TestEveryReturnToTheNewestLineFollowsTheAnswerAgain(t *testing.T) {
	for _, tc := range []struct {
		name string
		back func(DM) DM
	}{
		{"wheel down", func(d DM) DM { return d.ScrollUp(-1000) }},
		{"follow banner click", DM.JumpToLatest},
		{"ctrl-e", DM.toggleExpanded},
		{"subagent view", func(d DM) DM { return d.Viewing("dispatch-1").Viewing("") }},
		{"restore", func(d DM) DM {
			return d.Before([]core.Event{{Kind: core.KindAssistantText, SessionID: "s1", Text: "an older block"}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pushDM(t, pushH)
			d, _ = streamRows(d, "", previewRoom(d))
			d = d.ScrollUp(10)
			d = d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "a block that landed behind the reader"})
			d, text := streamRows(d, "", minPreviewRows+2)
			d = d.SetSize(pushW, pushH) // a resize lands while they read

			d = tc.back(d)
			if !d.tr.atBottom() {
				t.Fatal("the way back did not return to the newest line")
			}
			d, text = streamRows(d, text, len(wrappedRows(text))+previewRoom(d)+2)
			assertFollowsAnswer(t, "back at the newest line", d, text)
			if frame := frameRows(d); len(frame) != pushH {
				t.Errorf("the pane drew %d rows, want %d", len(frame), pushH)
			}
		})
	}
}

// The draft wins. A line added to it takes a row from the window, never from the
// box: a composer frozen at the size it had when the answer filled the pane would
// wrap a growing draft inside a fixed box.
func TestTheDraftWinsOverAnAnswerFillingThePane(t *testing.T) {
	d := withDraft(t, pushDM(t, pushH), "first line")
	d, text := streamRows(d, "", previewRoom(d)+2)
	boxBefore := composerRowsDrawn(t, frameRows(d))

	d = withDraft(t, d, "second line") // ⌃J then more text: one more row in the box
	after := frameRows(d)
	if len(after) != pushH {
		t.Fatalf("a draft line under an answer filling the pane drew %d rows in a %d-row pane", len(after), pushH)
	}
	if got := composerRowsDrawn(t, after); got != boxBefore+1 {
		t.Errorf("the composer drew %d rows after a draft line, want %d: the answer froze it", got, boxBefore+1)
	}
	assertFollowsAnswer(t, "after a draft line", d, text)
}

// A menu is answered by typing at the box, so it takes its rows from the window
// the moment it opens, and the answer's newest row stays on the window's last.
func TestAMenuTakesItsRowsFromAnAnswerFillingThePane(t *testing.T) {
	d := pushDM(t, pushH)
	d, text := streamRows(d, "", previewRoom(d)+2)
	d = d.SetSize(pushW, pushH)

	var menu []string
	for i := 1; i <= 5; i++ {
		menu = append(menu, fmt.Sprintf("menu row %d", i))
	}
	d = d.WithMenu(strings.Join(menu, "\n"))
	frame := frameRows(d)
	if len(frame) != pushH {
		t.Fatalf("a menu over an answer filling the pane drew %d rows in a %d-row pane", len(frame), pushH)
	}
	for _, row := range menu {
		if !slices.ContainsFunc(frame, func(l string) bool { return strings.Contains(l, row) }) {
			t.Errorf("%q is not drawn: the answer kept its rows and the menu was clipped:\n%s", row, strings.Join(frame, "\n"))
		}
	}
	assertFollowsAnswer(t, "under a menu", d, text)
}

// The block that supersedes a long preview lands where the preview was: the
// transcript stays pinned to its newest line, nothing is left under it, and the
// pane is exactly its height on the frame the preview left.
func TestTheFinishedBlockReplacesAFullPreviewInPlace(t *testing.T) {
	end := func(d DM) DM { return d.Append(core.Event{Kind: core.KindTurnEnd, SessionID: "s1"}) }
	land := func(d DM) DM {
		return d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "the finished answer"})
	}
	for _, tc := range []struct {
		name, newest string
		finish       func(DM) DM
	}{
		{"block lands", "the finished answer", land},
		{"interrupted turn", pushNewest, end},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pushDM(t, pushH)
			d, _ = streamRows(d, "", previewRoom(d)+2)

			d = tc.finish(d)
			frame := frameRows(d)
			if len(frame) != pushH {
				t.Fatalf("the pane drew %d rows once the preview was replaced, want %d", len(frame), pushH)
			}
			if !d.tr.atBottom() {
				t.Error("the transcript is not at its newest line")
			}
			if got := previewUnder(t, frame, tc.newest); len(got) != 0 {
				t.Errorf("the preview left %d rows under the transcript:\n%s", len(got), strings.Join(got, "\n"))
			}
			if strings.Contains(strings.Join(frame, "\n"), "Sentence") {
				t.Errorf("the preview's words are still on screen:\n%s", strings.Join(frame, "\n"))
			}
		})
	}
}

// The pane is exactly its height at every size the preview can be, in every
// pane from the tightest one up, following or not, under any draft and whatever
// else is pinned around the box - a frame one row too tall scrolls the alt
// screen away on every draw.
func TestThePaneIsItsHeightAtEveryPreviewSize(t *testing.T) {
	base := pushDM(t, pushH)
	for name, chrome := range map[string]func(DM) DM{
		"bare": func(d DM) DM { return d },
		"checklist": func(d DM) DM {
			return d.Append(taskCreate("write the plan", "writing")).Append(taskCreate("run the checks", "running"))
		},
		"queue":      func(d DM) DM { return d.WithQueued([]string{"a message typed ahead", "and another one"}) },
		"menu":       func(d DM) DM { return d.WithMenu("menu row one\nmenu row two\nmenu row three") },
		"card":       func(d DM) DM { return d.WithMenu("a card\nwith a question\nand two options") },
		"no working": func(d DM) DM { d.Agent.State = rpc.StateIdle; return d },
	} {
		fresh := func() DM { return chrome(base.WithComposer(NewComposer())) }
		floor := fresh().SetSize(pushW, pushH).minHeight()
		for h := floor; h <= floor+24; h++ {
			for _, scrolled := range []bool{false, true} {
				for _, draft := range [][]string{nil, {"one", "two", "three"}} {
					d := withDraft(t, fresh().SetSize(pushW, h), draft...)
					if scrolled {
						d = d.ScrollUp(3)
					}
					text := ""
					for rows := 0; rows <= 22; rows += 3 {
						d, text = streamRows(d, text, rows)
						if got := lipgloss.Height(d.View(pushW, h)); got != h {
							t.Fatalf("%s h=%d scrolled=%v draft=%d lines, %d-row answer: the pane drew %d rows", name, h, scrolled, len(draft), len(wrappedRows(text)), got)
						}
					}
				}
			}
		}
	}
}
