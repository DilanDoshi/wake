package ui

// A local command's reply - /list-agents, /config, /model, /mcp - is text whose
// line breaks carry it: a row per session, per setting. Claude Code draws such
// output as its lines, and markdown would fold them into one paragraph.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// localReplies is every local command's reply text a recorded stream holds.
func localReplies(t *testing.T, name string) []core.Event {
	t.Helper()
	var out []core.Event
	for _, ev := range decodeWorkflowFixture(t, name) {
		if ev.Kind == core.KindAssistantText && ev.LocalCommand {
			out = append(out, ev)
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s holds no local command's reply, so this asserts nothing", name)
	}
	return out
}

// drawnRows is a block's rows as a reader sees them, trailing padding dropped.
func drawnRows(block string) []string {
	rows := strings.Split(ansi.Strip(block), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	return rows
}

// Each line of the reply is a row of its own, its indentation and its column
// alignment kept, one margin in like every other block.
func TestALocalCommandsReplyKeepsItsLines(t *testing.T) {
	const width = 200 // wider than any recorded line, so nothing here wraps
	margin := strings.Repeat(" ", bodyIndent)
	for _, name := range []string{"list-agents-bare.jsonl", "list-agents.jsonl", "bare-config.jsonl", "bare-model.jsonl", "bare-mcp.jsonl"} {
		for _, ev := range localReplies(t, name) {
			rows := drawnRows(NewDM("s1", "sydney").kindBlock(ev, width))
			at := 0
			for _, line := range strings.Split(strings.TrimSpace(ev.Text), "\n") {
				want := strings.TrimRight(margin+line, " ")
				if line == "" {
					want = ""
				}
				for at < len(rows) && rows[at] != want {
					at++
				}
				if at == len(rows) {
					t.Errorf("%s: no row, in order, draws %q as it was printed:\n%s", name, line, strings.Join(rows, "\n"))
					break
				}
				at++
			}
		}
	}
}

// /context's reply is a markdown document - headings, bold, tables - so it is
// still drawn as markdown.
func TestContextsMarkdownReplyIsStillRendered(t *testing.T) {
	var reply core.Event
	for _, ev := range localReplies(t, "slash-commands.jsonl") {
		if strings.HasPrefix(ev.Text, "## ") {
			reply = ev
		}
	}
	if reply.Text == "" {
		t.Fatal("slash-commands.jsonl holds no markdown reply, so this asserts nothing")
	}
	drawn := ansi.Strip(NewDM("s1", "sydney").kindBlock(reply, 120))
	if strings.Contains(drawn, "## ") || strings.Contains(drawn, "**") {
		t.Errorf("/context's reply was drawn as its markdown source:\n%s", drawn)
	}
}

// A row longer than the pane wraps inside it, as every block must: an over-wide
// line shoves the sidebars out of place.
func TestALongLocalReplyRowWrapsInsideThePane(t *testing.T) {
	const width = 30
	ev := core.Event{Kind: core.KindAssistantText, LocalCommand: true,
		Text: "Other Claude sessions (1):\n  [idle]  ·  release-notes  ·  /private/tmp/a/very/deep/project  ·  started 3s ago"}
	for _, row := range strings.Split(NewDM("s1", "sydney").kindBlock(ev, width), "\n") {
		if w := lipgloss.Width(row); w > width {
			t.Errorf("a row is %d cells in a %d-cell pane: %q", w, width, ansi.Strip(row))
		}
	}
}

// A copy gives back the rows the reply printed, not one joined paragraph.
func TestCopyingALocalReplyGivesBackItsRows(t *testing.T) {
	fresh(t)
	ev := localReplies(t, "list-agents-bare.jsonl")[0]
	ev.SessionID = "s1"
	dm := resized(t, dmApp(nil, Stream{}, "s1", "alex"), 120, 40)
	dm = dm.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
	tr := dm.transcriptIn("s1")
	got := copyOf(t, dm, "s1", lineHolding(t, tr, "Other Claude sessions"), 0, tr.lines.len()-1, tr.width-1)
	if want := strings.TrimSpace(ev.Text); got != want {
		t.Errorf("the DM copied\n%q\nwant\n%q", got, want)
	}
}
