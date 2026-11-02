package ui

// The transcript behind a card is drawn exactly as it is drawn with no card.
//
// It used to be dimmed - every row stripped of its own colour and redrawn in
// HintStyle so the card would read as modal - and the owner asked for that to
// go (decisions.md, 2026-11-01): the card is framed already, and the rows above
// it are the conversation the answer is about. Byte-identical rather than the
// same text, because a muted re-render keeps the words and loses everything else.
//
// Under a colour profile, or there is nothing to lose: glamour draws a plain
// paragraph with no colour, so a tool call is appended to give the history a
// row lipgloss styles.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// paneRowsOf is the conversation id's pane as drawn, row by row, colour kept.
func paneRowsOf(a App, id string) []string {
	w, h, ok := a.paneSize(id)
	if !ok {
		return nil
	}
	return strings.Split(a.dmPane(id, w, h), "\n")
}

// aboveTheBox is a pane's rows above its first framed block - the card when one
// is up, the composer otherwise - which are the transcript rows it shows.
func aboveTheBox(rows []string) []string {
	for i, r := range rows {
		if strings.Contains(r, composerTopGlyph) {
			return rows[:i]
		}
	}
	return rows
}

// colouredHistory is a conversation of height rows with a history that ends on
// a lipgloss-styled row, drawn under a colour profile for the test's duration.
func colouredHistory(t *testing.T, height int) App {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor: a re-render has colour to lose
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	call := bashCall("t1")
	return beforeAsking(t, pageSetup{height: height, history: pageHistory}).applyFrame(rpcEvent("s1", &call))
}

// sameRowsSameBytes holds every row of got whose text is also a row of want to
// want's bytes for it. A text want draws two ways is not compared. It fails
// outright when no compared row carries colour, since then it has proved nothing
// about a re-render.
func sameRowsSameBytes(t *testing.T, got, want []string) {
	t.Helper()
	drawn, ambiguous := map[string]string{}, map[string]bool{}
	for _, r := range want {
		text := stripANSI(r)
		if prev, seen := drawn[text]; seen && prev != r {
			ambiguous[text] = true
		}
		drawn[text] = r
	}
	styled := 0
	for i, r := range got {
		text := stripANSI(r)
		w, ok := drawn[text]
		if !ok || ambiguous[text] {
			continue
		}
		if r != w {
			t.Errorf("row %d behind the card is not the row the pane draws without one:\n got %q\nwant %q", i, r, w)
		}
		if strings.Contains(w, "\x1b[") {
			styled++
		}
	}
	if styled == 0 {
		t.Fatal("no coloured row behind the card is one the pane drew without it: the comparison proves nothing")
	}
}

func TestTheTranscriptBehindACardIsDrawnAsItIsWithoutOne(t *testing.T) {
	before := colouredHistory(t, 40)
	asked := before.applyFrame(rpcEvent("s1", questionAsk(t)))
	if _, ok := asked.cardOf("s1"); !ok {
		t.Fatal("the conversation is putting no card, so this test is about nothing")
	}
	sameRowsSameBytes(t, aboveTheBox(paneRowsOf(asked, "s1")), aboveTheBox(paneRowsOf(before, "s1")))
}

// A pane shorter than its card, wheeled until the card and the composer have left
// by the bottom: the window is transcript alone, drawn by the page's own path.
func TestAPagedCardsTranscriptIsDrawnAsItIsWithoutOne(t *testing.T) {
	const height = 10
	before := colouredHistory(t, height)
	asked := before.applyFrame(rpcEvent("s1", questionAsk(t)))
	var page []string
	for range height { // the card's top is within height rows of the window's top at rest
		asked = wheeled(asked, tea.MouseButtonWheelUp, 1)
		if page = paneRowsOf(asked, "s1"); !hasRow(page, composerTopGlyph) {
			break
		}
	}
	if hasRow(page, composerTopGlyph) {
		t.Fatalf("the wheel never took the framed blocks off the window, so this is not the transcript-only page:\n%s", stripANSI(strings.Join(page, "\n")))
	}
	sameRowsSameBytes(t, page, aboveTheBox(paneRowsOf(before, "s1")))
}
