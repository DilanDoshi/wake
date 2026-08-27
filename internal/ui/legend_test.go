package ui

// The hint line under the composer, and the rule it exists to keep: it
// describes only what this build actually does.
//
// The rule's own history is why the guard has to be derived rather than
// listed. ⇧⇥ and ⌃⇧A were both advertised - one moved a label and reached no
// agent, the other was bound to nothing at all - and a legend that lies is
// trusted, which is what makes it worse than a missing feature. A guard that
// iterates a hand-written list of the pairs that already exist pins those and
// enforces nothing about the next one, which is the same failure one level up.
//
// So: the glyphs come out of ui.legendEntries and the keys come out of
// App.key's own switch, parsed. The only hand-written part left is which key a
// glyph stands for, and an entry missing from it fails from either side.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// legendKey is one key a glyph stands for: the tea constant App.key's switch
// has to name, and the message it has to take at runtime.
//
// name is the identifier as keys.go spells it. bubbletea has aliases - KeyEsc
// and KeyEscape are the same value - so switching keys.go to the other spelling
// fails here rather than silently passing, which is the right way round for a
// map whose whole job is to be checkable.
type legendKey struct {
	name string
	msg  tea.KeyMsg
}

// legendKeyNames says which key each glyph in legendEntries stands for.
//
// This is the one thing neither side can derive: ⇞⇟ being two keys, ⌃C being
// KeyCtrlC, are facts about how humans write key names. Everything else is
// read off the source. A glyph with no entry here is a failure, so this cannot
// be the place a new key quietly avoids the check.
var legendKeyNames = map[string][]legendKey{
	"↵":   {{name: "KeyEnter", msg: tea.KeyMsg{Type: tea.KeyEnter}}},
	"esc": {{name: "KeyEsc", msg: tea.KeyMsg{Type: tea.KeyEsc}}},
	"⇥":   {{name: "KeyTab", msg: tea.KeyMsg{Type: tea.KeyTab}}},
	"⇧⇥":  {{name: "KeyShiftTab", msg: tea.KeyMsg{Type: tea.KeyShiftTab}}},
	"↑↓":  {{name: "KeyUp", msg: tea.KeyMsg{Type: tea.KeyUp}}, {name: "KeyDown", msg: tea.KeyMsg{Type: tea.KeyDown}}},
	// The same two tea constants with Alt set, which is how bubbletea reports
	// ⌥↑↓ - so the message is what tells these two entries apart, and this row
	// is what makes the guard press the modifier rather than the bare key.
	"⌥↑↓": {{name: "KeyUp", msg: tea.KeyMsg{Type: tea.KeyUp, Alt: true}}, {name: "KeyDown", msg: tea.KeyMsg{Type: tea.KeyDown, Alt: true}}},
	"⇧←→↑↓": {
		{name: "KeyShiftLeft", msg: tea.KeyMsg{Type: tea.KeyShiftLeft}},
		{name: "KeyShiftRight", msg: tea.KeyMsg{Type: tea.KeyShiftRight}},
		{name: "KeyShiftUp", msg: tea.KeyMsg{Type: tea.KeyShiftUp}},
		{name: "KeyShiftDown", msg: tea.KeyMsg{Type: tea.KeyShiftDown}},
	},
	"⌃D":   {{name: "KeyCtrlD", msg: tea.KeyMsg{Type: tea.KeyCtrlD}}},
	"⌃Y":   {{name: "KeyCtrlY", msg: tea.KeyMsg{Type: tea.KeyCtrlY}}},
	"⌃B":   {{name: "KeyCtrlB", msg: tea.KeyMsg{Type: tea.KeyCtrlB}}},
	"⌃W":   {{name: "KeyCtrlW", msg: tea.KeyMsg{Type: tea.KeyCtrlW}}},
	"⌃G":   {{name: "KeyCtrlG", msg: tea.KeyMsg{Type: tea.KeyCtrlG}}},
	"⌃R":   {{name: "KeyCtrlR", msg: tea.KeyMsg{Type: tea.KeyCtrlR}}},
	"⇞⇟":   {{name: "KeyPgUp", msg: tea.KeyMsg{Type: tea.KeyPgUp}}, {name: "KeyPgDown", msg: tea.KeyMsg{Type: tea.KeyPgDown}}},
	"⌃C":   {{name: "KeyCtrlC", msg: tea.KeyMsg{Type: tea.KeyCtrlC}}},
	"⌃F":   {{name: "KeyCtrlF", msg: tea.KeyMsg{Type: tea.KeyCtrlF}}},
	"⌃O":   {{name: "KeyCtrlO", msg: tea.KeyMsg{Type: tea.KeyCtrlO}}},
	"⌃Q":   {{name: "KeyCtrlQ", msg: tea.KeyMsg{Type: tea.KeyCtrlQ}}},
	"⌃T":   {{name: "KeyCtrlT", msg: tea.KeyMsg{Type: tea.KeyCtrlT}}},
	"⌃X":   {{name: "KeyCtrlX", msg: tea.KeyMsg{Type: tea.KeyCtrlX}}},
	"⌃N⌃P": {{name: "KeyCtrlN", msg: tea.KeyMsg{Type: tea.KeyCtrlN}}, {name: "KeyCtrlP", msg: tea.KeyMsg{Type: tea.KeyCtrlP}}},
	"⌃E":   {{name: "KeyCtrlE", msg: tea.KeyMsg{Type: tea.KeyCtrlE}}},
}

// Both directions, and neither is the one that already passed.
//
//   - a glyph in the legend whose key App.key does not take is the original
//     failure: an advertised control that silently does nothing.
//   - a key App.key takes that no glyph names is this build's own hazard -
//     binding the key that stops a runaway turn and telling nobody.
func TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed(t *testing.T) {
	bound := keyCasesIn(t, "keys.go", "key")
	if len(bound) < len(legendKeyNames) {
		t.Fatalf("found %d tea.Key cases in App.key but %d glyphs are mapped: the scan is broken and this test is asserting nothing", len(bound), len(legendKeyNames))
	}
	if len(legendEntries) == 0 {
		t.Fatal("the legend is empty: this test is asserting nothing")
	}

	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex")
	named := map[string]bool{}

	for _, e := range legendEntries {
		keys, ok := legendKeyNames[e.glyph]
		if !ok {
			t.Errorf("the legend advertises %q (%s) and nothing says which key that is: name it here, or stop advertising it", e.glyph, e.what)
			continue
		}
		for _, k := range keys {
			named[k.name] = true
			if !bound[k.name] {
				t.Errorf("the legend advertises %q (%s) but App.key's switch has no case for tea.%s: an advertised key that does nothing is trusted, which is what makes it worse than a missing one", e.glyph, e.what, k.name)
			}
			if _, _, handled := a.key(k.msg); !handled {
				t.Errorf("the legend advertises %q but App.key does not take %v - it falls through to the composer", e.glyph, k.msg)
			}
		}
	}

	for name := range bound {
		if !named[name] {
			t.Errorf("App.key binds tea.%s and no legend entry names it: a key nobody is told about is a key nobody presses", name)
		}
	}
}

// keyCasesIn returns every tea.Key… named in the switch inside one method.
func keyCasesIn(t *testing.T, file, method string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	out := map[string]bool{}
	found := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != method || fn.Recv == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				if name := teaKeyName(expr); name != "" {
					out[name] = true
				}
			}
			return true
		})
	}
	if !found {
		t.Fatalf("%s has no method %q: the scan is looking at the wrong thing", file, method)
	}
	return out
}

// teaKeyName pulls Key… out of a `tea.Key…` expression, or "" for anything
// else.
func teaKeyName(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "tea" || !strings.HasPrefix(sel.Sel.Name, "Key") {
		return ""
	}
	return sel.Sel.Name
}

// narrowLegendWidth is a pane too narrow for the whole legend.
//
// Chosen rather than picked: the full legend is 230 cells plus one column of
// indent, the keys are the first 210 of those, and this leaves 214 - room for
// every key and not for "permissions: auto". It is well under the 231 at which
// everything fits, and an inverted order would spend its first 20 cells on the
// mode and lose the tail of the keys - which is what the first draft of this
// test failed to notice: it asserted only that the glyphs were present, so
// putting the mode *first* left it passing while the legend cut "scroll"
// instead.
//
// Both bounds are derived below rather than trusted here. This comment is
// arithmetic on a legend that has now grown four times, and a number nothing
// asserts is wrong by default: ⌃F took the keys from 133 to 143, which broke
// *this* constant before it broke fullLegendWidth, the rebinding took them
// to 174 - ⌃O detach and ⌃Q quit & park all added, ⌃C relabelled park - and
// ⌃T mention mode took them to 192, ↑↓ pick agent to 208, spelling ⎋ as
// `esc` - a glyph the owner did not recognise - to 210, and the grid keys
// `⌃Y open right` and `⌃B open below` to 240 (with `⌃W close DM` relabelled
// `close pane`, two cells wider). They arrived spelled `⌃⇧→` and `⌃⇧↓`, which
// was 242 - one cell each for a chord macOS keeps.
//
// 293 is the **widest** width that still shows every key and no character of the
// mode: the keys are 289 cells, the separator after them is 3, and the indent is
// 1. It was 267 until the grid keys stopped being ⌃⇧→ and ⌃⇧↓ - macOS never
// delivered either - and became ⌃Y and ⌃B, one cell narrower each; then 265;
// then `⇧←→↑↓ move focus` took the keys from 261 to 280, and `⌃E expand tool
// results` took them to 289. Those last two are one merge rather than two
// rebindings: they were built on branches off the same commit and landed
// together, so their 19 and 9 cells are additive rather than alternatives.
// A width above that draws part of `permissions: auto`, and a legend that
// cuts a word in half is the thing ⇧⇥ was taken out of this line for. The test's
// name says the mode is dropped, so the constant has to be a width where it is
// gone rather than merely unrecognisable. Both ends are asserted in
// TestTheLegendFitsInTheWidthItClaims, which is what makes the numbers above a
// record of a measurement rather than a claim.
//
// It was 214 until ⇧⇥ became the permission mode: `⇧⇥ next blocked` became
// `⇧⇥ permissions` and `⌃X next blocked` arrived beside it, which is 17 cells of
// key net. Then 231 until the grid's keys merged in beside them, and 311 until
// `⌥↑↓ prompt history` arrived beside `⌃N⌃P dispatches` - the entry is 18 cells
// and the separator in front of it is 3.
const narrowLegendWidth = 335

// What a legend too long for its pane loses, and in what order.
//
// The keys come first and the mode last, which is the priority: a key is
// something to do and the mode is something to know. If the order ever
// inverts, a narrow pane starts hiding keys instead - including the one that
// stops a runaway turn.
func TestANarrowLegendKeepsTheKeysAndDropsTheMode(t *testing.T) {
	out := stripANSI(NewComposer().View(narrowLegendWidth))

	for _, e := range legendEntries {
		if !strings.Contains(out, e.glyph) {
			t.Errorf("a %d-column composer dropped the %q key from its legend:\n%s", narrowLegendWidth, e.glyph, out)
		}
	}
	// The half the first draft was missing. Without it the test is satisfied
	// by any order at all, including one that keeps the mode and cuts a key.
	if strings.Contains(out, spawnedMode) {
		t.Errorf("a %d-column composer kept the mode, so something else was cut to make room for it:\n%s", narrowLegendWidth, out)
	}
}

// And the whole legend fits once there is room for it, so the test above is
// measuring a truncation rather than a legend that never had a mode.
func TestAWideLegendShowsEverything(t *testing.T) {
	out := stripANSI(NewComposer().View(fullLegendWidth))
	for _, e := range legendEntries {
		if !strings.Contains(out, e.glyph+" "+e.what) {
			t.Errorf("the full-width legend is missing %q %q:\n%s", e.glyph, e.what, out)
		}
	}
	if !strings.Contains(out, spawnedMode) {
		t.Errorf("the full-width legend is missing the permission mode:\n%s", out)
	}
}

// fullLegendWidth is the narrowest pane that shows the whole legend: 312 cells
// of legend plus hintIndentWidth. Stated as a number because it is a fact
// about this build worth noticing when it changes, and it has changed five
// times: ⇥ took pane focus and ⇧⇥ took the next-blocked jump, which was 15
// columns more legend than the 139 the room shipped with; then ⌃F added the ten
// that made it 164; then the rebinding added 31 more - `⌃O detach` and
// `⌃Q park all & quit` are two new entries, and `⌃C park` is four cells shorter
// than `⌃C detach` was; then `⌃T mention mode` added 18, and `↑↓ pick agent`
// - the key that makes every other conversation reachable - added 16; then the
// grid keys added 34, which is the largest single jump this line has taken and
// the most deliberate: ⌃Y and ⌃B are the whole of the bounded grid's keyboard
// surface, and they sit *after* ⌃D because a pane you cannot place is still a
// pane you can open. They added 36 while they were spelled ⌃⇧→ and ⌃⇧↓, and
// gave two cells back when macOS turned out to keep both chords.
//
// The fifth is this one: ⇧⇥ gave up the next-blocked jump for the permission
// mode it was always meant to carry, and next-blocked moved to ⌃X rather than
// being dropped - one shorter entry and one whole new one, 17 cells net.
//
// The sixth is not a rebinding at all but a merge: the grid keys and the
// permission mode were built on branches off the same commit and landed
// together, so the two jumps above are additive rather than alternatives.
//
// The seventh is `⇧←→↑↓ move focus`, 19 cells: the ⌘+arrow that was asked for
// is named by bubbletea for no byte sequence at all, and ⌃+arrow is spent on
// spaces and Mission Control, so shift is the one arrow family left. One entry
// carries all four keys because four would cost 76 cells to say one thing.
//
// The eighth is `⌃E expand tool results`, 12 cells, and it is the seventh's
// twin rather than its successor: both were built on branches off the same
// commit and landed in the same merge, so the two jumps are additive.
//
// That is a real cost and it is paid deliberately. **Every ordinary terminal
// now truncates it**: the room pane is the terminal less the sidebars and the
// frame, so the whole legend needs a terminal of legendFitsAtTerminalWidth
// columns room-only, and at 200 - the width this was first measured at - the
// room pane is 164 and 31 short. "Fits in no pane this product has" was the
// first draft of that sentence and is false on a large monitor at a small font,
// which is a setup people run. So what is on screen is decided by the *order*
// of legendEntries rather than by the length, which is why that order is a
// priority statement, why ⌃O sits third and ⌃C fourth - leaving is what a pane
// too narrow to explain itself must still advertise - why ⇥ sits fifth, and why
// ⌃Q sits last.
//
// The seventh is `⌥↑↓ prompt history`, which is 21 cells. It sits below `↑↓
// pick agent` because it is those two keys under a modifier, and its cost is
// paid at the far end of the truncation rather than at the near one: a pane too
// narrow for it is a pane where recalling a prompt is a convenience and
// leaving, parking and answering are not.
//
// The constant is not therefore pointless: it is what
// TestAWideLegendShowsEverything renders at, so the truncation test above is
// measuring a cut rather than a legend that never had a mode.
const fullLegendWidth = 352

func TestTheLegendFitsInTheWidthItClaims(t *testing.T) {
	if got := len([]rune(hintLine(spawnedMode))) + hintIndentWidth; got != fullLegendWidth {
		t.Errorf("the legend needs %d columns, fullLegendWidth says %d: update it deliberately", got, fullLegendWidth)
	}
	if fullLegendWidth <= narrowLegendWidth {
		t.Errorf("narrowLegendWidth (%d) is not narrower than the legend (%d), so the truncation test truncates nothing", narrowLegendWidth, fullLegendWidth)
	}
	// And the other end of narrowLegendWidth, which nothing asserted until ⌃F
	// broke it: the narrow pane has to be wide enough for every key, or
	// TestANarrowLegendKeepsTheKeysAndDropsTheMode fails as a *legend* defect
	// while the real defect is this constant. Derived from the production
	// strings rather than restated, so the next key moves it by construction.
	keys := legendKeysWidth()
	if narrowLegendWidth-hintIndentWidth < keys {
		t.Errorf("the keys alone need %d columns and narrowLegendWidth is %d: the truncation test is now asserting that a key survives a width that cannot hold it", keys, narrowLegendWidth)
	}
	// And the other side of it, which nothing asserted while the constant sat at
	// 150 and rendered `permissions: per`. A width that cuts the mode mid-word
	// satisfies "does not contain auto" and is not what that test claims to be
	// measuring.
	if last := keys + len([]rune(hintSep)) + hintIndentWidth; narrowLegendWidth > last {
		t.Errorf("narrowLegendWidth is %d and anything above %d draws part of the mode: the truncation test says the mode is dropped, so the width has to be one where it is gone rather than truncated", narrowLegendWidth, last)
	}
}

// legendKeysWidth is how many cells the key half of the legend takes: the whole
// hint line, less the separator and the mode that follow it.
//
// Subtracted from hintLine rather than re-joining legendEntries, because a
// second copy of the join is a second thing to keep in step with the first -
// and this number's whole job is to be the one the composer actually renders.
func legendKeysWidth() int {
	return len([]rune(hintLine(spawnedMode))) -
		len([]rune(hintSep)) -
		len([]rune(fmt.Sprintf(modeFormat, spawnedMode)))
}

// --- and the paragraph in CLAUDE.md that describes all of this -------------

// claudeMD is the document this package's legend paragraph lives in, from here.
const claudeMD = "../../CLAUDE.md"

// eightyColumnPane is the width the sentence under test names. It is the
// takeover width - one pane, no room beside it - and the narrowest a laptop
// terminal is ever really set to.
const eightyColumnPane = 80

// legendWidthClaim matches "needs **N columns**", which is a shape that appears
// nowhere else in that document.
var legendWidthClaim = regexp.MustCompile(`needs \*\*([0-9]+) columns\*\*`)

// terminalClaim matches "terminal at least **N** columns wide".
var terminalClaim = regexp.MustCompile(`terminal at least \*\*([0-9]+)\*\* columns wide`)

// legendFitsAtTerminalWidth is the narrowest *terminal* whose room pane can
// draw the whole legend: room-only, both sidebars open, which is the widest
// pane this product produces.
//
// Derived rather than stated, because the first draft of the sentence it backs
// said the legend "fits in no pane this product has" - true of the 200-column
// terminal it was measured at and false on a large monitor at a small font,
// which is an ordinary setup rather than a corner. The room pane is the
// terminal less the two sidebars and the frame, so this is a search rather than
// arithmetic: Regions owns that relation and is allowed to change it.
func legendFitsAtTerminalWidth(t *testing.T) int {
	t.Helper()
	for width := fullLegendWidth; width < fullLegendWidth*3; width++ {
		roomOnly := Layout{Width: width, Height: 40, ShowGroups: true, ShowRoster: true}
		if roomOnly.Regions(1, 0).Room() >= fullLegendWidth {
			return width
		}
	}
	t.Fatalf("no terminal up to %d columns gives a room pane of %d: either Regions changed shape or "+
		"the search is broken, and every claim resting on it asserts nothing", fullLegendWidth*3, fullLegendWidth)
	return 0
}

// eightyColumnClaim matches the backticked key list in "An 80-column pane keeps
// `…`".
var eightyColumnClaim = regexp.MustCompile("80-column pane keeps `([^`]+)`")

// lostClaim matches the two entries named in "…loses the rest, which now
// includes `⌃F fork` and `⌃Q quit & park all`".
//
// **A subset claim rather than a list**, which is why it is matched as two
// captures instead of joined the way the kept list is: more than two entries are
// lost at 80 columns, and the sentence names the two the branch added. What is
// checked is that each named pair is a real legend entry, spelled the way the
// legend spells it, and that it is genuinely one of the lost.
var lostClaim = regexp.MustCompile("loses the\nrest, which now includes `([^`]+)` and `([^`]+)`")

// CLAUDE.md's two claims about the legend are derived from the legend rather
// than believed.
//
// *"A number in a comment that nothing asserts is wrong by default"* is this
// project's own rule, it has been broken five times, and **this paragraph is
// where two of those five were**: the width went stale when ⇧⇥ arrived and the
// key list has never been checked at all. The fifth instance was fixed by
// deriving the sentence rather than by re-counting it
// (TestCLAUDEmdNamesTheTwoLargestNonTestFiles), and that is the move repeated
// here - a rebinding that changes what an 80-column pane shows now fails with
// the correction in its own message.
//
// Both claims are asserted because the paragraph makes both, and they fail
// independently: a legend can grow without changing what survives 80 columns,
// and the order can change without moving the total width by a cell.
//
// Two floors, for the reason that guard has them. Each sentence has to be found
// at all - a reworded paragraph otherwise yields "no violation", which reads as
// the strongest possible pass - and the 80-column pane has to actually be
// dropping something, or the second claim is a restatement of legendEntries.
func TestCLAUDEmdDescribesTheLegendItDraws(t *testing.T) {
	doc, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read %s: %v", claudeMD, err)
	}

	width := legendWidthClaim.FindSubmatch(doc)
	if width == nil {
		t.Fatalf("%s makes no `needs **N columns**` claim about the legend. Either the sentence was "+
			"reworded - in which case this check has to follow it - or it is gone, and a check that "+
			"matches nothing reports the strongest possible pass", claudeMD)
	}
	if got, want := string(width[1]), strconv.Itoa(fullLegendWidth); got != want {
		t.Errorf("%s says the legend needs %s columns; it needs %s. The number is re-measured rather "+
			"than adjusted, which is this project's own rule", claudeMD, got, want)
	}

	terminal := terminalClaim.FindSubmatch(doc)
	if terminal == nil {
		t.Fatalf("%s makes no `terminal at least **N** columns wide` claim. That sentence is the premise "+
			"of \"the order is what decides what is on screen\", and its first draft - \"fits in no pane "+
			"this product has\" - was false above a width people actually run", claudeMD)
	}
	if got, want := string(terminal[1]), strconv.Itoa(legendFitsAtTerminalWidth(t)); got != want {
		t.Errorf("%s says the whole legend needs a terminal of %s columns; it needs %s. The room pane is "+
			"the terminal less the sidebars and the frame, and Regions owns that relation", claudeMD, got, want)
	}

	kept := eightyColumnClaim.FindSubmatch(doc)
	if kept == nil {
		t.Fatalf("%s makes no claim about what an 80-column pane keeps, and that sentence is what says "+
			"the order of legendEntries is a priority statement rather than a grouping", claudeMD)
	}
	drawn := entriesDrawnWhole(eightyColumnPane)
	if len(drawn) == len(legendEntries) {
		t.Fatalf("an %d-column pane draws all %d legend entries, so the sentence about what it keeps is a "+
			"restatement of legendEntries and asserts nothing about the order",
			eightyColumnPane, len(legendEntries))
	}
	if got, want := string(kept[1]), strings.Join(drawn, " "); got != want {
		t.Errorf("%s says an %d-column pane keeps `%s`; it keeps `%s`.\n"+
			"That list is the whole evidence for the order being a priority statement, and the "+
			"rebinding is exactly the kind of change that moves it without moving anything a test "+
			"would otherwise see", claudeMD, eightyColumnPane, got, want)
	}

	// And the two entries the same sentence says are *lost*, which is the half
	// nothing read.
	//
	// The kept list is glyphs, because entriesDrawnWhole returns glyphs. The
	// lost pair is quoted with **labels**, and the labels were believed: the
	// sentence said `⌃Q park all & quit` while the legend had reordered it to
	// `quit & park all` inside this same branch, and recorded four lines of
	// reason for the reorder - `park all & quit` right-truncates to `⌃Q park`,
	// which is ⌃C's whole label, so the legend advertised two keys with one
	// word and one of them closes the workspace.
	//
	// What makes that worth deriving rather than correcting is where it sat:
	// one clause away from three derived numbers, inside a sentence whose own
	// boldface claim is that it is derived. An underived value has no stronger
	// protection available to it than standing next to derived ones.
	lost := lostClaim.FindSubmatch(doc)
	if lost == nil {
		t.Fatalf("%s makes no `loses the rest, which now includes ...` claim naming two entries. That "+
			"clause is where the labels are quoted, and a check that matches nothing reports the "+
			"strongest possible pass", claudeMD)
	}
	for _, quoted := range []string{string(lost[1]), string(lost[2])} {
		entry, spelled := legendEntryNamed(quoted)
		switch {
		case !spelled:
			t.Errorf("%s quotes `%s` as a legend entry and the legend has no such (glyph, label) pair. "+
				"The label is the half nothing read: `⌃Q park all & quit` survived here after "+
				"the legend reordered it to `quit & park all`, because `park all & quit` truncates "+
				"to ⌃C's whole label", claudeMD, quoted)
		case slices.Contains(drawn, entry.glyph):
			t.Errorf("%s says an %d-column pane loses `%s`, and it keeps it. The sentence's argument is "+
				"that the *order* decides what survives, so an entry on both sides of it is that "+
				"argument stating its own counterexample", claudeMD, eightyColumnPane, quoted)
		}
	}
}

// legendEntryNamed resolves a `glyph label` string to the legend entry that
// spells it exactly, so a quoted label cannot drift from the rendered one.
func legendEntryNamed(quoted string) (legendEntry, bool) {
	for _, e := range legendEntries {
		if e.glyph+" "+e.what == quoted {
			return e, true
		}
	}
	return legendEntry{}, false
}

// entriesDrawnWhole is the glyphs of the legend entries a pane of this width
// draws **with their labels**, in order.
//
// Whole entries rather than surviving glyphs, and the difference is the defect
// TestANarrowLegendKeepsTheKeysAndDropsTheMode was written against: the cut is a
// plain right-truncation, so the entry straddling the edge keeps its glyph and
// loses its label. A glyph with no word beside it is not an advertised key - it
// is the ragged end of a line - and counting it is how the old sentence came to
// name ⌃D.
func entriesDrawnWhole(width int) []string {
	out := stripANSI(NewComposer().View(width))
	var kept []string
	for _, e := range legendEntries {
		if strings.Contains(out, e.glyph+" "+e.what) {
			kept = append(kept, e.glyph)
		}
	}
	return kept
}

// --- a truncated label may not read as a different key's whole label -------

// Every legend entry has its own label.
//
// Two keys sharing one means an operator reads a finished, correct entry and
// attributes it to the wrong key - and it needs no truncation to happen.
//
// # What this used to be, and why the rest of it is gone
//
// It was TestNoWidthCutsALegendEntryIntoADifferentKeysLabel: it scanned every
// width for an entry the *cell* truncation had cut short, and required that
// what remained never read as another key's whole label. `⌃Q`'s label is
// `quit & park all` rather than `park all & quit` because of it - a cut landing
// after `park` would otherwise have read as `⌃C park`.
//
// The legend now cuts at an entry boundary (see hintFitting), so no width
// truncates an entry at all - and that test's own vacuity floor said so, which
// is the floor doing its job rather than an inconvenience. Its scan is
// subsumed strictly: TestNoWidthDrawsAPartOfALegendEntry requires every part
// drawn to be a whole entry, and if no fragment is ever drawn then no fragment
// can read as anything. Reverting hintFitting to a cell cut fails there.
//
// What did not depend on truncation is this, so this is what remains. `⌃Q`'s
// label ordering is now belt-and-braces rather than load-bearing, and is kept
// for the same reason.
func TestEveryLegendEntryHasItsOwnLabel(t *testing.T) {
	whole := map[string]string{}
	for _, e := range legendEntries {
		if owner, taken := whole[e.what]; taken {
			t.Errorf("%s and %s are both labelled %q, so the legend names one thing twice and an "+
				"operator cannot tell which key they are reading", owner, e.glyph, e.what)
		}
		whole[e.what] = e.glyph
	}
	if len(whole) != len(legendEntries) {
		t.Errorf("%d entries share %d labels", len(legendEntries), len(whole))
	}
}

// And an *armed* legend has its own labels too, which is the half a check over
// legendEntries alone cannot see: the arms swap labels at render time.
//
// The ⌃O arm is the case that needs it. It swaps two entries rather than one -
// `↵ send` to `↵ detach` and `⌃O detach` to `⌃O cancel` - and a build that
// swapped only the first would draw `detach` twice, on the key that does it and
// on the key that no longer does. See detach.go.
func TestEveryArmedLegendEntryHasItsOwnLabelToo(t *testing.T) {
	for _, arms := range []legendArms{{esc: true}, {detach: true}} {
		whole := map[string]bool{}
		for _, part := range hintParts(spawnedMode, arms) {
			if whole[part] {
				t.Errorf("with %+v the legend draws %q twice:\n%s", arms, part,
					strings.Join(hintParts(spawnedMode, arms), hintSep))
			}
			whole[part] = true
		}
		glyphs := map[string]string{}
		for i, e := range legendEntries {
			label := strings.TrimPrefix(hintParts(spawnedMode, arms)[i], e.glyph+" ")
			if owner, taken := glyphs[label]; taken {
				t.Errorf("with %+v, %s and %s are both labelled %q, so the legend names one thing "+
					"twice and an operator cannot tell which key does it", arms, owner, e.glyph, label)
			}
			glyphs[label] = e.glyph
		}
	}
}

// No width draws half an entry.
//
// The legend is cut to the width of a *pane*, and CLAUDE.md records that every
// ordinary terminal truncates it — so the truncation is not an edge case, it is
// what almost every operator sees. Cutting by cell left the line ending in a
// bare glyph advertising nothing:
//
//	… ⇥ next chat   ⇧⇥ next blocked   ⌃D
//
// The dangerous half of that was already closed: `⌃Q`'s label was reordered to
// `quit & park all` so no cut can make it read as another key's whole label,
// and TestNoWidthCutsALegendEntryIntoADifferentKeysLabel holds it as a class.
// This is the ragged half, and cutting at an entry boundary closes both.
//
// # Why it walks every width rather than sampling
//
// The last legend defect shipped *because* its guard sampled: the narrow test
// looked for each entry's glyph at one chosen width, and at that width the
// glyph survived while its label was cut. A property about truncation cannot be
// checked at a width somebody picked — the cut lands somewhere different for
// every one, and the interesting ones are precisely the widths nobody thought
// of.
//
// It splits on the separator and requires every part to be a whole entry. That
// is exact where `strings.Contains` is not: `⇥` is a substring of `⇧⇥`, so a
// containment check cannot tell a present key from a fragment of another.
func TestNoWidthDrawsAPartOfALegendEntry(t *testing.T) {
	whole := map[string]bool{fmt.Sprintf(modeFormat, spawnedMode): true}
	for _, e := range legendEntries {
		whole[e.glyph+" "+e.what] = true
	}

	drawn := 0
	for width := 1; width <= fullLegendWidth+2; width++ {
		line := hintRow(t, width)
		if line == "" {
			continue
		}
		for _, part := range strings.Split(line, hintSep) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			drawn++
			if !whole[part] {
				t.Errorf("at %d columns the legend draws %q, which is not a whole entry. A cut that "+
					"leaves half an entry advertises a key without saying what it does, and at the "+
					"other end of the same cut it can read as a different key's label:\n%s",
					width, part, line)
			}
		}
	}

	// A floor, because the loop above is satisfied by a legend that is empty at
	// every width — which is exactly what an off-by-one in the fitting would
	// produce, and it would read as a pass.
	if drawn == 0 {
		t.Fatal("no legend entry was drawn at any width, so this asserted nothing about truncation")
	}
}

// hintRow is the legend as the composer draws it at one width.
//
// The hint is the last row the composer renders, and it is the only one that
// can contain the mode — which is what separates it from the target line above
// it without depending on either one's position.
func hintRow(t *testing.T, width int) string {
	t.Helper()
	out := stripANSI(NewComposer().View(width))
	for _, row := range strings.Split(out, "\n") {
		if strings.Contains(row, legendEntries[0].glyph) {
			return strings.TrimSpace(row)
		}
	}
	return ""
}
