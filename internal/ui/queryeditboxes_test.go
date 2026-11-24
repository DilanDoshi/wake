package ui

// The two boxes that edit their filter like the chat box: what a key does to the
// query and the caret, what it leaves alone, and what is drawn.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func jumpPressed(t testing.TB, a App, keys ...tea.KeyMsg) App {
	t.Helper()
	for _, k := range keys {
		a, _ = pressKey(a, k)
		if !a.jumpPicker.Open() {
			t.Fatalf("%q closed the box", k.String())
		}
	}
	return a
}

func TestTheJumpFilterEditsAtItsCaret(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "blue")
	a = jumpPressed(t, a, kmsg(tea.KeyLeft), kmsg(tea.KeyLeft), runeKey("x"))
	if q, back := a.jumpPicker.Query, a.jumpPicker.Back; q != "blxue" || back != 2 {
		t.Fatalf("← ← and x gave %s, want blx|ue", showCaret(q, back))
	}
	a = jumpPressed(t, a, kmsg(tea.KeyRight))
	if q, back := a.jumpPicker.Query, a.jumpPicker.Back; showCaret(q, back) != "blxu|e" {
		t.Fatalf("→ gave %s, want blxu|e", showCaret(q, back))
	}
	a = jumpPressed(t, a, kmsg(tea.KeyLeft), kmsg(tea.KeyBackspace), kmsg(tea.KeyHome), kmsg(tea.KeyDelete))
	if q, back := a.jumpPicker.Query, a.jumpPicker.Back; showCaret(q, back) != "|lue" {
		t.Errorf("⌫, Home, ⌦ gave %s, want |lue", showCaret(q, back))
	}
	a = jumpPressed(t, a, kmsg(tea.KeyEnd), altKey(tea.KeyBackspace))
	if q, back := a.jumpPicker.Query, a.jumpPicker.Back; showCaret(q, back) != "|" {
		t.Errorf("End, ⌥⌫ gave %s, want an empty filter", showCaret(q, back))
	}
	a = jumpTyped(a, "web blur")
	a = jumpPressed(t, a, kmsg(tea.KeyCtrlH)) // the byte a terminal's ⌫ or ⌃⌫ sends: one rune, as in the composer
	if got := a.jumpPicker.Query; got != "web blu" {
		t.Errorf("⌃H over web blur left %q, want %q", got, "web blu")
	}
	a = jumpPressed(t, a, runeKey("r"), altKey(tea.KeyBackspace))
	if got := a.jumpPicker.Query; got != "web " {
		t.Errorf("⌥⌫ over web blur left %q, want %q", got, "web ")
	}
	a = jumpPressed(t, a, kmsg(tea.KeyCtrlU))
	if got := a.jumpPicker.Query; got != "" {
		t.Errorf("⌃U left %q", got)
	}
}

func TestAPasteLandsAtTheJumpFiltersCaret(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "wb")
	a = jumpPressed(t, a, kmsg(tea.KeyLeft))
	a = jumpPressed(t, a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e\n"), Paste: true})
	if q, back := a.jumpPicker.Query, a.jumpPicker.Back; showCaret(q, back) != "we |b" {
		t.Errorf("a paste of e and a newline gave %s, want we |b", showCaret(q, back))
	}
}

// A move leaves the cursored agent where it is; a change of the text re-homes it.
func TestMovingTheJumpCaretKeepsTheCursorAndEditingTheTextHomesIt(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "blu")
	a = jumpPressed(t, a, kmsg(tea.KeyDown))
	moved := jumpCursorName(t, a)
	if moved == jumpNames(a)[0] {
		t.Fatalf("↓ left the cursor on the first row, %q", moved)
	}
	a = jumpPressed(t, a, kmsg(tea.KeyLeft), kmsg(tea.KeyHome), kmsg(tea.KeyEnd), altKey(tea.KeyLeft))
	if got := jumpCursorName(t, a); got != moved {
		t.Errorf("moving the caret moved the cursor from %q to %q", moved, got)
	}
	a = jumpPressed(t, a, kmsg(tea.KeyCtrlK)) // the caret is at the start, so this empties the filter
	if got := jumpCursorName(t, a); got != jumpNames(a)[0] {
		t.Errorf("a change of the text left the cursor on %q, want the best match %q", got, jumpNames(a)[0])
	}
}

func TestTheJumpFilterDrawsItsCaretWhereItIs(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "ab")
	if row := jumpPanes(a)[rowHaving(jumpPanes(a), "⌕")]; !strings.Contains(row, "⌕ ab"+jumpCaret) {
		t.Errorf("a caret at the end draws %q", row)
	}
	a = jumpPressed(t, a, kmsg(tea.KeyLeft))
	if row := jumpPanes(a)[rowHaving(jumpPanes(a), "⌕")]; !strings.Contains(row, "⌕ a"+jumpCaret+"b") {
		t.Errorf("a caret between a and b draws %q", row)
	}
	b := jumpOpened(t, blueFleet(t, 140, 40))
	if row := jumpPanes(b)[rowHaving(jumpPanes(b), "⌕")]; !strings.Contains(row, jumpHint+jumpCaret) {
		t.Errorf("an empty filter draws %q, want the hint and the caret", row)
	}
}

// The keys Wake takes in the composer are not the box's: they stay swallowed, so
// ⌃W does not close a pane from under the box and ⌃A does not toggle the narrowing.
func TestTheKeysWakeClaimsStaySwallowedInTheJumpFilter(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyCtrlW, tea.KeyCtrlA, tea.KeyCtrlE, tea.KeyCtrlV, tea.KeyCtrlT, tea.KeyCtrlN, tea.KeyCtrlP} {
		a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "blue")
		a = jumpPressed(t, a, kmsg(tea.KeyLeft))
		_, _, handled := a.jumpKey(kmsg(k)) // handled: App.key's switch never sees it, so the composer never does
		if !handled {
			t.Errorf("%s leaked past the box", tea.Key{Type: k}.String())
		}
		after := jumpPressed(t, a, kmsg(k))
		if after.jumpPicker.Query != "blue" || after.jumpPicker.Back != 1 || after.focus != a.focus {
			t.Errorf("%s changed the box: %s", tea.Key{Type: k}.String(), showCaret(after.jumpPicker.Query, after.jumpPicker.Back))
		}
	}
}

func TestAJumpOpenedWithAFilterHasItsCaretAtTheEnd(t *testing.T) {
	a, _ := blueFleet(t, 140, 40).jump("blu")
	if a.jumpPicker.Query != "blu" || a.jumpPicker.Back != 0 {
		t.Errorf("/jump blu opened with %s", showCaret(a.jumpPicker.Query, a.jumpPicker.Back))
	}
}

func resumePressed(t testing.TB, a App, keys ...tea.KeyMsg) App {
	t.Helper()
	for _, k := range keys {
		next, _, handled := a.resumePickerKey(k)
		if !handled {
			t.Fatalf("%q was not handled", k.String())
		}
		a = next
	}
	return a
}

func TestTheResumeSearchEditsAtItsCaret(t *testing.T) {
	a := openPicker(twoRows(), false)
	a = resumePressed(t, a, runeKey("a"), runeKey("b"), runeKey("c"), kmsg(tea.KeyLeft), runeKey("x"))
	if q, back := a.resumePicker.Query, a.resumePicker.Back; showCaret(q, back) != "abx|c" {
		t.Fatalf("a b c ← x gave %s, want abx|c", showCaret(q, back))
	}
	a = resumePressed(t, a, kmsg(tea.KeyHome), kmsg(tea.KeyDelete), kmsg(tea.KeyRight), kmsg(tea.KeyCtrlK))
	if q, back := a.resumePicker.Query, a.resumePicker.Back; showCaret(q, back) != "b|" {
		t.Errorf("Home ⌦ → ⌃K gave %s, want b|", showCaret(q, back))
	}
	a = resumePressed(t, a, runeKey("cd"), kmsg(tea.KeyCtrlH))
	if got := a.resumePicker.Query; got != "bc" {
		t.Errorf("⌃H over bcd left %q, want bc", got)
	}
	a = resumePressed(t, a, kmsg(tea.KeyHome), kmsg(tea.KeyEnd))
	if back := a.resumePicker.Back; back != 0 {
		t.Errorf("Home then End left the caret %d from the end", back)
	}
	a = resumePressed(t, a, kmsg(tea.KeyLeft), kmsg(tea.KeyCtrlU))
	if q, back := a.resumePicker.Query, a.resumePicker.Back; showCaret(q, back) != "|c" {
		t.Errorf("← ⌃U over bc gave %s, want |c", showCaret(q, back))
	}
	a = resumePressed(t, a, kmsg(tea.KeyEnd), altKey(tea.KeyBackspace))
	if got := a.resumePicker.Query; got != "" {
		t.Errorf("⌥⌫ left %q", got)
	}
}

func TestMovingTheResumeCaretKeepsItsCursorAndEditingHomesIt(t *testing.T) {
	a := openPicker(twoRows(), false)
	a.resumePicker.Cursor = 1
	a = resumePressed(t, a, kmsg(tea.KeyLeft), kmsg(tea.KeyHome))
	if a.resumePicker.Cursor != 1 {
		t.Errorf("moving the caret moved the cursor to %d", a.resumePicker.Cursor)
	}
	a = resumePressed(t, a, runeKey("i"))
	if a.resumePicker.Cursor != 0 {
		t.Errorf("typing left the cursor on %d, want the top", a.resumePicker.Cursor)
	}
}

func TestTheResumeSearchDrawsItsCaretUnlessTheWindowIsBlurred(t *testing.T) {
	p := ResumePicker{Rows: twoRows(), Query: "ab", Back: 1}
	if v := stripANSI(p.View(100, 30)); !strings.Contains(v, "› a"+jumpCaret+"b") {
		t.Errorf("the search box does not draw its caret between a and b:\n%s", v)
	}
	p.windowBlurred = true
	if v := stripANSI(p.View(100, 30)); strings.Contains(v, jumpCaret) || !strings.Contains(v, "› ab") {
		t.Errorf("a blurred window draws a caret, or lost the query:\n%s", v)
	}
	if v := stripANSI((ResumePicker{Rows: twoRows()}).View(100, 30)); !strings.Contains(v, "› search…"+jumpCaret) {
		t.Errorf("an empty search draws no hint and caret:\n%s", v)
	}
}

func TestTheKeysWakeClaimsStaySwallowedInTheResumeSearch(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyCtrlW, tea.KeyCtrlA, tea.KeyCtrlE, tea.KeyCtrlV, tea.KeyCtrlT, tea.KeyCtrlD} {
		a := resumePressed(t, openPicker(twoRows(), false), runeKey("a"), runeKey("b"), kmsg(tea.KeyLeft))
		after := resumePressed(t, a, kmsg(k))
		if after.resumePicker.Query != "ab" || after.resumePicker.Back != 1 {
			t.Errorf("%s changed the search: %s", tea.Key{Type: k}.String(), showCaret(after.resumePicker.Query, after.resumePicker.Back))
		}
	}
}
