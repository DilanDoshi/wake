package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// marks is a ToolStyle whose parts are told apart by what they wrap rather
// than by colour. `go test` is not a terminal, so lipgloss emits no escapes
// here and a colour assertion would pass against unstyled output - see
// internal/ui/appearance_test.go, which forces a profile for the half of this
// that is genuinely about colour. What this package owns is which part gets
// which style, and Transform proves that at any profile.
func marks() ToolStyle {
	mark := func(tag string) lipgloss.Style {
		return lipgloss.NewStyle().Transform(func(s string) string { return "<" + tag + ">" + s })
	}
	return ToolStyle{Bullet: mark("b"), Name: mark("n"), Arg: mark("a"), Body: mark("y"), Fail: mark("f")}
}

// --- headers ---------------------------------------------------------------

func TestToolCallShowsNameAndDisplayArgument(t *testing.T) {
	out := ToolCall(Call{Name: "Bash", Display: "go test ./..."}, ToolStyle{}, 60)
	if want := "⏺ Bash(go test ./...)"; out != want {
		t.Errorf("ToolCall = %q, want %q", out, want)
	}
}

func TestToolCallWithNoDisplayShowsNameOnly(t *testing.T) {
	for _, name := range []string{"MysteryTool", "Bash"} {
		t.Run(name, func(t *testing.T) {
			if out, want := ToolCall(Call{Name: name}, ToolStyle{}, 60), "⏺ "+name; out != want {
				t.Errorf("ToolCall(%s) = %q, want %q", name, out, want)
			}
		})
	}
}

// A Title heads the line in place of the name and its argument, which is what
// Claude Code does with a Bash description: `⏺ Finding tool-call glyphs`, with
// the command on the line beneath. All 33 recorded Bash calls carry one.
func TestATitleHeadsTheCallInsteadOfTheToolName(t *testing.T) {
	out := ToolCall(Call{Name: "Bash", Title: "Finding tool-call glyphs", Display: "grep -rn x"}, ToolStyle{}, 60)
	if want := "⏺ Finding tool-call glyphs"; out != want {
		t.Errorf("ToolCall = %q, want %q", out, want)
	}
}

// The fallback matters because a title is the agent's to write: a call that
// arrives without one must still say what tool ran.
func TestACallWithNoTitleFallsBackToItsName(t *testing.T) {
	out := ToolCall(Call{Name: "Bash", Display: "ls"}, ToolStyle{}, 60)
	if !strings.Contains(out, "Bash") {
		t.Errorf("a call with no title lost its tool name: %q", out)
	}
}

// ToolCall returns exactly one line, deliberately: it is the row a settling
// result rewrites in place, and a two-line return had that rewrite draw the
// `$ …` line a second time under the first.
func TestTheCommandIsItsOwnLineUnderTheHeader(t *testing.T) {
	if out, want := ToolCall(Call{Name: "Bash", Title: "Listing files"}, ToolStyle{}, 60), "⏺ Listing files"; out != want {
		t.Errorf("ToolCall = %q, want %q", out, want)
	}
	if out, want := ToolCommand("ls -la", ToolStyle{}, 60), "  ⎿  $ ls -la"; out != want {
		t.Errorf("ToolCommand = %q, want %q", out, want)
	}
	if out := ToolCommand("", ToolStyle{}, 60); out != "" {
		t.Errorf("ToolCommand with no command = %q, want empty", out)
	}
}

// Claude Code's own cap, matched against 2.1.233: its Bash renderer
// slices the command at 160 characters and appends an ellipsis. Without it a
// 4,000-character command is 260 rows of header in a quarter-screen pane.
func TestALongCommandIsCappedAndStaysInsideTheWidth(t *testing.T) {
	out := ToolCommand(strings.Repeat("x", 4000), ToolStyle{}, 40)
	if !strings.Contains(out, ellipsis) {
		t.Errorf("a 4,000-character command was not capped:\n%s", out)
	}
	rows := strings.Split(out, "\n")
	if len(rows) > commandChars/(40-5)+2 {
		t.Errorf("a capped command still drew %d rows:\n%s", len(rows), out)
	}
	for _, line := range rows {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("line is %d cells against a width of 40: %q", w, line)
		}
	}
}

func TestToolCallCollapsesWhitespaceInArgument(t *testing.T) {
	out := ToolCall(Call{Name: "Bash", Display: "go build\n  ./...\t-v"}, ToolStyle{}, 80)
	if want := "⏺ Bash(go build ./... -v)"; out != want {
		t.Errorf("ToolCall = %q, want %q", out, want)
	}
}

func TestToolCallTruncatesLongArgument(t *testing.T) {
	out := ToolCall(Call{Name: "Bash", Display: strings.Repeat("x", 200)}, ToolStyle{}, 60)
	if !strings.Contains(out, ellipsis) {
		t.Errorf("long argument was not truncated: %q", out)
	}
	if got := ansi.StringWidth(out); got > 60 {
		t.Errorf("tool call is %d cells wide, want <= 60: %q", got, out)
	}
}

// The reserve has to come from the tool's actual name, and the assembled line
// has to be bounded on the no-argument branch too: MCP tool names run past
// twenty columns, and one overlong line shoves a neighbouring pane out of
// alignment.
func TestToolCallNeverExceedsWidth(t *testing.T) {
	const mcpName = "mcp__wake__send_message" // 23 columns

	cases := []struct {
		name string
		call Call
	}{
		{"long argument", Call{Name: "Bash", Display: strings.Repeat("x", 200)}},
		{"long path", Call{Name: "Read", Display: strings.Repeat("/dir", 50)}},
		{"long name, no argument", Call{Name: mcpName}},
		{"wide runes", Call{Name: "Bash", Display: strings.Repeat("世界", 40)}},
		{"wide name, no argument", Call{Name: strings.Repeat("世", 30)}},
		{"long title", Call{Name: "Bash", Title: strings.Repeat("word ", 60)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, width := range []int{4, 10, 20, 40, 60, 80} {
				for _, line := range strings.Split(ToolCall(c.call, ToolStyle{}, width), "\n") {
					if got := ansi.StringWidth(line); got > width {
						t.Errorf("width %d: line is %d cells: %q", width, got, line)
					}
				}
			}
		})
	}
}

func TestToolCallStylesTheBulletAndTheNameApart(t *testing.T) {
	out := ToolCall(Call{Name: "Bash", Display: "ls"}, marks(), 60)
	if want := "<b>⏺ <n>Bash<a>(ls)"; out != want {
		t.Errorf("ToolCall =\n%q\nwant\n%q", out, want)
	}
}

// --- results ---------------------------------------------------------------

func TestToolResultShortBodyIsNeverCollapsed(t *testing.T) {
	got := ToolResult(Result{Body: "one\ntwo\nthree", Collapsed: true}, ToolStyle{}, 60)
	want := "  ⎿  one\n     two\n     three"
	if got != want {
		t.Errorf("ToolResult =\n%q\nwant\n%q", got, want)
	}
}

func TestToolResultEmptyBodyIsEmpty(t *testing.T) {
	for _, body := range []string{"", "\n", "\n\n\n"} {
		if got := ToolResult(Result{Body: body, Collapsed: true}, ToolStyle{}, 60); got != "" {
			t.Errorf("ToolResult(%q) = %q, want empty", body, got)
		}
	}
}

// A body under a header that already opened the gutter continues it rather
// than drawing a second ⎿ - which is what a Bash call's `$ …` line does.
func TestAContinuedResultDoesNotDrawASecondGutter(t *testing.T) {
	got := ToolResult(Result{Body: "out", Continued: true, Collapsed: true}, ToolStyle{}, 60)
	if want := "     out"; got != want {
		t.Errorf("ToolResult =\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(got, "⎿") {
		t.Errorf("a continued body drew a second gutter: %q", got)
	}
}

func TestToolResultExpandedShowsEveryLine(t *testing.T) {
	got := ToolResult(Result{Body: strings.Repeat("alpha\n", 40)}, ToolStyle{}, 60)
	if lines := strings.Count(got, "\n") + 1; lines != 40 {
		t.Errorf("expanded result has %d lines, want 40", lines)
	}
	if strings.Contains(got, ellipsis) {
		t.Errorf("expanded result must not report hidden lines:\n%s", got)
	}
}

func TestACollapsedResultIsBoundedToItsBudget(t *testing.T) {
	got := ToolResult(Result{Body: strings.Repeat("line\n", 200), Collapsed: true}, ToolStyle{}, 60)
	if lines := strings.Count(got, "\n") + 1; lines != collapsedResultLines+1 {
		t.Errorf("collapsed result has %d lines, want %d", lines, collapsedResultLines+1)
	}
	if want := fmt.Sprintf("+%d lines", 200-collapsedResultLines); !strings.Contains(got, want) {
		t.Errorf("ToolResult =\n%s\nwant a %q footer", got, want)
	}
}

// The fold names the key that undoes it. Claude Code prints `(ctrl+o to
// expand)` and composes that string from its own keybinding registry; Wake's
// key is ⌃E, because ⌃O detaches here.
func TestTheFoldNamesTheKeyThatExpandsIt(t *testing.T) {
	got := ToolResult(Result{Body: strings.Repeat("line\n", 40), Collapsed: true, Expand: "⌃E"}, ToolStyle{}, 60)
	if want := "(⌃E to expand)"; !strings.Contains(got, want) {
		t.Errorf("ToolResult =\n%s\nwant a %q hint", got, want)
	}
}

// An unnamed key draws no parenthetical rather than an empty one.
func TestTheFoldWithNoKeyNamesNothing(t *testing.T) {
	got := ToolResult(Result{Body: strings.Repeat("line\n", 40), Collapsed: true}, ToolStyle{}, 60)
	if strings.Contains(got, "(") {
		t.Errorf("ToolResult drew an empty expand hint:\n%s", got)
	}
}

// A receipt stands in for the whole body while it is folded - `Read 5 lines` -
// and the count is of the body's own lines.
func TestAReceiptReplacesTheBodyWhileCollapsed(t *testing.T) {
	body := "1\tone\n2\ttwo\n3\tthree\n4\tfour\n5\tfive\n"
	got := ToolResult(Result{Body: body, Receipt: "Read %d lines", Collapsed: true, Expand: "⌃E"}, ToolStyle{}, 60)
	if want := "  ⎿  Read 5 lines (⌃E to expand)"; got != want {
		t.Errorf("ToolResult =\n%q\nwant\n%q", got, want)
	}
}

func TestAReceiptIsNotDrawnOnceExpanded(t *testing.T) {
	got := ToolResult(Result{Body: "1\tone\n2\ttwo\n", Receipt: "Read %d lines"}, ToolStyle{}, 60)
	if strings.Contains(got, "Read 2 lines") {
		t.Errorf("an expanded result drew its receipt instead of its body:\n%s", got)
	}
	if !strings.Contains(got, "one") {
		t.Errorf("an expanded result lost its body:\n%s", got)
	}
}

// The defect this file was opened for. A tab is **zero** cells to
// ansi.StringWidth and four to lipgloss's renderer, which expands it at draw
// time - so a line measured with the tab still in it is laid out narrower than
// it is drawn, overflows the pane and wraps to column 0. A grep over
// tab-indented source is the common case, and Read results are tab-separated
// too (`1\tname,qty`), so this is most of what a DM shows.
//
// Asserting on the *width* rather than on the absence of a tab: lipgloss
// expands it either way, so a `strings.Contains(got, "\t")` guard passes
// against the broken build and proves nothing.
func TestATabIndentedLineIsMeasuredAtTheWidthItIsDrawn(t *testing.T) {
	const width = 20
	got := ToolResult(Result{Body: "\t\t\treturn fmt.Errorf(\"start claude\")"}, ToolStyle{}, width)
	for _, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Errorf("line is %d cells against a width of %d, so the terminal will wrap it: %q", w, width, line)
		}
	}
}

func TestEveryResultLineFitsTheWidth(t *testing.T) {
	bodies := []string{
		strings.Repeat("w", 400),
		"\t\t\t" + strings.Repeat("deep ", 60),
		strings.Repeat("世界", 120),
		"\x1b[31m" + strings.Repeat("red ", 60) + "\x1b[0m",
	}
	for i, body := range bodies {
		for _, width := range []int{8, 12, 20, 40, 60} {
			for _, collapsed := range []bool{true, false} {
				out := ToolResult(Result{Body: body, Collapsed: collapsed, Expand: "⌃E"}, ToolStyle{}, width)
				for _, line := range strings.Split(out, "\n") {
					if w := ansi.StringWidth(line); w > width {
						t.Errorf("body %d width %d collapsed=%v: line is %d cells: %q", i, width, collapsed, w, line)
					}
				}
			}
		}
	}
}

// Wrapping rather than truncation is the second half of the layout fix: a long
// line used to end in `…` with the rest of it unreachable. Claude Code wraps
// into the gutter, so the continuation lines up under the text above it.
func TestALongResultLineWrapsIntoTheGutterInsteadOfBeingCut(t *testing.T) {
	got := ToolResult(Result{Body: strings.Repeat("word ", 40)}, ToolStyle{}, 30)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("a 200-column line did not wrap at width 30: %q", got)
	}
	if strings.Contains(got, ellipsis) {
		t.Errorf("an expanded result was cut rather than wrapped:\n%s", got)
	}
	for i, l := range lines[1:] {
		if !strings.HasPrefix(l, resultIndent) {
			t.Errorf("continuation %d is not indented into the gutter: %q", i, l)
		}
	}
	if !strings.HasPrefix(lines[0], resultBullet) {
		t.Errorf("first line does not open the gutter: %q", lines[0])
	}
}

// The budget is rendered rows, not source lines. Wake draws into a pane that
// is often a quarter of the screen, where four wrapped source lines is twenty
// rows - the clutter the fold exists to prevent.
func TestOneEnormousLineCannotBlowTheCollapsedBudget(t *testing.T) {
	got := ToolResult(Result{Body: strings.Repeat("x", 4000), Collapsed: true, Expand: "⌃E"}, ToolStyle{}, 40)
	if lines := strings.Count(got, "\n") + 1; lines > collapsedResultLines+1 {
		t.Errorf("collapsed result is %d rows, want at most %d:\n%s", lines, collapsedResultLines+1, got)
	}
}

func TestAFailedResultIsDrawnInTheFailureStyle(t *testing.T) {
	got := ToolResult(Result{Body: "boom", Failed: true}, marks(), 60)
	if !strings.Contains(got, "<f>") {
		t.Errorf("a failed result is not wearing the failure style: %q", got)
	}
	if strings.Contains(got, "<y>") {
		t.Errorf("a failed result is wearing the ordinary body style: %q", got)
	}
}

func TestAnOrdinaryResultIsDrawnInTheBodyStyle(t *testing.T) {
	got := ToolResult(Result{Body: "ok"}, marks(), 60)
	if !strings.Contains(got, "<y>") {
		t.Errorf("result body is unstyled: %q", got)
	}
}

// --- the rollup line -------------------------------------------------------

// A folded run of tool calls stands in for its per-call blocks with one dimmed
// line: the count Claude Code shows under a message rather than the calls
// themselves.
func TestToolRollupDrawsTheSummaryUnderAGutterWithTheExpandHint(t *testing.T) {
	out := ToolRollup("28 tool uses · 24 bash · 1 read · 3 linear-server", "⌃E", marks(), 80)
	for _, want := range []string{"⎿", "28 tool uses · 24 bash · 1 read · 3 linear-server", "(⌃E to expand)"} {
		if !strings.Contains(out, want) {
			t.Errorf("ToolRollup lost %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "\n"); got != 0 {
		t.Errorf("ToolRollup drew %d line breaks, want a single line:\n%s", got, out)
	}
}

// The whole line is the body's, because a rollup is what a fold shows and a
// fold recedes.
func TestToolRollupIsDrawnInTheBodyStyle(t *testing.T) {
	if out := ToolRollup("2 tool uses · 2 bash", "⌃E", marks(), 80); !strings.HasPrefix(out, "<y>") {
		t.Errorf("the rollup is not in the body style: %q", out)
	}
}

// It never exceeds the width it is given: the gutter, the count and the hint
// are all truncated to fit rather than wrapping the pane.
func TestToolRollupNeverExceedsItsWidth(t *testing.T) {
	out := ToolRollup("40 tool uses · 30 bash · 5 read · 5 linear-server", "⌃E", ToolStyle{}, 24)
	if w := ansi.StringWidth(out); w > 24 {
		t.Errorf("ToolRollup drew %d columns into a 24-column pane: %q", w, out)
	}
}

// An empty summary draws nothing rather than a bare gutter, which is what a run
// with nothing to count would leave.
func TestToolRollupDrawsNothingForAnEmptySummary(t *testing.T) {
	if out := ToolRollup("", "⌃E", ToolStyle{}, 80); out != "" {
		t.Errorf("ToolRollup drew %q for an empty summary, want nothing", out)
	}
}
