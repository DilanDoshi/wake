package ui

// The transcript behind a card is drawn exactly as it is drawn with no card.
//
// It used to be dimmed - every row stripped of its own colour and redrawn in
// HintStyle so the card would read as modal - and the owner asked for that to
// go (decisions.md, 2026-11-01): the card is framed already, and the rows above
// it are the conversation the answer is about. Byte-identical rather than the
// same text, because a muted re-render keeps the words and loses everything else.
//
// Under a colour profile, because otherwise there is nothing to lose. Glamour
// draws a plain paragraph with no colour, so the history opens and closes with
// a tool call: a row lipgloss styles at whichever end a window shows.

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

// colouredHistory is a conversation of height rows whose history opens and ends
// on a lipgloss-styled row, drawn under a colour profile for the test's duration.
func colouredHistory(t *testing.T, height int) App {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor: a re-render has colour to lose
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	call := bashCall("t1")
	return beforeAsking(t, pageSetup{height: height, history: pageHistory, styled: true}).applyFrame(rpcEvent("s1", &call))
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

// A pane shorter than its card, wheeled to the page's oldest line: the card and
// the composer are rows below the window, and what shows is transcript alone,
// drawn by the page's own path (DM.transcriptPage). The pane with no card is
// wheeled to its oldest line too, so the two windows show the same rows. The
// question arrives with the reader on the newest line: a page opened behind a
// reader already at the top has only its clipped rows to give the wheel.
func TestAPagedCardsTranscriptIsDrawnAsItIsWithoutOne(t *testing.T) {
	const height = 12 // room for the opening call under the banner, none for the card
	before := colouredHistory(t, height)
	asked := wheeled(before.applyFrame(rpcEvent("s1", questionAsk(t))), tea.MouseButtonWheelUp, pageHistory)
	before = wheeled(before, tea.MouseButtonWheelUp, pageHistory)
	page := paneRowsOf(asked, "s1")
	text := strings.Split(stripANSI(strings.Join(page, "\n")), "\n")
	if hasRow(text, composerTopGlyph) {
		t.Fatalf("a framed block is still on the window at the page's top, so this is not the transcript-only page:\n%s", strings.Join(text, "\n"))
	}
	plain := aboveTheBox(paneRowsOf(before, "s1"))
	plainText := strings.Split(stripANSI(strings.Join(plain, "\n")), "\n")
	if !hasRow(text, "1 tool use") || !hasRow(plainText, "1 tool use") { // the history's opening call, folded: both windows are at the top
		t.Fatalf("the two windows do not both show the history's opening tool call, so they need not share a row:\npage:\n%s\nno card:\n%s", strings.Join(text, "\n"), strings.Join(plainText, "\n"))
	}
	sameRowsSameBytes(t, page, plain)
}
