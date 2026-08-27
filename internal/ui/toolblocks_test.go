package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// escFor is the escape lipgloss emits for one colour at the forced profile.
// Derived rather than hard-coded, for background()'s reason: this is about
// whether a style was applied, not about how lipgloss spells it.
func escFor(t *testing.T, c lipgloss.AdaptiveColor) string {
	t.Helper()
	rendered := lipgloss.NewStyle().Foreground(c).Render("x")
	esc, _, ok := strings.Cut(rendered, "x")
	if !ok || esc == "" {
		t.Fatalf("lipgloss emitted no escape at this profile: %q", rendered)
	}
	return esc
}

func bashCall(id string) core.Event {
	return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
		ID: id, Name: "Bash", Title: "Listing files", Display: "ls -la", Command: "ls -la",
	}}
}

func result(id, body string, failed bool) core.Event {
	return core.Event{Kind: core.KindToolResult, Text: body, Tool: &core.ToolCall{ID: id, IsError: failed}}
}

// withRunOpen draws d with the tool run keyed by id already open - each call as
// its own ⏺ block with its result still folded and clickable, the view a click
// on a rollup produces. A run of tool calls folds to one line by default (see
// rollup.go); these tests are about a single call's own rendering, its bullet
// and its clickable result, so they open the run it belongs to. Set before the
// appends and carried through them, so the run draws whole as it grows.
func withRunOpen(d DM, id string) DM {
	d.runOpen = map[string]bool{id: true}
	return d
}

// --- the header shape ------------------------------------------------------

// Claude Code heads a shell call with its description and puts the command
// underneath. The command is what tells a reader it was a shell call at all,
// so losing either half is a header that says less than the old Bash(cmd) did.
func TestABashCallIsHeadedByItsDescriptionWithTheCommandUnderIt(t *testing.T) {
	d := withRunOpen(NewDM("s1", "alex").SetSize(60, 20), "t1").Append(bashCall("t1"))

	assertShows(t, d, 60, 20, "⏺ Listing files")
	assertShows(t, d, 60, 20, "$ ls -la")
	assertHides(t, d, 60, 20, "Bash(")
}

// The body continues the gutter the command opened rather than drawing a
// second ⎿, which would read as two results.
func TestABashResultContinuesTheGutterItsCommandOpened(t *testing.T) {
	d := withRunOpen(NewDM("s1", "alex").SetSize(60, 20), "t1").
		Append(bashCall("t1")).
		Append(result("t1", "total 8\ndrwxr-xr-x", false))

	if got := strings.Count(visible(d, 60, 20), "⎿"); got != 1 {
		t.Errorf("a Bash call and its result drew %d gutters, want 1:\n%s", got, visible(d, 60, 20))
	}
}

// A Read collapses to a count rather than to four lines of the file, which is
// the receipt Claude Code draws. Read is the only tool that gets one - see
// core's toolShapes.
func TestAReadResultCollapsesToItsLineCount(t *testing.T) {
	body := "1\talpha\n2\tbravo\n3\tcharlie\n4\tdelta\n5\techo\n6\tfoxtrot"
	d := withRunOpen(NewDM("s1", "alex").SetSize(60, 20), "t1").
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			ID: "t1", Name: "Read", Display: "auth.go", Receipt: "Read %d lines",
		}}).
		Append(result("t1", body, false))

	assertShows(t, d, 60, 20, "Read 6 lines")
	assertHides(t, d, 60, 20, "alpha")
}

// --- the bullet ------------------------------------------------------------

// The bullet is the one part of a tool block that changes after it is drawn:
// dim while the result is outstanding, green when it lands, red when it fails.
// Read out of Claude Code's own bullet component - see theme.go.
func TestTheBulletSaysWhatBecameOfTheCall(t *testing.T) {
	forceColour(t)
	running, ok, fail := escFor(t, Muted), escFor(t, Success), escFor(t, Error)

	d := withRunOpen(NewDM("s1", "alex").SetSize(60, 20), "t1").Append(bashCall("t1"))
	head := headerRow(t, d)
	if !strings.Contains(head, running) {
		t.Errorf("a call with no result yet is not dim: %q", head)
	}

	settled := d.Append(result("t1", "total 8", false))
	if head = headerRow(t, settled); !strings.Contains(head, ok) {
		t.Errorf("a call whose result landed is not green: %q", head)
	}

	failed := d.Append(result("t1", "boom", true))
	if head = headerRow(t, failed); !strings.Contains(head, fail) {
		t.Errorf("a call whose result failed is not red: %q", head)
	}
}

// A command that printed nothing still finishes. Its result draws no block at
// all, so a bullet settled only where a block was added stays dim forever -
// and an empty result is the ordinary case for most shell commands.
func TestACallWhoseResultIsEmptyStillSettles(t *testing.T) {
	forceColour(t)
	d := withRunOpen(NewDM("s1", "alex").SetSize(60, 20), "t1").
		Append(bashCall("t1")).
		Append(result("t1", "", false))

	if head := headerRow(t, d); !strings.Contains(head, escFor(t, Success)) {
		t.Errorf("a call whose result was empty never settled: %q", head)
	}
}

// The settle rewrites one line rather than re-deriving the transcript, because
// a result is an ordinary event and Append may not cost what the conversation
// so far cost.
func TestSettlingACallDoesNotReRenderTheTranscript(t *testing.T) {
	renders := 0
	prev := renderTranscript
	renderTranscript = func(d DM) []block { renders++; return prev(d) }
	t.Cleanup(func() { renderTranscript = prev })

	d := NewDM("s1", "alex").SetSize(60, 20).Append(bashCall("t1"))
	renders = 0
	d.Append(result("t1", "total 8", false))

	if renders != 0 {
		t.Errorf("settling a call re-rendered the transcript %d time(s)", renders)
	}
}

// A re-wrap re-derives every block, so the outcome has to survive it: the
// bullet is not stored on the line, it is drawn from what the pane knows.
func TestAnOutcomeSurvivesARewrap(t *testing.T) {
	forceColour(t)
	d := withRunOpen(NewDM("s1", "alex").SetSize(60, 20), "t1").
		Append(bashCall("t1")).
		Append(result("t1", "total 8", false)).
		SetSize(48, 20)

	if head := headerRow(t, d); !strings.Contains(head, escFor(t, Success)) {
		t.Errorf("a settled call went dim again after a re-wrap: %q", head)
	}
}

// --- click to expand -------------------------------------------------------

func TestClickingAFoldedResultOpensThatOneAlone(t *testing.T) {
	// One run holds both calls - they arrive with nothing between them - so
	// opening the run keyed by its first use draws each call whole with its
	// result still folded, which is the click target this test is about.
	d := withRunOpen(NewDM("s1", "alex").SetSize(60, 40), "t1").
		Append(bashCall("t1")).
		Append(result("t1", strings.Join(repeatLines("first", 30), "\n"), false)).
		Append(bashCall("t2")).
		Append(result("t2", strings.Join(repeatLines("second", 30), "\n"), false))

	assertHides(t, d, 60, 40, "first-29")
	assertHides(t, d, 60, 40, "second-29")

	line, ok := d.tr.headLine("t1")
	if !ok {
		t.Fatal("the first call was never drawn")
	}
	opened, hit := d.openTool(line)
	if !hit {
		t.Fatalf("line %d holds no tool call", line)
	}

	assertShows(t, opened, 60, 40, "first-29")
	assertHides(t, opened, 60, 40, "second-29")
}

func TestClickingAnOpenedResultFoldsItBack(t *testing.T) {
	d := withRunOpen(NewDM("s1", "alex").SetSize(60, 40), "t1").
		Append(bashCall("t1")).
		Append(result("t1", strings.Join(repeatLines("out", 30), "\n"), false))

	line, _ := d.tr.headLine("t1")
	opened, _ := d.openTool(line)
	assertShows(t, opened, 60, 40, "out-29")

	folded, hit := opened.openTool(line)
	if !hit {
		t.Fatal("the opened block stopped being clickable")
	}
	assertHides(t, folded, 60, 40, "out-29")
}

// A click on prose is not a click on a tool call, and must not be swallowed:
// the same gesture starts a text selection.
func TestClickingAnythingElseOpensNothing(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "the retry header survives"})

	for line := range 6 {
		if _, hit := d.openTool(line); hit {
			t.Errorf("line %d of a prose block reports a tool call", line)
		}
	}
}

func repeatLines(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + "-" + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	return out
}

// headerRow is the rendered ⏺ line of the one call in a conversation.
func headerRow(t *testing.T, d DM) string {
	t.Helper()
	at, ok := d.tr.headLine("t1")
	if !ok {
		t.Fatal("no call was drawn")
	}
	return d.tr.lines.at(at)
}

// A conversation read back off claude's disk arrives through Before, and its
// calls have to be filed the way live ones are: without that, every restored
// call draws a bullet that never settles and a Read that shows its file
// instead of its count.
func TestCallsRestoredFromHistoryCarryTheirOutcomes(t *testing.T) {
	forceColour(t)
	d := withRunOpen(NewDM("s1", "alex").SetSize(60, 20), "t1").Before([]core.Event{
		bashCall("t1"),
		result("t1", "total 8", false),
	})

	if head := headerRow(t, d); !strings.Contains(head, escFor(t, Success)) {
		t.Errorf("a call restored from history never settled: %q", head)
	}
	assertShows(t, d, 60, 20, "⏺ Listing files")
}

// ⌃E opens every result in a conversation at once. Marking every row of every
// opened block would put a click entry on essentially every line in the
// transcript, rebuilt on each re-wrap - so the target is bounded and the
// header stays the way back whatever the block's height.
func TestAnExpandedResultDoesNotMarkEveryLineItHas(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 40).
		Append(bashCall("t1")).
		Append(result("t1", strings.Join(repeatLines("out", 60), "\n"), false)).
		toggleExpanded()

	if got, limit := len(d.tr.tools), clickableRows+1; got > limit {
		t.Errorf("an expanded result marks %d clickable lines, want at most %d", got, limit)
	}
	// The header is still the way back, which is what makes the bound safe.
	at, ok := d.tr.headLine("t1")
	if !ok {
		t.Fatal("the call lost its header")
	}
	if _, hit := d.openTool(at); !hit {
		t.Error("an expanded call's header is no longer clickable")
	}
}

// A permission ask is decided on what will actually run.
//
// An ask goes through the same core.toolCall as an invocation, so a Bash ask
// carries its description in Title - and a block headed by that alone puts
// "Run the test suite" in front of an operator approving `rm -rf`. The
// description may be shown; the command may not be dropped.
func TestAPermissionAskAlwaysShowsTheCommandBeingApproved(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).Append(core.Event{
		Kind: core.KindPermissionRequest,
		Tool: &core.ToolCall{
			ID: "t1", Name: "Bash", Title: "Run the test suite",
			Display: "rm -rf /tmp/scratch && make test",
			Command: "rm -rf /tmp/scratch && make test",
		},
	})

	assertShows(t, d, 60, 20, "permission request")
	assertShows(t, d, 60, 20, "rm -rf /tmp/scratch")
}
