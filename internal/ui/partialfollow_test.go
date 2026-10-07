package ui

// A streamed answer pushes the conversation up a row at a time while the reader
// follows the newest line, and moves nothing for one who has scrolled back.
// partial.go's previewCap carries the rule; everything here reads it off the
// frame the pane draws, so a cap and a layout cannot agree on the same wrong row.

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

// previewRoom is the rows a preview may take in d's pane: the transcript's rows less
// its one-row floor, read off the layout of a pane with no preview in it.
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

// A reader who follows the newest line is shown the answer as it is written, and
// every row of it pushes the conversation up one row - Claude Code's own
// behaviour - rather than the answer scrolling inside a three-row box at the
// bottom of a transcript that never moves, its start cut off.
func TestAStreamedAnswerPushesTheTranscriptUpAsItGrows(t *testing.T) {
	d := pushDM(t, pushH)
	room := previewRoom(d)
	if room <= minPreviewRows+2 {
		t.Fatalf("a %d-row pane leaves %d rows for a preview: too tight to tell the cap from the floor", pushH, room)
	}
	newestAt := slices.IndexFunc(frameRows(d), func(l string) bool { return strings.Contains(l, pushNewest) })
	if newestAt < 0 {
		t.Fatal("the transcript's newest line is not on screen before the answer starts")
	}

	text := ""
	for rows := 1; rows <= room+2; rows++ {
		d, text = streamRows(d, text, rows)
		wrapped := wrappedRows(text)
		want := wrapped[max(len(wrapped)-room, 0):] // the pane's rows, never the block's
		frame := frameRows(d)

		if len(frame) != pushH {
			t.Fatalf("a %d-row answer drew %d rows in a %d-row pane", len(wrapped), len(frame), pushH)
		}
		if got := previewUnder(t, frame, pushNewest); !slices.Equal(got, want) {
			t.Fatalf("a %d-row answer, ceiling %d: the preview draws\n%s\nwant its newest %d rows\n%s",
				len(wrapped), room, strings.Join(got, "\n"), len(want), strings.Join(want, "\n"))
		}
		if at := slices.IndexFunc(frame, func(l string) bool { return strings.Contains(l, pushNewest) }); at != newestAt-len(want) {
			t.Errorf("a %d-row preview put the transcript's newest line on row %d, want %d: each streamed row pushes it up one",
				len(want), at, newestAt-len(want))
		}
		if len(wrapped) <= room && !strings.Contains(strings.Join(frame, "\n"), wrapped[0]) {
			t.Errorf("a %d-row answer fits and its start %q is not on screen", len(wrapped), wrapped[0])
		}
	}
}

// A reader who has scrolled back is reading what is on screen, and a floor of
// three rows is what lets the answer be seen without moving a line of it.
func TestAScrolledBackReaderKeepsTheFloorWhileAnAnswerStreams(t *testing.T) {
	d := pushDM(t, pushH)
	room := previewRoom(d)
	d, text := streamRows(d, "", room) // following: the answer is large when the reader leaves
	d = d.ScrollUp(3)

	top := ""
	for i := range 8 {
		d, text = streamRows(d, text, len(wrappedRows(text))+1)
		frame := frameRows(d)
		if len(frame) != pushH {
			t.Fatalf("step %d: the scrolled-back pane drew %d rows in a %d-row pane", i, len(frame), pushH)
		}
		if got := previewUnder(t, frame, followBannerText); len(got) != minPreviewRows {
			t.Fatalf("step %d: a scrolled-back reader's preview drew %d rows, want the floor of %d:\n%s", i, len(got), minPreviewRows, strings.Join(frame, "\n"))
		}
		if i == 0 {
			top = frame[0]
		} else if frame[0] != top {
			t.Fatalf("step %d: the transcript's top row moved from %q to %q under a reader who scrolled back", i, top, frame[0])
		}
	}
}

// Every way back to the newest line gives the preview its room again, mid-stream:
// the wheel, a click on the banner, ⌃E - which is also a re-render, and the one
// key from the symptom - a subagent's view and back, and a restore. The reader leaves while one answer streams, a block
// lands behind them and the next answer starts, so the preview the way back finds
// has been held at the floor by everything that measured it since.
func TestEveryReturnToTheNewestLineRestoresThePreviewsRoom(t *testing.T) {
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
			room := previewRoom(d)
			d, _ = streamRows(d, "", room)
			d = d.ScrollUp(3)
			d = d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "a block that landed behind the reader"})
			d, text := streamRows(d, "", minPreviewRows+2)
			d = d.SetSize(pushW, pushH) // a resize lands while they read, settling the stored chrome
			if got := previewUnder(t, frameRows(d), followBannerText); len(got) != minPreviewRows {
				t.Fatalf("scrolled back, the preview drew %d rows, want %d", len(got), minPreviewRows)
			}

			d = tc.back(d)
			if !d.tr.atBottom() {
				t.Fatal("the way back did not return to the newest line")
			}
			// Tokens keep arriving; the preview regrows to the pane's room rather
			// than staying in the floor's box.
			d, text = streamRows(d, text, len(wrappedRows(text))+room+2)
			frame := frameRows(d)
			if got := previewUnder(t, frame, "a block that landed"); len(got) != room {
				t.Errorf("back at the newest line the preview drew %d rows of an answer %d rows long, want the pane's %d", len(got), len(wrappedRows(text)), room)
			}
			if len(frame) != pushH {
				t.Errorf("the pane drew %d rows, want %d", len(frame), pushH)
			}
		})
	}
}

// The draft wins. A line added to it takes a row from the preview, never from
// the box: a composer frozen at the size it had when the preview filled the
// pane would wrap a growing draft inside a fixed box.
func TestTheDraftWinsOverAFullPreview(t *testing.T) {
	d := withDraft(t, pushDM(t, pushH), "first line")
	room := previewRoom(d)
	d, text := streamRows(d, "", room+2)

	before := frameRows(d)
	if got := previewUnder(t, before, pushNewest); len(got) != room {
		t.Fatalf("the preview drew %d rows, want the pane's %d before the draft grows", len(got), room)
	}
	boxBefore := composerRowsDrawn(t, before)

	d = withDraft(t, d, "second line") // ⌃J then more text: one more row in the box
	after := frameRows(d)
	if len(after) != pushH {
		t.Fatalf("a draft line under a full preview drew %d rows in a %d-row pane", len(after), pushH)
	}
	if got := composerRowsDrawn(t, after); got != boxBefore+1 {
		t.Errorf("the composer drew %d rows after a draft line, want %d: the preview froze it", got, boxBefore+1)
	}
	wrapped := wrappedRows(text)
	want := wrapped[len(wrapped)-(room-1):]
	if got := previewUnder(t, after, pushNewest); !slices.Equal(got, want) {
		t.Errorf("after a draft line the preview draws\n%s\nwant its newest %d rows\n%s", strings.Join(got, "\n"), room-1, strings.Join(want, "\n"))
	}
}

// A menu is answered by typing at the box, so it takes its rows from the preview
// the moment it opens - even when the preview fills the pane and the menu's
// opening leaves the chrome's sum exactly where it was.
func TestAMenuTakesItsRowsBackFromAFullPreview(t *testing.T) {
	d := pushDM(t, pushH)
	room := previewRoom(d)
	d, _ = streamRows(d, "", room+2)
	// A resize lands mid-answer, so the stored chrome is the full preview's - the
	// state a menu's own rows and the composerGap it drops cancel against.
	d = d.SetSize(pushW, pushH)

	var menu []string
	for i := 1; i <= 5; i++ {
		menu = append(menu, fmt.Sprintf("menu row %d", i))
	}
	d = d.WithMenu(strings.Join(menu, "\n"))
	frame := frameRows(d)

	if len(frame) != pushH {
		t.Fatalf("a menu over a full preview drew %d rows in a %d-row pane", len(frame), pushH)
	}
	for _, row := range menu {
		if !slices.ContainsFunc(frame, func(l string) bool { return strings.Contains(l, row) }) {
			t.Errorf("%q is not drawn: the preview kept its rows and the menu was clipped:\n%s", row, strings.Join(frame, "\n"))
		}
	}
	if got := previewUnder(t, frame, pushNewest); len(got) != minPreviewRows {
		t.Errorf("under a menu the preview drew %d rows, want the floor of %d", len(got), minPreviewRows)
	}
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
// pane from the tightest one up, following or not and under any draft - a frame
// one row too tall scrolls the alt screen away on every draw.
func TestThePaneIsItsHeightAtEveryPreviewSize(t *testing.T) {
	base := pushDM(t, pushH)
	floor := base.minHeight()
	for h := floor; h <= floor+30; h++ {
		for _, scrolled := range []bool{false, true} {
			for _, draft := range [][]string{nil, {"one", "two", "three"}} {
				d := withDraft(t, base.WithComposer(NewComposer()).SetSize(pushW, h), draft...)
				if scrolled {
					d = d.ScrollUp(3)
				}
				text := ""
				for rows := 0; rows <= 22; rows += 3 {
					d, text = streamRows(d, text, rows)
					if got := lipgloss.Height(d.View(pushW, h)); got != h {
						t.Fatalf("h=%d scrolled=%v draft=%d lines, %d-row answer: the pane drew %d rows", h, scrolled, len(draft), len(wrappedRows(text)), got)
					}
				}
			}
		}
	}
}
