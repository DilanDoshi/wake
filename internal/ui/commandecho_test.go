package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// A slash command the operator types is passed through to the agent, but its
// echo used to draw as a turn of prose - the whole command sitting in the chat
// history like something you said. It reads as a called command instead: a
// compact invocation line, the way Claude Code shows a skill or command you ran.

func TestCommandInvocationRecognizesTypedCommands(t *testing.T) {
	cases := map[string]struct {
		text string
		word string
		ok   bool
	}{
		"a skill":               {"/complete-linear-ticket", "complete-linear-ticket", true},
		"a command with an arg": {"/effort max", "effort", true},
		"a hyphenated name":     {"/deploy-prod", "deploy-prod", true},
		"a namespaced command":  {"/codex:rescue", "codex:rescue", true},
		"a plugin skill":        {"/superpowers:brainstorming", "superpowers:brainstorming", true},
		"an underscore name":    {"/deploy_prod", "deploy_prod", true},
		"leading whitespace":    {"  /rename bob ", "rename", true},
		"a newline after it":    {"/foo\nbar", "foo", true},
		"a dotted name is not":  {"/foo.md is the file", "", false},
		"a path is not one":     {"/usr/local/bin", "", false},
		"a path with an arg":    {"/etc/hosts is here", "", false},
		"a slash mid-sentence":  {"look at /foo", "", false},
		"prose":                 {"not a command", "", false},
		"a bare slash":          {"/", "", false},
		"a double slash":        {"//x", "", false},
		"a digit start":         {"/123go", "", false},
		"empty":                 {"", "", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			word, ok := commandInvocation(c.text)
			if ok != c.ok || word != c.word {
				t.Errorf("commandInvocation(%q) = (%q, %v), want (%q, %v)", c.text, word, ok, c.word, c.ok)
			}
		})
	}
}

func TestATypedCommandRendersAsAnInvocationLineNotProse(t *testing.T) {
	const w = 60
	cmd := userBlock(core.Event{Kind: core.KindUserText, Text: "/complete-linear-ticket"}, w)
	if !strings.Contains(cmd, "complete-linear-ticket") {
		t.Errorf("the invocation line does not name the command:\n%s", cmd)
	}
	if !strings.Contains(cmd, commandGlyph) {
		t.Errorf("the invocation line carries no command glyph %q:\n%s", commandGlyph, cmd)
	}
	if strings.Contains(cmd, userLabel) {
		t.Errorf("a typed command is still headed as a prose turn (%q):\n%s", userLabel, cmd)
	}

	// A genuine sentence is unchanged: it keeps the prose head and no glyph.
	prose := userBlock(core.Event{Kind: core.KindUserText, Text: "please ship the fix"}, w)
	if !strings.Contains(prose, userLabel) {
		t.Errorf("an ordinary turn lost its %q head:\n%s", userLabel, prose)
	}
	if strings.Contains(prose, commandGlyph) {
		t.Errorf("an ordinary turn drew a command glyph:\n%s", prose)
	}
}

func TestATypedCommandInTheRoomRendersAsAnInvocationLine(t *testing.T) {
	const w = 60
	cmd := youSaid("/complete-linear-ticket", w)
	if !strings.Contains(cmd, "complete-linear-ticket") {
		t.Errorf("the room invocation line does not name the command:\n%s", cmd)
	}
	if !strings.Contains(cmd, commandGlyph) {
		t.Errorf("the room invocation line carries no command glyph %q:\n%s", commandGlyph, cmd)
	}

	prose := youSaid("who is stuck?", w)
	if strings.Contains(prose, commandGlyph) {
		t.Errorf("an ordinary room turn drew a command glyph:\n%s", prose)
	}
}

// Only a turn the operator typed reads as an invocation. A replayed frame and a
// prompt an agent handed a subagent keep their own heads, even when the text
// happens to open with a slash.
func TestAReplayedOrSubagentSlashStaysProse(t *testing.T) {
	const w = 60
	replayed := userBlock(core.Event{Kind: core.KindUserText, Text: "/foo bar", Echoed: true}, w)
	if strings.Contains(replayed, commandGlyph) {
		t.Errorf("a replayed frame was drawn as an invocation:\n%s", replayed)
	}
	if !strings.Contains(replayed, echoedLabel) {
		t.Errorf("a replayed frame lost its %q head:\n%s", echoedLabel, replayed)
	}

	handed := userBlock(core.Event{
		Kind:     core.KindUserText,
		Text:     "/foo bar",
		Subagent: &core.Subagent{Type: "reviewer"},
	}, w)
	if strings.Contains(handed, commandGlyph) {
		t.Errorf("a subagent prompt was drawn as an invocation:\n%s", handed)
	}
	if !strings.Contains(handed, promptLabel) {
		t.Errorf("a subagent prompt lost its %q head:\n%s", promptLabel, handed)
	}
}

// A command routed from the room is still the operator's own turn, so it
// compacts and reads the same way its own disk readback will. A room turn that
// carries a mention opens with '@', never matches, and keeps its prose "from
// the room" head - the only place that head has a mention to explain.
func TestARoomRoutedCommandCompactsButAMentionStaysProse(t *testing.T) {
	const w = 60
	routed := userBlock(core.Event{Kind: core.KindUserText, Text: "/complete-linear-ticket", FromRoom: true}, w)
	if !strings.Contains(routed, commandGlyph) {
		t.Errorf("a command routed from the room did not compact:\n%s", routed)
	}

	mentioned := userBlock(core.Event{Kind: core.KindUserText, Text: "@all /compact", FromRoom: true}, w)
	if strings.Contains(mentioned, commandGlyph) {
		t.Errorf("a mention-bearing room turn was compacted, losing its head:\n%s", mentioned)
	}
	if !strings.Contains(mentioned, roomTurnLabel) {
		t.Errorf("a mention-bearing room turn lost its %q head:\n%s", roomTurnLabel, mentioned)
	}
}

// A long command is one line and never wider than the pane, cut with an
// ellipsis rather than silently - so the echo says what was sent instead of
// misrepresenting it.
func TestALongCommandTruncatesToWidthWithAnEllipsis(t *testing.T) {
	const w = 30
	out := commandLine("/deploy-prod "+strings.Repeat("x", 200), w)
	if got := lipgloss.Width(out); got > w {
		t.Errorf("commandLine width = %d, want <= %d:\n%s", got, w, out)
	}
	if strings.Count(out, "\n") != 0 {
		t.Errorf("a compact command is one line, got:\n%s", out)
	}
	if !strings.Contains(out, ellipsis) {
		t.Errorf("a truncated command carries no ellipsis:\n%s", out)
	}
}
