package ui

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// email is the owner's report: a message with paragraphs, a list and a sign-off,
// long enough that every pane below wraps it.
const email = "Hi Sam,\n\nThanks for getting back to me so quickly about the quarterly planning review. " +
	"I wanted to follow up on a few items we discussed during Tuesday's meeting.\n\n" +
	"- First, the budget numbers need a second look before Friday.\n" +
	"- Second, the vendor contract renewal is due at the end of the month."

// emailCopied is what pasting it should give back: markdown drew the list, so
// the bullets are the drawn ones, but no wrap and no margin survives.
const emailCopied = "Hi Sam,\n\nThanks for getting back to me so quickly about the quarterly planning review. " +
	"I wanted to follow up on a few items we discussed during Tuesday's meeting.\n\n" +
	"• First, the budget numbers need a second look before Friday.\n" +
	"• Second, the vendor contract renewal is due at the end of the month."

// copyOf releases a selection from (fromLine, fromCol) to (toLine, toCol) in a
// pane and returns what reached the clipboard, through the real release path.
func copyOf(t *testing.T, a App, pane string, fromLine, fromCol, toLine, toCol int) string {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	a.sel = selection{pane: pane, anchor: point{line: fromLine, col: fromCol}, head: point{line: toLine, col: toCol}}
	a.selecting = true
	_, cmd := a.endSelection()
	if cmd == nil {
		t.Fatal("the release copied nothing")
	}
	seq := cmd().(copiedMsg).seq
	text, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\a"))
	if err != nil {
		t.Fatalf("the clipboard sequence %q does not decode: %v", seq, err)
	}
	return string(text)
}

// lineHolding is the first transcript row whose text contains s.
func lineHolding(t *testing.T, tr transcript, s string) int {
	t.Helper()
	for i := tr.lines.first(); i < tr.lines.len(); i++ {
		if strings.Contains(ansi.Strip(tr.lines.at(i)), s) {
			return i
		}
	}
	t.Fatalf("no row holds %q", s)
	return 0
}

func resized(t *testing.T, a App, w, h int) App {
	t.Helper()
	m, _ := a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.(App)
}

// Mutation check: passing nil rejoins from endSelection (the old copy) fails
// both at the first wrapped row.
func TestCopyingAnAgentsEmailGivesBackItsParagraphs(t *testing.T) {
	fresh(t)
	ev := core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: email}

	dm := resized(t, dmApp(nil, Stream{}, "s1", "alex"), 90, 40)
	dm = dm.applyFrame(eventFrame("s1", email))
	tr := dm.transcriptIn("s1")
	got := copyOf(t, dm, "s1", lineHolding(t, tr, "Hi Sam"), 0, tr.lines.len()-1, tr.width-1)
	if want := emailCopied; got != want {
		t.Errorf("the DM copied\n%q\nwant\n%q", got, want)
	}

	room := NewRoomApp(nil, Stream{}, nil)
	room.layout.ShowGroups, room.layout.ShowRoster = false, false
	room = resized(t, room, 70, 40)
	m, _ := room.Update(eventMsg{Event: ev})
	room = m.(App)
	tr = room.transcriptIn("")
	// From the speaker's name down: the head is a row of its own, never prose.
	head := lineHolding(t, tr, "Hi Sam") - 1
	name := strings.TrimSpace(ansi.Strip(tr.lines.at(head)))
	got = copyOf(t, room, "", head, 0, tr.lines.len()-1, tr.width-1)
	if want := name + "\n" + emailCopied; got != want {
		t.Errorf("the room copied\n%q\nwant\n%q", got, want)
	}
}

// A drag that starts and ends mid-paragraph takes the words between, joined
// across the wrap by the space the wrap consumed.
func TestAPartialDragAcrossAWrapTakesTheWordsBetween(t *testing.T) {
	fresh(t)
	dm := resized(t, dmApp(nil, Stream{}, "s1", "alex"), 90, 40)
	dm = dm.applyFrame(eventFrame("s1", email))
	tr := dm.transcriptIn("s1")
	row := lineHolding(t, tr, "Thanks for")
	first := ansi.Strip(tr.lines.at(row))
	from := strings.Index(first, "quarterly")
	to := strings.Index(ansi.Strip(tr.lines.at(row+1)), "follow") + len("follow") - 1
	got := copyOf(t, dm, "s1", row, from, row+1, to)
	if want := "quarterly planning review. I wanted to follow"; got != want {
		t.Errorf("the drag copied %q, want %q", got, want)
	}
}

// typedEmail is the same email pasted into the composer by the operator: their
// own turn is drawn as typed, so it copies back exactly as typed.
const typedEmail = "Hi Sam,\n\nThanks for getting back to me so quickly about the quarterly planning review. " +
	"I wanted to follow up on a few items.\n\nBest regards,\nDilan"

func TestCopyingYourOwnTurnGivesBackWhatYouTyped(t *testing.T) {
	fresh(t)
	room := NewRoomApp(nil, Stream{}, nil)
	room.layout.ShowGroups, room.layout.ShowRoster = false, false
	room = resized(t, room, 50, 40)
	m, _ := room.Update(eventMsg{Event: core.Event{Kind: core.KindUserText, SessionID: "s1", Text: typedEmail}})
	room = m.(App)
	tr := room.transcriptIn("")
	got := copyOf(t, room, "", lineHolding(t, tr, "Hi Sam"), 0, tr.lines.len()-1, tr.width-1)
	if want := typedEmail; got != want {
		t.Errorf("the room copied\n%q\nwant\n%q", got, want)
	}

	dm := resized(t, dmApp(nil, Stream{}, "s1", "alex"), 60, 40)
	m, _ = dm.Update(eventMsg{Event: core.Event{Kind: core.KindUserText, SessionID: "s1", Text: typedEmail}})
	dm = m.(App)
	tr = dm.transcriptIn("s1")
	head := lineHolding(t, tr, "Hi Sam") - 1 // the DM's "you" label, which is not what was typed
	label := strings.TrimSpace(ansi.Strip(tr.lines.at(head)))
	got = copyOf(t, dm, "s1", head, 0, tr.lines.len()-1, tr.width-1)
	if want := label + "\n" + typedEmail; got != want {
		t.Errorf("the DM copied\n%q\nwant\n%q", got, want)
	}
}

// A tab the operator typed is drawn as spaces, so their turn still matches what
// they typed and rejoins across its wraps, the tab coming back as the spaces
// drawn for it rather than the block falling back to its row breaks.
func TestYourOwnTurnWithATabStillRejoins(t *testing.T) {
	fresh(t)
	typed := "Run\tthis first: the quick brown fox jumps over the lazy dog again and again until it wraps"
	room := NewRoomApp(nil, Stream{}, nil)
	room.layout.ShowGroups, room.layout.ShowRoster = false, false
	room = resized(t, room, 50, 40)
	m, _ := room.Update(eventMsg{Event: core.Event{Kind: core.KindUserText, SessionID: "s1", Text: typed}})
	room = m.(App)
	tr := room.transcriptIn("")
	got := copyOf(t, room, "", lineHolding(t, tr, "Run"), 0, tr.lines.len()-1, tr.width-1)
	if want := strings.ReplaceAll(typed, "\t", strings.Repeat(" ", ownTabWidth)); got != want {
		t.Errorf("the room copied\n%q\nwant\n%q", got, want)
	}
}

// Below minBlockWidth a block is drawn wider than its pane and clipped, so the
// rows hold text nobody saw. Rejoining their visible halves would present the
// gap as continuous text; the span copies as drawn instead.
func TestAClippedBlockCopiesAsDrawn(t *testing.T) {
	para := render.Markdown(strings.Repeat("words that wrap ", 8), minBlockWidth)
	tr := transcript{}.sized(minBlockWidth/2, 20).add(block{text: para, copied: markdownRows})
	m := marked{from: point{line: 0}, to: point{line: tr.lines.len() - 1, col: lineEnd}}
	lines, first := tr.selectionLines(m)
	for i, j := range tr.rejoins(lines, first) {
		if j != hardBreak {
			t.Errorf("row %d of a clipped block rejoins as %+v, want it kept as drawn", i, j)
		}
	}
}

// A reply that opens with a fence sits directly under the speaker's name, with
// no blank row between. The name is not the document, so it must not pull the
// fence's shared lead to zero and leave glamour's margin on the code.
func TestARoomReplyOpeningWithAFenceCopiesItsCode(t *testing.T) {
	fresh(t)
	room := NewRoomApp(nil, Stream{}, nil)
	room.layout.ShowGroups, room.layout.ShowRoster = false, false
	room = resized(t, room, 70, 40)
	m, _ := room.Update(eventMsg{Event: core.Event{Kind: core.KindAssistantText, SessionID: "s1",
		Text: "```python\ndef f():\n    return 1\n```"}})
	room = m.(App)
	tr := room.transcriptIn("")
	head := lineHolding(t, tr, "def f") - 1
	name := strings.TrimSpace(ansi.Strip(tr.lines.at(head)))
	got := copyOf(t, room, "", head, 0, tr.lines.len()-1, tr.width-1)
	if want := name + "\ndef f():\n    return 1"; got != want {
		t.Errorf("the room copied\n%q\nwant\n%q", got, want)
	}
}
