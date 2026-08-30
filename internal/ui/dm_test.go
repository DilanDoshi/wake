package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

// ansiPattern matches the SGR sequences glamour and lipgloss emit. Assertions
// read what a user would see, so they stay stable whether or not the test runs
// against a terminal — and glamour splits even a two-word sentence across
// several colour runs, so a raw Contains would be testing escape codes rather
// than text.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// The conversation's info bar sits above the legend - the info row over the
// keys - and the pane stays within its height with the bar in its new place.
func TestDMDrawsTheBarAboveTheLegend(t *testing.T) {
	d := NewDM("s1", "alex")
	d.Agent = Agent{Cwd: "/tmp/repo", Model: "claude-opus-5", Effort: "xhigh"}
	d = d.SetSize(120, 24)

	out := stripANSI(d.View(120, 24))
	lines := strings.Split(out, "\n")
	if len(lines) > 24 {
		t.Fatalf("the pane drew %d rows into 24 - the bar's new row was double-counted:\n%s", len(lines), out)
	}
	barAt, hintAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, effortLabel+"xhigh") {
			barAt = i
		}
		if strings.Contains(l, "send") {
			hintAt = i
		}
	}
	if barAt < 0 {
		t.Fatalf("the status bar was not drawn:\n%s", out)
	}
	if hintAt < 0 {
		t.Fatalf("the legend was not drawn:\n%s", out)
	}
	if barAt > hintAt {
		t.Fatalf("the bar (row %d) must sit above the legend (row %d):\n%s", barAt, hintAt, out)
	}
}

// expandedDM is a conversation whose tool runs are drawn whole - every call as
// its own ⏺ block with its result open - the ⌃E view. A run of tool calls folds
// to one rollup line by default (see rollup.go), so the tests about how one call
// renders assert against this rather than the fold. The flag is set from
// construction and carried through SetSize and Append, so a call drawn as it
// arrives is drawn whole.
func expandedDM(id, name string) DM {
	d := NewDM(id, name)
	d.expanded = true
	return d
}

// visible renders the DM and reduces it to what a reader sees: no escape
// codes, no trailing padding.
func visible(d DM, w, h int) string {
	lines := strings.Split(stripANSI(d.View(w, h)), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// lineIndex returns the first line containing sub, or -1.
func lineIndex(s, sub string) int {
	for i, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

func assertShows(t *testing.T, d DM, w, h int, want string) {
	t.Helper()
	if out := visible(d, w, h); !strings.Contains(out, want) {
		t.Errorf("view is missing %q:\n%s", want, out)
	}
}

func assertHides(t *testing.T, d DM, w, h int, unwanted string) {
	t.Helper()
	if out := visible(d, w, h); strings.Contains(out, unwanted) {
		t.Errorf("view should not contain %q:\n%s", unwanted, out)
	}
}

// --- the brief's six ---------------------------------------------------

func TestAppendRendersAssistantText(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	d = d.Append(core.Event{Kind: core.KindAssistantText, Text: "hello there"})

	assertShows(t, d, 60, 20, "hello there")
}

func TestAppendRendersToolCall(t *testing.T) {
	d := expandedDM("s1", "alex").SetSize(60, 20)
	d = d.Append(core.Event{
		Kind: core.KindToolUse,
		Tool: &core.ToolCall{Name: "Bash", Display: "go test ./..."},
	})

	assertShows(t, d, 60, 20, "Bash")
	assertShows(t, d, 60, 20, "⏺")
}

func TestAppendIsImmutable(t *testing.T) {
	base := NewDM("s1", "alex").SetSize(60, 20)
	next := base.Append(core.Event{Kind: core.KindAssistantText, Text: "mutated?"})

	assertHides(t, base, 60, 20, "mutated?")
	assertShows(t, next, 60, 20, "mutated?")
}

func TestUnknownEventsAreNotRendered(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	d = d.Append(core.Event{Kind: core.KindUnknown, Text: "internal noise"})

	assertHides(t, d, 60, 20, "internal noise")
}

func TestHeaderShowsAgentName(t *testing.T) {
	d := NewDM("s1", "sydney").SetSize(60, 20)
	assertShows(t, d, 60, 20, "sydney")
}

func TestViewIncludesComposer(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	assertShows(t, d, 60, 20, "send")
}

// --- immutability, beyond the receiver ---------------------------------

// Two appends from one base must not see each other. A DM shares both of the
// sequences it holds - the events it was handed and the lines they rendered to
// - with every DM derived from it, so an append that writes into either in
// place is the second branch overwriting the first's event.
//
// Three sizes because an overwrite hides at the size the branches were built
// at and surfaces the moment one is re-laid: a new height re-windows the
// stored lines, and a new width re-renders every block from the events.
// chunked_test.go pins the same property one level down, at the chunk
// boundary where it is most at risk.
func TestAppendDoesNotShareItsBackingArray(t *testing.T) {
	base := NewDM("s1", "alex").SetSize(60, 24)
	for i := range 3 {
		base = base.Append(core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("history %d", i)})
	}

	first := base.Append(core.Event{Kind: core.KindAssistantText, Text: "branch one"})
	second := base.Append(core.Event{Kind: core.KindAssistantText, Text: "branch two"})

	for _, at := range [][2]int{{60, 24}, {60, 30}, {100, 24}} {
		w, h := at[0], at[1]
		assertShows(t, first, w, h, "branch one")
		assertHides(t, first, w, h, "branch two")
		assertShows(t, second, w, h, "branch two")
		assertHides(t, second, w, h, "branch one")
	}
}

// Resizing re-wraps every block, so it is the operation most likely to write
// through into a DM someone else still holds.
func TestSetSizeDoesNotRewrapTheReceiver(t *testing.T) {
	narrow := NewDM("s1", "alex").SetSize(40, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: wrappingSentence})

	wide := narrow.SetSize(120, 20)

	assertHides(t, narrow, 40, 20, wrappingSentence)
	assertShows(t, wide, 120, 20, wrappingSentence)
}

func TestWithComposerDoesNotChangeTheReceiver(t *testing.T) {
	base := NewDM("s1", "alex")
	// A second NewComposer rather than an edit of the first: two Composers
	// share one text area internally, so typing into a copy is visible through
	// the original and could not distinguish them. This is the hazard dm.go's
	// Composer doc warns about, arriving in a test.
	other := typeInto(t, NewComposer(), "x")
	next := base.WithComposer(other)

	if got := next.Composer().Value(); got != "x" {
		t.Fatalf("WithComposer did not carry the composer: Value() = %q", got)
	}
	if got := base.Composer().Value(); got != "" {
		t.Errorf("WithComposer changed the receiver's composer: it now holds %q", got)
	}
}

// View draws; it must not edit. Two calls agree.
func TestViewIsStable(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "stable output"})

	if first, second := d.View(60, 20), d.View(60, 20); first != second {
		t.Errorf("View is not stable:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// --- geometry -----------------------------------------------------------

// The grid joins panes on their widest line and stacks them by height, so a
// DM must measure exactly the box it was handed.
func TestViewMeasuresExactlyTheSizeRequested(t *testing.T) {
	for _, h := range []int{minDMHeight, 10, 20, 40} {
		for _, w := range []int{20, 40, 60, 120} {
			d := NewDM("s1", "alex").SetSize(w, h).
				Append(core.Event{Kind: core.KindAssistantText, Text: "a message long enough to wrap somewhere"})

			out := d.View(w, h)
			if got := lipgloss.Height(out); got != h {
				t.Errorf("View(%d,%d) is %d rows tall, want %d:\n%s", w, h, got, h, out)
			}
			if got := lipgloss.Width(out); got != w {
				t.Errorf("View(%d,%d) is %d columns wide, want %d:\n%s", w, h, got, w, out)
			}
		}
	}
}

// Below the floor the DM stops shrinking rather than rendering a broken box,
// the same discipline Composer applies to width.
func TestViewFloorsBelowItsMinimumSize(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {1, 1}, {-4, -4}, {8, 3}} {
		d := NewDM("s1", "alex").SetSize(size[0], size[1]).
			Append(core.Event{Kind: core.KindAssistantText, Text: "still here"})

		out := d.View(size[0], size[1])
		if got := lipgloss.Height(out); got != minDMHeight {
			t.Errorf("View%v is %d rows tall, want the %d-row floor:\n%s", size, got, minDMHeight, out)
		}
		if got := lipgloss.Width(out); got != minComposerWidth {
			t.Errorf("View%v is %d columns wide, want the %d-column floor:\n%s", size, got, minComposerWidth, out)
		}
	}
}

// countRenders reports how many times f re-rendered a whole transcript through
// glamour. It swaps the renderTranscript seam rather than timing anything, so
// the assertion is exact instead of flaky.
func countRenders(t testing.TB, f func()) int {
	t.Helper()
	n := renderCounter(t)
	f()
	return *n
}

// renderCounter is the same seam for a caller whose measured section cannot be
// a closure: testing.B.Loop has to be the loop condition of the benchmark
// function itself. The count keeps rising until the test ends.
func renderCounter(t testing.TB) *int {
	t.Helper()
	n := 0
	original := renderTranscript
	renderTranscript = func(d DM) []block {
		n++
		return original(d)
	}
	t.Cleanup(func() { renderTranscript = original })
	return &n
}

func sizedDM(t *testing.T, w, h int) DM {
	t.Helper()
	return NewDM("s1", "alex").SetSize(w, h).
		Append(core.Event{Kind: core.KindAssistantText, Text: wrappingSentence})
}

// View has a value receiver, so it cannot memoize a re-lay: a caller who keeps
// passing a width SetSize never saw re-renders the whole transcript every
// frame, behind the process-global mutex every session's rendering shares.
// This is the fast path that keeps that from happening.
func TestViewDoesNotReRenderAtASizeAlreadySet(t *testing.T) {
	d := sizedDM(t, 60, 20)

	if n := countRenders(t, func() {
		for range 3 {
			_ = d.View(60, 20)
		}
	}); n != 0 {
		t.Errorf("View re-rendered the transcript %d times at a size SetSize already applied", n)
	}
}

// Height changes the viewport, not the wrapping.
func TestAHeightChangeDoesNotReRenderTheTranscript(t *testing.T) {
	d := sizedDM(t, 60, 20)

	if n := countRenders(t, func() { _ = d.SetSize(60, 40).View(60, 40) }); n != 0 {
		t.Errorf("a height change re-rendered the transcript %d times, want 0", n)
	}
}

// Width does re-wrap - once, not once per frame.
func TestAWidthChangeReRendersTheTranscriptExactlyOnce(t *testing.T) {
	d := sizedDM(t, 60, 20)

	if n := countRenders(t, func() {
		wide := d.SetSize(120, 20)
		for range 3 {
			_ = wide.View(120, 20)
		}
	}); n != 1 {
		t.Errorf("a width change re-rendered the transcript %d times, want 1", n)
	}
}

// And this is the cost of not calling SetSize, pinned rather than left as a
// surprise: View re-lays for a size it was not given so an early frame is
// never wrong, but a value receiver cannot memoize the result, so the whole
// transcript goes through glamour again on every frame. Whoever finds a way to
// remove that has this test to delete.
func TestViewWithoutSetSizeReRendersEveryFrame(t *testing.T) {
	d := NewDM("s1", "alex").Append(core.Event{Kind: core.KindAssistantText, Text: wrappingSentence})

	if n := countRenders(t, func() {
		for range 3 {
			_ = d.View(80, 24)
		}
	}); n != 3 {
		t.Errorf("View at an unset size re-rendered %d times over 3 frames, want 3", n)
	}
}

// A View at a size SetSize was never told about must still be correct — App
// renders before the first WindowSizeMsg lands.
func TestViewAdoptsASizeItWasNotSet(t *testing.T) {
	d := NewDM("s1", "alex").Append(core.Event{Kind: core.KindAssistantText, Text: "early message"})

	assertShows(t, d, 80, 24, "early message")
	if got := lipgloss.Height(d.View(80, 24)); got != 24 {
		t.Errorf("height = %d, want 24", got)
	}
}

// wrappingSentence is longer than 40 columns and shorter than 120, so it is
// broken across lines at the first width and whole at the second.
const wrappingSentence = "the quick brown fox jumps over the lazy dog and keeps running past the margin"

// Resizing must re-wrap the whole transcript. Blocks rendered once at the old
// width and never revisited leave an hour of scrollback wrapped for a window
// that no longer exists.
func TestResizeRewrapsTheExistingTranscript(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(40, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: wrappingSentence})

	assertHides(t, d, 40, 20, wrappingSentence)
	assertShows(t, d.SetSize(120, 20), 120, 20, wrappingSentence)
}

// --- what reaches the transcript, and what does not ---------------------

func TestLifecycleChatterIsDropped(t *testing.T) {
	for _, subtype := range []string{"hook_started", "hook_response", "init", "status", "thinking_tokens"} {
		d := NewDM("s1", "alex").SetSize(60, 20).
			Append(core.Event{Kind: core.KindSystem, Text: subtype})

		assertHides(t, d, 60, 20, subtype)
	}
}

// Two system events mean something to a human reading the transcript, and the
// DM is told which by core.Notice rather than by the raw subtype. Text still
// carries the subtype and must not be what decides: keying on it here is what
// let this map grow one wire string at a time.
func TestMeaningfulSystemFramesBecomeNotices(t *testing.T) {
	for notice, want := range map[core.Notice]string{
		core.NoticeContextCompacted: "compacted",
		core.NoticeToolDenied:       "denied",
	} {
		d := NewDM("s1", "alex").SetSize(60, 20).
			Append(core.Event{Kind: core.KindSystem, Text: "some_subtype", Notice: notice})

		assertShows(t, d, 60, 20, want)
	}
}

// The other half: a subtype the airlock resolved no notice for draws nothing,
// however meaningful its name looks. Without this the map above could be
// replaced by "draw everything" and still pass.
func TestASystemFrameWithNoNoticeDrawsNothing(t *testing.T) {
	empty := NewDM("s1", "alex").SetSize(60, 20)
	got := empty.Append(core.Event{Kind: core.KindSystem, Text: "compact_boundary"})
	if got.View(60, 20) != empty.View(60, 20) {
		t.Errorf("an unresolved subtype drew a block:\n%s", visible(got, 60, 20))
	}
}

func TestTurnEndIsNotRendered(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindTurnEnd, Text: "the turn's final text"})

	assertHides(t, d, 60, 20, "the turn's final text")
}

func TestEmptyTextProducesNoBlock(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	blank := d.Append(core.Event{Kind: core.KindAssistantText, Text: "   \n  "})

	if blank.View(60, 20) != d.View(60, 20) {
		t.Errorf("a blank message added a block:\n%s", visible(blank, 60, 20))
	}
}

func TestToolUseWithoutAToolIsDropped(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	next := d.Append(core.Event{Kind: core.KindToolUse})

	if next.View(60, 20) != d.View(60, 20) {
		t.Errorf("a tool_use with no tool added a block:\n%s", visible(next, 60, 20))
	}
}

// A frame missing the half a block is built from must cost nothing. A blank
// row for every malformed event is how a transcript fills with gaps.
func TestIncompleteEventsAddNoBlankRows(t *testing.T) {
	empty := NewDM("s1", "alex").SetSize(60, 20)
	for name, ev := range map[string]core.Event{
		"user text that is only whitespace":                           {Kind: core.KindUserText, Text: "  \n "},
		"a subagent event with nothing in it":                         {Kind: core.KindThinking, Subagent: &core.Subagent{Dispatch: "toolu_a", Task: "count lines"}},
		"thinking with no content":                                    {Kind: core.KindThinking, Text: ""},
		"an empty tool result":                                        {Kind: core.KindToolResult, Text: ""},
		"a session-reset-shaped system frame with an unknown subtype": {Kind: core.KindSystem, Text: "hook_started"},
		"a rate limit with no status":                                 {Kind: core.KindRateLimit},
	} {
		if got := empty.Append(ev); got.View(60, 20) != empty.View(60, 20) {
			t.Errorf("%s added a block:\n%s", name, visible(got, 60, 20))
		}
	}
}

// Which inputs resolve a diff at all is decided in the airlock now, and
// internal/core's TestToolCallResolvesItsDiffOnlyWhenBothHalvesArePresent
// owns the table. What is left here is that a nil Diff costs nothing but the
// header - TestToolCallWithoutADiffRendersOnlyItsHeader, below.

// An edit whose halves are identical changed nothing, and renders as nothing
// beyond its header.
func TestANoOpEditRendersNoDiff(t *testing.T) {
	d := expandedDM("s1", "alex").SetSize(60, 20)
	next := d.Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
		Name:    "Edit",
		Display: "auth.go",
		Diff:    &core.ToolDiff{Old: "same", New: "same"},
	}})

	out := visible(next, 60, 20)
	if lineIndex(out, "Edit(auth.go)") < 0 {
		t.Errorf("the tool header is missing:\n%s", out)
	}
	if strings.Contains(out, "same") {
		t.Errorf("an unchanged edit rendered a diff:\n%s", out)
	}
}

// A permission request with no tool attached still has to say the agent is
// blocked, or the DM just goes quiet.
func TestPermissionRequestWithoutAToolStillWarns(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindPermissionRequest, RequestID: "req-1"})

	assertShows(t, d, 60, 20, "permission")
}

// --- the unfiltered content the DM exists for ---------------------------

func TestThinkingShowsItsContentAndNotJustALabel(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindThinking, Text: "weighing two approaches"})

	assertShows(t, d, 60, 20, "weighing two approaches")
	assertShows(t, d, 60, 20, "✻")
}

func TestToolResultIsCollapsedWithACount(t *testing.T) {
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = fmt.Sprintf("output line %d", i)
	}
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindToolResult, Text: strings.Join(lines, "\n")})

	assertShows(t, d, 60, 20, "output line 0")
	assertShows(t, d, 60, 20, "lines (⌃E to expand)")
	assertHides(t, d, 60, 20, "output line 24")
}

// An Edit carries the before and after in its own input; the DM is the view
// the spec promises full +/- diffs in.
func TestEditToolRendersADiff(t *testing.T) {
	d := expandedDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			Name:    "Edit",
			Display: "auth.go",
			Diff:    &core.ToolDiff{Old: "alpha\nbravo\ncharlie", New: "alpha\nBRAVO\ncharlie"},
		}})

	assertShows(t, d, 60, 20, "- bravo")
	assertShows(t, d, 60, 20, "+ BRAVO")
	assertHides(t, d, 60, 20, "- alpha") // unchanged lines stay out of the diff
}

// An edit shows its diff by default, without ⌃E or a click. The diff is the
// point of the edit, and folding it into a `1 tool use · 1 edit` count hides
// exactly what changed - so a diff-carrying call draws whole the way a checklist
// does, out of the run rather than into its tally. Owner's 2026-08-28 request.
func TestEditShowsItsDiffWithoutExpanding(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			ID:      "t1",
			Name:    "Edit",
			Display: "auth.go",
			Diff:    &core.ToolDiff{Old: "alpha\nbravo\ncharlie", New: "alpha\nBRAVO\ncharlie"},
		}})

	assertShows(t, d, 60, 20, "- bravo")
	assertShows(t, d, 60, 20, "+ BRAVO")
	// And it is not hidden behind a folded rollup count.
	assertHides(t, d, 60, 20, "1 tool use")
}

// A successful edit's result is pure confirmation - "the file has been updated" -
// which the diff and the green ⏺ already carry, so it is not drawn, the way
// Claude Code omits it. A *failed* edit still shows its result, because that is
// the error the operator has to read.
func TestASuccessfulEditHidesItsConfirmationResult(t *testing.T) {
	forceColour(t)
	updated := "The file auth.go has been updated successfully. (file state is current in your context — no need to Read it back)"
	d := NewDM("s1", "alex").SetSize(70, 20).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			ID: "t1", Name: "Edit", Display: "auth.go",
			Diff: &core.ToolDiff{Old: "alpha\nbravo", New: "alpha\nBRAVO"},
		}}).
		Append(result("t1", updated, false))

	assertShows(t, d, 70, 20, "+ BRAVO")          // the diff still shows
	assertHides(t, d, 70, 20, "has been updated") // the confirmation does not
	// The bullet still settles green even though its result drew nothing.
	if head := headerRow(t, d); !strings.Contains(head, escFor(t, Success)) {
		t.Errorf("a successful edit whose result was suppressed never settled: %q", head)
	}

	// A failed edit shows its result - that is the error.
	f := NewDM("s2", "bob").SetSize(70, 20).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			ID: "t2", Name: "Edit", Display: "auth.go",
			Diff: &core.ToolDiff{Old: "alpha\nbravo", New: "alpha\nBRAVO"},
		}}).
		Append(result("t2", "String to replace not found in file.", true))
	assertShows(t, f, 70, 20, "String to replace not found")
}

// render.Diff has no cap of its own, and its prefix/suffix trim turns two
// scattered edits in a large file into a hunk spanning everything between them
// — here, 1582 styled lines for a change of two.
//
// The pane is tall enough to hold the whole capped block, or the viewport
// would do the bounding and the cap would go untested.
func TestAHugeDiffIsCappedWithACount(t *testing.T) {
	const height = render.MaxDiffLines + 60
	before := make([]string, 800)
	for i := range before {
		before[i] = fmt.Sprintf("line %d", i)
	}
	after := append([]string(nil), before...)
	after[5] = "changed near the top"
	after[795] = "changed near the bottom"

	d := expandedDM("s1", "alex").SetSize(60, height).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			Name:    "Edit",
			Display: "big.go",
			Diff:    &core.ToolDiff{Old: strings.Join(before, "\n"), New: strings.Join(after, "\n")},
		}})

	out := visible(d, 60, height)
	if !strings.Contains(out, "lines not shown") {
		t.Errorf("an oversized diff was not capped:\n%s", out)
	}
	dels, adds := diffPolarity(out)
	if dels+adds > render.MaxDiffLines {
		t.Errorf("diff emitted %d lines, cap is %d", dels+adds, render.MaxDiffLines)
	}
	// render.Diff emits every removed line before every added one, so a cut
	// that keeps only the head renders this as a wall of red with no green —
	// a one-sided truncation in the view whose promise is "full +/−".
	if dels == 0 || adds == 0 {
		t.Errorf("a capped diff kept only one polarity: %d removed, %d added:\n%s", dels, adds, out)
	}
}

// An ordinary refactor — a whole function rewritten — must render whole. §9 of
// the spec promises the DM "full +/−" where the group chat shows none, so the
// cap has to sit above anything a human would actually write.
func TestAnOrdinaryRefactorIsNotCapped(t *testing.T) {
	const height = render.MaxDiffLines + 60
	oldBody := make([]string, 40)
	newBody := make([]string, 40)
	for i := range oldBody {
		oldBody[i] = fmt.Sprintf("\told line %d", i)
		newBody[i] = fmt.Sprintf("\tnew line %d", i)
	}

	d := expandedDM("s1", "alex").SetSize(60, height).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			Name:    "Edit",
			Display: "auth.go",
			Diff:    &core.ToolDiff{Old: strings.Join(oldBody, "\n"), New: strings.Join(newBody, "\n")},
		}})

	out := visible(d, 60, height)
	if strings.Contains(out, "lines not shown") {
		t.Errorf("an 80-line refactor was truncated:\n%s", out)
	}
	if got := countDiffLines(out); got != len(oldBody)+len(newBody) {
		t.Errorf("rendered %d diff lines, want %d:\n%s", got, len(oldBody)+len(newBody), out)
	}
}

// diffPolarity counts the removed and added lines on screen.
func diffPolarity(out string) (dels, adds int) {
	for _, l := range strings.Split(out, "\n") {
		switch t := strings.TrimSpace(l); {
		case strings.HasPrefix(t, "-"):
			dels++
		case strings.HasPrefix(t, "+"):
			adds++
		}
	}
	return dels, adds
}

func countDiffLines(out string) int {
	dels, adds := diffPolarity(out)
	return dels + adds
}

// A tool call with no diffable input renders its header and nothing else.
func TestToolCallWithoutADiffRendersOnlyItsHeader(t *testing.T) {
	d := expandedDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			Name:    "Write",
			Display: "notes.txt",
		}})

	assertShows(t, d, 60, 20, "Write(notes.txt)")
	assertHides(t, d, 60, 20, "+ ok")
}

func TestPermissionRequestIsVisible(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{
			Kind:      core.KindPermissionRequest,
			RequestID: "req-1",
			Tool:      &core.ToolCall{Name: "Write", Display: "notes.txt"},
		})

	assertShows(t, d, 60, 20, "Write")
	assertShows(t, d, 60, 20, "permission")
}

// A rate limit never draws in the transcript at all now: a warning is a timed
// pop-up above the composer (ratelimit.go, TestARateLimitNeverDrawsInThe-
// Transcript) and a benign `allowed` heartbeat is chrome. This asserts the DM
// side of that - the renderer draws nothing for either.
func TestRateLimitDrawsNoTranscriptBlock(t *testing.T) {
	quiet := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindRateLimit, Text: "allowed"})
	assertHides(t, quiet, 60, 20, "allowed")

	loud := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindRateLimit, Text: "exhausted", Notice: core.NoticeRateLimited})
	assertHides(t, loud, 60, 20, "exhausted")
}

// --- user turns and Echoed ---------------------------------------------

// Both halves of the conversation belong in a 1:1 view. Echoed picks the
// label; it never decides whether the text is shown, because which value the
// echo carries has never been observed.
func TestUserTextIsRenderedWhetherEchoedOrNot(t *testing.T) {
	fresh := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindUserText, Text: "ship the fix"})
	assertShows(t, fresh, 60, 20, "ship the fix")
	assertShows(t, fresh, 60, 20, userLabel)

	echoed := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindUserText, Text: "ship the fix", Echoed: true})
	assertShows(t, echoed, 60, 20, "ship the fix")
	assertShows(t, echoed, 60, 20, echoedLabel)
}

// The <local-command-stdout> envelope is pure wire format and is unwrapped in
// the airlock now, so this package never sees it and no longer names it.
// internal/core's TestUserTextLosesTheLocalCommandStdoutEnvelope owns it.

// --- transcript layout --------------------------------------------------

// Glamour opens every document with blank rows of its own - a newline, and for
// some blocks a row of spaces inside the margin. Left alone that is a wasted
// line per message on top of the blank line the transcript already puts between
// blocks, which in an hour-long DM is a third of the screen. render.Markdown
// trims both edges; joinBlock trims the parts it joins.
func TestBlocksAreSeparatedByExactlyOneBlankRow(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "first message"}).
		Append(core.Event{Kind: core.KindAssistantText, Text: "second message"})

	out := visible(d, 60, 20)
	first, second := lineIndex(out, "first message"), lineIndex(out, "second message")
	if first < 0 || second < 0 {
		t.Fatalf("both messages should be visible:\n%s", out)
	}
	if gap := second - first; gap != 2 {
		t.Errorf("messages are %d rows apart, want 2 (one blank row):\n%s", gap, out)
	}
}

// A label introduces the text under it. Glamour's leading blank row would land
// between the two unless every part of a block is trimmed, not just the block
// as a whole.
func TestALabelSitsDirectlyOnTheTextItIntroduces(t *testing.T) {
	for label, ev := range map[string]core.Event{
		userLabel:     {Kind: core.KindUserText, Text: "ship the fix"},
		echoedLabel:   {Kind: core.KindUserText, Text: "ship the fix", Echoed: true},
		thinkingLabel: {Kind: core.KindThinking, Text: "ship the fix"},
	} {
		out := visible(NewDM("s1", "alex").SetSize(60, 20).Append(ev), 60, 20)
		gap := lineIndex(out, "ship the fix") - lineIndex(out, label)
		if gap != 1 {
			t.Errorf("%q is %d rows above its text, want 1:\n%s", label, gap, out)
		}
	}
}

// Every block's content starts in the same column; only labels and markers sit
// at the left edge.
func TestPlainBodiesAlignWithRenderedMarkdown(t *testing.T) {
	markdown := visible(NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "agent prose"}), 60, 20)
	thinking := visible(NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindThinking, Text: "agent prose"}), 60, 20)

	if got, want := indentOf(thinking, "agent prose"), indentOf(markdown, "agent prose"); got != want {
		t.Errorf("thinking body is indented %d columns, markdown %d:\n%s", got, want, thinking)
	}
}

// indentOf returns the leading spaces on the first line containing sub.
func indentOf(s, sub string) int {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			return len(l) - len(strings.TrimLeft(l, " "))
		}
	}
	return -1
}

// A result sits directly under the call it answers. The ⏺/⎿ pair is Claude
// Code's most recognizable idiom, and a blank row between the halves reads as
// two unrelated things.
func TestAToolResultSitsDirectlyUnderItsCall(t *testing.T) {
	d := expandedDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			Name:    "Bash",
			Display: "go test ./...",
		}}).
		Append(core.Event{Kind: core.KindToolResult, Text: "ok  wake  0.4s"})

	out := visible(d, 60, 20)
	call, result := lineIndex(out, "⏺"), lineIndex(out, "⎿")
	if call < 0 || result < 0 {
		t.Fatalf("both halves should be visible:\n%s", out)
	}
	if gap := result - call; gap != 1 {
		t.Errorf("result is %d rows below its call, want 1:\n%s", gap, out)
	}
}

// Two turns of prose still get their blank row - only the result attaches.
func TestOnlyToolResultsAttachToThePrecedingBlock(t *testing.T) {
	d := expandedDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			Name:    "Bash",
			Display: "go test ./...",
		}}).
		Append(core.Event{Kind: core.KindAssistantText, Text: "tests are green"})

	out := visible(d, 60, 20)
	if gap := lineIndex(out, "tests are green") - lineIndex(out, "⏺"); gap != 2 {
		t.Errorf("prose is %d rows below the tool call, want 2:\n%s", gap, out)
	}
}

// The transcript is a conversation: for a reader at the bottom, the newest
// message is the one on screen.
func TestTranscriptStaysPinnedToTheNewestMessage(t *testing.T) {
	d := longConversation(t, 30)

	assertShows(t, d, 60, 12, "message-29")
	assertHides(t, d, 60, 12, "message-00")
}

// ...but a reader who has scrolled back is reading. Yanking them to the newest
// line every time the agent speaks makes an hour-long session unreadable, and
// would silently undo the scrollback keys the app still has to grow.
//
// The scroll is applied to the transcript directly because DM has no Update
// yet; that is the seam those keys will arrive through. The assertion still
// reads the screen.
func TestANewEventDoesNotYankAScrolledReaderToTheBottom(t *testing.T) {
	d := longConversation(t, 30)
	d.tr = d.tr.scrolledUp(20)

	before := visible(d, 60, 12)
	after := d.Append(core.Event{Kind: core.KindAssistantText, Text: "message-30"})

	if got := visible(after, 60, 12); got != before {
		t.Errorf("a new event moved a scrolled reader:\nbefore:\n%s\nafter:\n%s", before, got)
	}
	assertHides(t, after, 60, 12, "message-30")
}

// Once the reader scrolls back down, the view follows again.
func TestReturningToTheBottomResumesFollowing(t *testing.T) {
	d := longConversation(t, 30)
	d.tr = d.tr.scrolledUp(20).toBottom()

	assertShows(t, d.Append(core.Event{Kind: core.KindAssistantText, Text: "message-30"}), 60, 12, "message-30")
}

func longConversation(t *testing.T, n int) DM {
	t.Helper()
	d := NewDM("s1", "alex").SetSize(60, 12)
	for i := range n {
		d = d.Append(core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("message-%02d", i)})
	}
	return d
}

// --- composer ownership -------------------------------------------------

func TestComposerDraftAppearsInTheView(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	d = d.WithComposer(typeInto(t, d.Composer(), "reply text"))

	assertShows(t, d, 60, 20, "reply text")
}

func TestNewDMKeepsItsIdentity(t *testing.T) {
	d := NewDM("session-uuid", "alex")
	if d.SessionID != "session-uuid" {
		t.Errorf("SessionID = %q, want %q", d.SessionID, "session-uuid")
	}
	if d.Name != "alex" {
		t.Errorf("Name = %q, want %q", d.Name, "alex")
	}
}
