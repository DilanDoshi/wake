package ui

// The Room as a pane: the size it draws, what a re-wrap costs, and what an
// arriving message may not do to somebody reading back.
//
// It reuses the DM's scale constants rather than declaring a second set. The
// two types share a transcript and a chunked backing, so a cost that is bounded
// in one and not the other would be a property of neither - and a second set of
// numbers is the parallel implementation this project forbids, in the tests.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// sinkRoom keeps the compiler from deciding a measured Append had no effect.
var sinkRoom Room

// roomScaleAgent is who is talking in every cost measurement.
var roomScaleAgent = Agent{ID: "s1", Name: "sydney", Label: "auth-fix"}

// roomScaleEvent is the cheapest thing the room draws: the quiet marker for a
// turn that said nothing.
//
// Prose is the realistic choice and it is the wrong one here, and
// dm_transcript_test.go's scaleEvent already carries the measurement -
// glamour spends roughly half a megabyte and 17µs on one paragraph, and a
// fixed cost that large is exactly what hides a per-append cost that grows
// with the transcript. Measured against prose there, an implementation that
// copies the whole history on every append comes in at 1.6x rather than the 9x
// it deserves. The task brief's own draft of this test used prose, which would
// have made it a test that cannot fail for the bug it names.
func roomScaleEvent() core.Event { return core.Event{Kind: core.KindTurnEnd} }

// roomOf returns a Room that has already been fed n events.
func roomOf(n int) Room {
	r := NewRoom().SetSize(scaleWidth, scaleHeight)
	for range n {
		r = r.Append(roomScaleEvent(), roomScaleAgent)
	}
	return r
}

// roomAppendCost is the time one Append costs on top of base, averaged over
// scaleSamples consecutive appends and taken from the fastest of scaleRounds.
func roomAppendCost(base Room) time.Duration {
	best := time.Duration(math.MaxInt64)
	for range scaleRounds {
		r := base
		start := time.Now()
		for range scaleSamples {
			r = r.Append(roomScaleEvent(), roomScaleAgent)
		}
		elapsed := time.Since(start) / scaleSamples
		sinkRoom = r
		if elapsed < best {
			best = elapsed
		}
	}
	return best
}

// roomBytesPerAppend is the heap one Append allocates on top of base. This is
// the half of the pair that does not depend on how busy the machine is: a
// rebuild of the whole room allocates the whole room, and no amount of
// scheduler noise makes that look flat.
func roomBytesPerAppend(base Room) uint64 {
	var before, after runtime.MemStats
	r := base
	runtime.ReadMemStats(&before)
	for range scaleSamples {
		r = r.Append(roomScaleEvent(), roomScaleAgent)
	}
	runtime.ReadMemStats(&after)
	sinkRoom = r
	return (after.TotalAlloc - before.TotalAlloc) / scaleSamples
}

// countRoomRenders reports how many times f sent the whole room back through
// glamour, by swapping the renderRoom seam rather than by timing anything.
func countRoomRenders(t testing.TB, f func()) int {
	t.Helper()
	n := 0
	original := renderRoom
	renderRoom = func(r Room, lines []roomLine) []block { n++; return original(r, lines) }
	t.Cleanup(func() { renderRoom = original })
	f()
	return n
}

// The room is one column of a joined layout, so the box it draws has to be
// exactly the box it was handed.
//
// The height and width are checked, and then the frame is decomposed - the
// title on the first row, the composer on the last rows. A total on its own is
// an assertion a compensating change keeps correct: a transcript one row short
// and a composer one row tall measure 24 together and are wrong apart, which is
// a shape this project has shipped twice on rendering code.
func TestTheRoomDrawsExactlyTheSizeItWasGiven(t *testing.T) {
	r := NewRoom().SetSize(80, 24)
	for i := range 40 {
		r = r.Append(core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("line %d", i)}, Agent{Name: "sydney"})
	}
	out := r.View(80, 24)
	if h := lipgloss.Height(out); h != 24 {
		t.Errorf("height = %d, want 24: the room is one column of a joined layout and one extra row scrolls the alt screen on every draw", h)
	}
	if w := lipgloss.Width(out); w != 80 {
		t.Errorf("width = %d, want 80", w)
	}

	lines := strings.Split(ansi.Strip(out), "\n")
	composer := strings.Split(ansi.Strip(r.Composer().WithTitle(roomTitle).View(80)), "\n")
	if got := lines[len(lines)-len(composer):]; !strings.Contains(strings.Join(got, "\n"), "> ") {
		t.Errorf("the last %d rows are not the composer:\n%s", len(composer), strings.Join(got, "\n"))
	}
	// The pane's name is in the composer's top border rather than on a row of
	// its own, which is where Claude Code puts its own. One blank row above it is
	// the breathing room the idle box keeps, so the composer's top is that many
	// rows up from the gap.
	if top := lines[len(lines)-len(composer)]; !strings.Contains(top, roomTitle) {
		t.Errorf("the composer's top edge is %q, want the pane's own name set into it", top)
	}
	if row := lines[len(lines)-len(composer)-composerGap]; strings.TrimSpace(row) != "" {
		t.Errorf("the row above the composer is %q, want the blank gap", row)
	}
	if want := 24 - len(composer) - composerGap; lipgloss.Height(r.tr.view(marked{})) != want {
		t.Errorf("the conversation has %d rows of a 24-row pane, want %d: the composer and the blank row above it are the whole of the rest", lipgloss.Height(r.tr.view(marked{})), want)
	}
}

func TestTheRoomDoesNotReWrapAtASizeAlreadySet(t *testing.T) {
	r := NewRoom().SetSize(80, 24)
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "hello"}, Agent{Name: "sydney"})

	before := countRoomRenders(t, func() { _ = r.View(80, 24) })
	if before != 0 {
		t.Errorf("View re-wrapped %d times at a size SetSize already saw. glamour renders behind one process-global mutex shared by every session, so a room in that state serializes the whole fleet's drawing - and SetSize returns the reader to the newest line, so scrollback would not work at all", before)
	}
	if after := countRoomRenders(t, func() { _ = r.View(70, 24) }); after != 1 {
		t.Errorf("View re-wrapped %d times for a new width, want exactly 1", after)
	}
	if h := countRoomRenders(t, func() { _ = r.View(80, 30) }); h != 0 {
		t.Errorf("a height change cost %d re-wraps, want 0: height moves a window over lines already rendered", h)
	}
}

func TestAnArrivingMessageDoesNotYankAScrolledReaderToTheBottom(t *testing.T) {
	r := NewRoom().SetSize(80, 10)
	for i := range 50 {
		r = r.Append(core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("line %d", i)}, Agent{Name: "sydney"})
	}
	r = r.ScrollUp(20)
	before := r.tr.scroll
	if before == r.tr.bottom() {
		t.Fatalf("the reader is still at the bottom after scrolling back: this fixture cannot tell a room that follows a scrolled reader from one that does not")
	}

	// And the frame drawn at the size SetSize was given shows where they
	// scrolled to, not the newest line.
	//
	// Asserted on the drawn output rather than on r.tr.scroll, which is the
	// whole point: View has a value receiver, so a View that re-lays on every
	// frame leaves the caller's scroll offset untouched and draws the bottom
	// anyway. A field assertion here passes against exactly the bug - scrollback
	// that does not work at all rather than merely being slow - and the render
	// counters cannot see it either, because a re-lay at an unchanged width
	// re-wraps nothing.
	if drawn := ansi.Strip(r.View(80, 10)); strings.Contains(drawn, "line 49") {
		t.Errorf("a reader who scrolled back is shown the newest line anyway. View re-lays only for a size SetSize was not given, and re-laying returns them to the bottom:\n%s", drawn)
	}

	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "and another"}, Agent{Name: "john"})
	if r.tr.scroll != before {
		t.Errorf("scroll moved %d -> %d when a message arrived: at 30 agents somebody reading back would be pulled to the newest line constantly, which is worse than having no scrollback", before, r.tr.scroll)
	}

	// A resize is the deliberate exception: it re-wraps the text, so the offset
	// the reader was holding no longer points at what they were reading, and
	// restoring a stale one is a worse lie than returning to the newest line.
	if resized := r.SetSize(70, 10); resized.tr.scroll != resized.tr.bottom() {
		t.Errorf("after a resize the reader is at %d of %d: a re-wrap invalidates the offset, so a resize returns them to the newest line on purpose", resized.tr.scroll, resized.tr.bottom())
	}

	// And the reader who *was* following still follows, or the guard above is
	// satisfied by a room that never moves at all.
	following := r.ScrollUp(-1000).Append(core.Event{Kind: core.KindAssistantText, Text: "one more"}, Agent{Name: "john"})
	if following.tr.scroll != following.tr.bottom() {
		t.Errorf("a reader at the newest line was left at %d of %d when a message arrived", following.tr.scroll, following.tr.bottom())
	}
}

// Every session's room-worthy events reach this one method whether the room is
// on screen or not, in Bubble Tea's single Update goroutine. Work proportional
// to the conversation makes the whole app slower the longer it runs.
func TestAppendCostsTheSameWhateverTheConversationHasCostSoFar(t *testing.T) {
	short := roomAppendCost(roomOf(scaleShort))
	long := roomAppendCost(roomOf(scaleLong))

	t.Logf("one append: %v at %d events, %v at %d (%.2fx)", short, scaleShort, long, scaleLong, float64(long)/float64(short))
	if ratio := float64(long) / float64(short); ratio > maxCostRatio {
		t.Errorf("one append costs %v at %d events and %v at %d - %.1fx for %dx the conversation, want at most %.1fx",
			short, scaleShort, long, scaleLong, ratio, scaleLong/scaleShort, maxCostRatio)
	}
}

func TestAppendAllocatesTheSameWhateverTheConversationHasCostSoFar(t *testing.T) {
	short := roomBytesPerAppend(roomOf(scaleShort))
	long := roomBytesPerAppend(roomOf(scaleLong))

	t.Logf("one append: %d bytes at %d events, %d at %d (%.2fx)", short, scaleShort, long, scaleLong, float64(long)/float64(short))
	if ratio := float64(long) / float64(short); ratio > maxAllocRatio {
		t.Errorf("one append allocates %d bytes at %d events and %d at %d - %.1fx for %dx the conversation, want at most %.1fx",
			short, scaleShort, long, scaleLong, ratio, scaleLong/scaleShort, maxAllocRatio)
	}
}

// The speaker is stored with the line rather than re-derived when the room is
// re-wrapped. An agent's label is its git branch and it changes when the agent
// changes branch, so re-deriving would silently rewrite the attribution on
// every line of history at the next resize - a conversation that says the
// wrong thing about who said what, produced by dragging a window.
func TestAResizeDoesNotRewriteWhoSaidWhat(t *testing.T) {
	r := NewRoom().SetSize(80, 24)
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "mapped the refresh"}, Agent{ID: "s1", Name: "sydney", Label: "auth-fix"})
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "moved on"}, Agent{ID: "s1", Name: "sydney", Label: "token-rotation"})

	out := ansi.Strip(r.SetSize(70, 24).View(70, 24))
	for _, label := range []string{"auth-fix", "token-rotation"} {
		if !strings.Contains(out, label) {
			t.Errorf("after a re-wrap the room no longer says %q:\n%s", label, out)
		}
	}
}

// An event the room draws nothing for costs no row. The one that reaches here
// today is a withdrawal, which retires a card cards.go owns - and a blank row
// per withdrawal would be a gap in the conversation with nothing in it.
//
// The ask itself is no longer one of them: the room announces a blocked agent,
// because an agent that has stopped and is waiting is the room's own filter.
// See roomBlock's KindPermissionRequest arm.
func TestAnEventWithNoRoomFormAddsNoRowAtAll(t *testing.T) {
	r := NewRoom().SetSize(80, 24)
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "working on it"}, roomScaleAgent)
	before := r.tr.lines.len()

	r = r.Append(core.Event{Kind: core.KindRequestWithdrawn, RequestID: "r1"}, roomScaleAgent)
	if got := r.tr.lines.len(); got != before {
		t.Errorf("an event with no room form added %d rows: it draws nothing, so it must cost nothing", got-before)
	}
	if n := countRoomRenders(t, func() { _ = r.SetSize(70, 24) }); n != 1 {
		t.Errorf("a re-wrap after a dropped event cost %d renders, want 1", n)
	}
}

// seamMethod is the method both conversation panes re-wrap through.
const seamMethod = "renderAll"

// The counting seam is only a seam while nothing goes round it.
//
// This is the blind spot decisions.md names as "a mutation battery inherits
// the blind spots of the tests it runs": every fast-path test above counts
// through renderRoom, so a SetSize that called r.renderAll() directly would
// leave all of them green and passing while the room re-wrapped on every
// frame. Both types' headers say "reach renderAll through this, never
// directly" and until now that was a comment with syntax.
//
// Derived from the package rather than restated: every type declaring the
// method must have exactly one seam variable holding its method expression,
// and nothing anywhere may call the method by name.
func TestNothingReachesRenderAllExceptThroughItsCountingSeam(t *testing.T) {
	declared, seams, calls := renderAllUses(t)
	if len(declared) == 0 {
		t.Fatalf("found no %s method in this package: this guard would pass over anything", seamMethod)
	}
	for _, recv := range declared {
		if seams[recv] == "" {
			t.Errorf("%s.%s has no seam variable holding it: a re-wrap it performs is invisible to the render counters, and every fast-path test in this package stays green while no longer discriminating", recv, seamMethod)
		}
	}
	// There is deliberately no check that a seam still points at a type that
	// declares the method. It cannot be reached by anything that compiles: the
	// seam's type is a method *expression*, func(Room) []block, and
	// countRoomRenders assigns a func of exactly that shape - so a seam rebound
	// to one instance stops this file compiling before it can be asserted on.
	// Three guards no state could reach were written and deleted in Task 9;
	// this is the fourth, deleted for the same reason.
	for _, at := range calls {
		t.Errorf("%s is called directly at %s: it must be reached through its seam, or the count a test asserts on is not the number of re-wraps that happened", seamMethod, at)
	}
}

// renderAllUses parses this package's non-test files for the three things that
// guard holds: which types declare the method, which variables hold it, and
// where it is called by name.
//
// The files are listed and parsed one at a time rather than through
// parser.ParseDir, which is deprecated. Test files are skipped because the
// counting helpers legitimately name the seam.
func renderAllUses(t *testing.T) (declared []string, seams map[string]string, calls []string) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("listing this package: %v", err)
	}
	fset := token.NewFileSet()
	seams = map[string]string{}
	parsed := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		parsed++
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				if recv := receiverName(node); recv != "" {
					declared = append(declared, recv)
				}
			case *ast.ValueSpec:
				collectSeams(node, seams)
			case *ast.CallExpr:
				if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == seamMethod {
					calls = append(calls, fset.Position(node.Pos()).String())
				}
			}
			return true
		})
	}
	if parsed == 0 {
		t.Fatal("parsed no files in this package: this guard would pass over anything")
	}
	return declared, seams, calls
}

// receiverName is the type a seamMethod declaration hangs off, or "" for any
// other function.
func receiverName(fn *ast.FuncDecl) string {
	if fn.Name.Name != seamMethod || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	if id, ok := fn.Recv.List[0].Type.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// collectSeams records every variable initialised with a T.seamMethod method
// expression.
func collectSeams(spec *ast.ValueSpec, into map[string]string) {
	for i, name := range spec.Names {
		if i >= len(spec.Values) {
			continue
		}
		sel, ok := spec.Values[i].(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != seamMethod {
			continue
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			into[id.Name] = name.Name
		}
	}
}

// The fix for "scrollback is dead while a card is pinned" made SetSize's return
// to the newest line conditional. That opens the opposite failure, which is the
// worse of the two: a reader sitting at the newest line watching a live
// conversation, whose pane changes height under them - a card pops up, a notice
// row arrives - and who is then left behind, watching a frame that has silently
// stopped updating. The scrollback bug looks broken; this one looks *finished*.
//
// Its sibling above covers a reader who scrolled away. This covers the one who
// did not, across the change that is not a re-wrap.
func TestAHeightChangeLeavesAFollowingReaderStillFollowing(t *testing.T) {
	r := NewRoom().SetSize(80, 20)
	for i := range 50 {
		r = r.Append(core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("line %d", i)}, Agent{Name: "sydney"})
	}
	if r.tr.scroll != r.tr.bottom() {
		t.Fatalf("the fixture does not start at the newest line (%d of %d), so it cannot tell following from not", r.tr.scroll, r.tr.bottom())
	}

	// Height only, and smaller - what a card being pinned does to the room.
	// The width is unchanged on purpose: a width change re-wraps and returns to
	// the bottom for its own reason, which would satisfy this assertion without
	// the property under test holding at all.
	r = r.SetSize(80, 14)
	if r.tr.scroll != r.tr.bottom() {
		t.Errorf("a card appearing left the reader at %d of %d instead of the newest line", r.tr.scroll, r.tr.bottom())
	}

	// And they are still *following*, not merely still at the bottom: the next
	// message has to reach them. A room that stopped following would pass the
	// assertion above and fail here, which is the failure worth naming.
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "the one they are waiting for"}, Agent{Name: "john"})
	if r.tr.scroll != r.tr.bottom() {
		t.Errorf("a message arriving after the height changed left the reader at %d of %d: the room has silently stopped updating for somebody who is watching it", r.tr.scroll, r.tr.bottom())
	}
	if drawn := ansi.Strip(r.View(80, 14)); !strings.Contains(drawn, "the one they are waiting for") {
		t.Errorf("the newest message is not on screen for a reader who never scrolled away:\n%s", drawn)
	}
}
