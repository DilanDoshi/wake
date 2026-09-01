package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	"↑↓": {{name: "KeyUp", msg: tea.KeyMsg{Type: tea.KeyUp}}, {name: "KeyDown", msg: tea.KeyMsg{Type: tea.KeyDown}}},
	"⇧↑↓": {
		{name: "KeyShiftUp", msg: tea.KeyMsg{Type: tea.KeyShiftUp}},
		{name: "KeyShiftDown", msg: tea.KeyMsg{Type: tea.KeyShiftDown}},
	},
	"⇧←→": {
		{name: "KeyShiftLeft", msg: tea.KeyMsg{Type: tea.KeyShiftLeft}},
		{name: "KeyShiftRight", msg: tea.KeyMsg{Type: tea.KeyShiftRight}},
	},
	"⌃D":   {{name: "KeyCtrlD", msg: tea.KeyMsg{Type: tea.KeyCtrlD}}},
	"⌃Y":   {{name: "KeyCtrlY", msg: tea.KeyMsg{Type: tea.KeyCtrlY}}},
	"⌃B":   {{name: "KeyCtrlB", msg: tea.KeyMsg{Type: tea.KeyCtrlB}}},
	"⌃W":   {{name: "KeyCtrlW", msg: tea.KeyMsg{Type: tea.KeyCtrlW}}},
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
//
// The legend is no longer drawn on every frame - only the armed cue is - but the
// bijection is unchanged: legendEntries is still the canonical list of what this
// build binds, and a key added to App.key without an entry here still fails.
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

// fullLegendWidth is a width wide enough that no composer truncates its armed
// cue. It is no longer the width of a drawn legend - there is no such thing now
// - but a good many tests in this package reuse it as their "render wide"
// value, so it stays as a shared constant.
const fullLegendWidth = 334

// armedCueLine is the full cue an arm draws ignoring width, for a guard that
// wants to name the whole of it. A test helper: production only ever fits the
// cue to a pane through armedCue.
func armedCueLine(arms legendArms) string {
	return strings.Join(armedCueParts(arms), hintSep)
}

// --- the legend is not drawn by default, and only the armed cue is ---------

// An ordinary composer draws no legend row: not the keys, not the permission
// mode. The always-on hints were redundant with the status bar, so they are
// gone; what one composer draws is exactly its box.
func TestUnarmedComposerDrawsNoLegendRow(t *testing.T) {
	out := stripANSI(NewComposer().View(fullLegendWidth))
	if got := lipgloss.Height(out); got != composerViewHeight {
		t.Errorf("an unarmed composer is %d rows, want just the %d-row box - a legend row came back:\n%s",
			got, composerViewHeight, out)
	}
	for _, e := range legendEntries {
		if strings.Contains(out, e.glyph+" "+e.what) {
			t.Errorf("an unarmed composer draws the legend entry %q %q, which moved off the always-on row:\n%s",
				e.glyph, e.what, out)
		}
	}
	if strings.Contains(out, spawnedMode) {
		t.Errorf("an unarmed composer draws the permission mode, which moved to the status bar:\n%s", out)
	}
}

// An armed composer draws exactly one extra row: the cue for that arm, and only
// the entries that arm swaps. Nothing of the old always-on legend is back.
func TestArmedComposerDrawsOnlyTheCue(t *testing.T) {
	for _, tc := range []struct {
		name string
		arms legendArms
	}{
		{"detach", legendArms{detach: true}},
		{"clear draft", legendArms{esc: true}},
		{"rewind", legendArms{rewind: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := stripANSI(NewComposer().WithArms(tc.arms).View(fullLegendWidth))
			if got := lipgloss.Height(out); got != composerViewHeight+1 {
				t.Errorf("an armed composer is %d rows, want the %d-row box plus one cue row:\n%s",
					got, composerViewHeight+1, out)
			}
			rows := strings.Split(out, "\n")
			last := strings.TrimSpace(rows[len(rows)-1])
			if want := armedCueLine(tc.arms); last != want {
				t.Errorf("the cue row is %q, want %q:\n%s", last, want, out)
			}
			// None of the unswapped entries are drawn - the cue is only the arm.
			for _, e := range legendEntries {
				if _, armed := armedLabel(e, tc.arms); !armed && strings.Contains(out, e.glyph+" "+e.what) {
					t.Errorf("the armed composer drew the unswapped entry %q %q:\n%s", e.glyph, e.what, out)
				}
			}
		})
	}
}

// The three cues, spelled out. detach names both keys it swaps - a build that
// swapped only ↵ would leave a live ⌃O with nothing saying it now cancels
// (detach.go's finding), and here that shows up as a one-part cue.
func TestTheArmedCuesAreExactlyTheirLabels(t *testing.T) {
	cases := []struct {
		arms legendArms
		want string
	}{
		{legendArms{detach: true}, sendGlyph + " " + armedSendLabel + hintSep + detachGlyph + " " + armedDetachLabel},
		{legendArms{esc: true}, escGlyph + " " + escClearLabel},
		{legendArms{rewind: true}, escGlyph + " " + escRewindLabel},
	}
	for _, c := range cases {
		if got := armedCueLine(c.arms); got != c.want {
			t.Errorf("%+v cue is %q, want %q", c.arms, got, c.want)
		}
	}
	// No arm draws a cue at all - the default is silence.
	if got := armedCueLine(legendArms{}); got != "" {
		t.Errorf("an unarmed cue is %q, want empty", got)
	}
}

// The cue is cut at a part boundary at every width, never mid-part: a bare `↵`
// advertising nothing, or a fragment that reads as another key's whole label.
// A whole part is dropped from the end rather than a fragment left.
func TestNoWidthDrawsAPartOfTheArmedCue(t *testing.T) {
	for _, arms := range []legendArms{{detach: true}, {esc: true}, {rewind: true}} {
		whole := map[string]bool{}
		for _, part := range armedCueParts(arms) {
			whole[part] = true
		}
		drawn := 0
		for width := 1; width <= fullLegendWidth; width++ {
			cue := armedCue(arms, width)
			if cue == "" {
				continue
			}
			for _, part := range strings.Split(cue, hintSep) {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				drawn++
				if !whole[part] {
					t.Errorf("at %d columns the %+v cue draws %q, which is not a whole part:\n%s",
						width, arms, part, cue)
				}
			}
		}
		if drawn == 0 {
			t.Errorf("the %+v cue drew nothing at any width, so this asserted nothing about truncation", arms)
		}
	}
}

// Every legend entry has its own label.
//
// Two keys sharing one means an operator reads a finished, correct cue and
// attributes it to the wrong key - and it needs no truncation to happen. It is
// belt-and-braces now that the legend is only ever the armed cue, but it is
// cheap and the property still holds.
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

// --- the paragraph in CLAUDE.md that describes all of this ------------------

// claudeMD is the document this package's legend paragraph lives in, from here.
const claudeMD = "../../CLAUDE.md"

// CLAUDE.md's claim about the legend is derived from the legend rather than
// believed.
//
// *"A number in a comment that nothing asserts is wrong by default"* is this
// project's own rule. The paragraph used to carry hardcoded column widths and a
// hand-written 80-column key list, two of which went stale before; the legend
// is no longer drawn on every frame, so those are gone and what is left is the
// armed cue. Each cue part is derived from the renderer here, so a relabelled
// arm fails with the correction in its own message.
//
// Two floors, for the reason that guard has them. The hidden-by-default sentence
// has to be found at all - a reworded paragraph otherwise yields "no violation",
// which reads as the strongest possible pass - and the default cue has to be
// genuinely empty, or the sentence describes a legend that is still drawn.
func TestCLAUDEmdDescribesTheLegendItDraws(t *testing.T) {
	doc, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read %s: %v", claudeMD, err)
	}
	text := string(doc)

	if !strings.Contains(text, hiddenByDefaultPhrase) {
		t.Fatalf("%s makes no %q claim about the legend. That sentence is the whole of what this "+
			"feature changed, and a check that matches nothing reports the strongest possible pass",
			claudeMD, hiddenByDefaultPhrase)
	}

	// The armed cue is the only thing the legend draws now, and CLAUDE.md names
	// each part it can draw. Derived from the renderer so a relabelled arm fails.
	for _, arms := range []legendArms{{detach: true}, {esc: true}, {rewind: true}} {
		for _, part := range armedCueParts(arms) {
			if !strings.Contains(text, part) {
				t.Errorf("%s does not name the armed cue part %q (%+v). The cue is the whole of what "+
					"the legend draws now, and this list is derived from the renderer so it cannot "+
					"drift from what an operator sees", claudeMD, part, arms)
			}
		}
	}

	// And the default really is empty, or the sentence above describes a legend
	// that is still drawn on every frame.
	if got := armedCue(legendArms{}, fullLegendWidth); got != "" {
		t.Fatalf("an unarmed cue is %q, not empty: the legend is being drawn by default again, so "+
			"CLAUDE.md's hidden-by-default claim is false", got)
	}
}

// hiddenByDefaultPhrase is the sentinel the paragraph has to contain, so a
// reworded one that drops the claim fails rather than passing silently.
const hiddenByDefaultPhrase = "drawn only while an arm is live"
