package ui

// What one line of the room has to be: who spoke, what they said, and - past
// the height the room shows inline - a pointer instead of the words.
//
// The boundary is a rendered-row count, so the tests measure the same render
// the code does (renderedRows) and assert the decision against roomInlineRows
// rather than against a hand-counted number glamour could invalidate. The one
// that matters most straddles the cap on purpose - the same reply above the
// line at a wide width and below it in a narrow column - because that
// width-adaptivity is the whole of what "variable sizing" bought.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

// roomWidth is an ordinary room pane: 120 columns with both sidebars open and
// a DM beside it leaves about this much.
const roomWidth = 60

func TestAShortReplyIsShownWholeAndNamesWhoSaidIt(t *testing.T) {
	b := roomBlock(
		core.Event{Kind: core.KindAssistantText, Text: "Fixed the retry header, tests pass"},
		Agent{ID: "s1", Name: "sydney", Label: "auth-fix"},
		roomWidth,
		false,
	)
	if !strings.Contains(b.text, "sydney") || !strings.Contains(b.text, "auth-fix") {
		t.Errorf("a room line does not say who spoke:\n%s", b.text)
	}
	if !strings.Contains(b.text, "Fixed the retry header") {
		t.Errorf("a 34-character reply was not shown whole:\n%s", b.text)
	}
	if strings.Contains(b.text, openDMHint) {
		t.Errorf("a short reply was turned into a pointer:\n%s", b.text)
	}
}

func TestAReplyTallerThanTheCapBecomesAPointerIntoTheDM(t *testing.T) {
	// Many short paragraphs: each is a rendered row with a blank between, so this
	// is unambiguously past any row cap without depending on wrap width.
	long := "token refresh.\n\n" + strings.Repeat("more on the token refresh.\n\n", roomInlineRows+5)
	b := roomBlock(
		core.Event{Kind: core.KindAssistantText, Text: long},
		Agent{ID: "s1", Name: "sydney", Label: "auth-fix"},
		roomWidth,
		false,
	)
	if !strings.Contains(b.text, openDMHint) {
		t.Errorf("a reply far taller than the %d-row cap was drawn as content. The boundary is a surface boundary, not a display nicety - the room is a hub and deep reading is DM work:\n%s", roomInlineRows, b.text)
	}
	if !strings.Contains(b.text, "token refresh") {
		t.Errorf("the pointer shows nothing of what was said, so there is no way to judge whether to open it:\n%s", b.text)
	}
	if lines := strings.Count(b.text, "\n") + 1; lines > 1+roomCollapseLines+1 {
		t.Errorf("the pointer is %d lines; a header, %d lines of preview and the key is the whole of it", lines, roomCollapseLines)
	}
}

// A folded room reply shows the response's real output-token count - the number
// Claude reported for that message - rather than a word count.
func TestAFoldedReplyShowsItsTokenCount(t *testing.T) {
	long := "token refresh.\n\n" + strings.Repeat("more on the token refresh.\n\n", roomInlineRows+5)
	b := roomBlock(
		core.Event{Kind: core.KindAssistantText, Text: long, OutputTokens: 210},
		Agent{ID: "s1", Name: "sydney", Label: "auth-fix"},
		roomWidth,
		false,
	)
	if !strings.Contains(b.text, "210 tokens") {
		t.Errorf("the folded reply did not show its token count:\n%s", b.text)
	}
	if strings.Contains(b.text, "words") {
		t.Errorf("the folded reply still counts words:\n%s", b.text)
	}
}

// A reply that arrived without a usage count drops the figure rather than
// showing a zero it cannot stand by - the open-DM hint is all that remains.
func TestAFoldedReplyWithNoTokenCountShowsNoFigure(t *testing.T) {
	long := "token refresh.\n\n" + strings.Repeat("more on the token refresh.\n\n", roomInlineRows+5)
	b := roomBlock(
		core.Event{Kind: core.KindAssistantText, Text: long, OutputTokens: 0},
		Agent{ID: "s1", Name: "sydney"},
		roomWidth,
		false,
	)
	if !strings.Contains(b.text, openDMHint) {
		t.Fatalf("precondition: the reply should still collapse to a pointer:\n%s", b.text)
	}
	if strings.Contains(b.text, "tokens") || strings.Contains(b.text, "words") {
		t.Errorf("a reply with no usage showed a length figure it cannot back up:\n%s", b.text)
	}
}

// The decision follows the rendered height, across shapes and widths. Each body
// is rendered by the test itself, so the assertion is not a guess about
// glamour's row math but the invariant the code implements: collapse iff the
// render is taller than roomInlineRows. It catches measuring the source instead
// of the render, or the wrong width being measured - but not the boundary
// operator, since none of these fixtures land on the cap; that is
// TestTheDecisionFlipsAtExactlyTheRowCap's job.
func TestAReplyCollapsesExactlyWhenItRendersTallerThanTheCap(t *testing.T) {
	bodies := []string{
		"one short line",
		strings.Repeat("a paragraph of a few words.\n\n", 5),
		strings.Repeat("a paragraph of a few words.\n\n", 20),
		"## Heading\n\n" + strings.Repeat("- a bullet in a list\n", 30),
		"```\n" + strings.Repeat("code line\n", 40) + "```",
	}
	for _, body := range bodies {
		for _, w := range []int{roomWidth, 100} {
			want := renderedRows(render.Markdown(strings.TrimSpace(body), w)) > roomInlineRows
			b := roomBlock(core.Event{Kind: core.KindAssistantText, Text: body}, Agent{Name: "sydney"}, w, false)
			if got := strings.Contains(b.text, openDMHint); got != want {
				t.Errorf("width %d: body rendering %d rows (cap %d) collapsed=%v, want %v:\n%s",
					w, renderedRows(render.Markdown(strings.TrimSpace(body), w)), roomInlineRows, got, want, b.text)
			}
		}
	}
}

// The flip is at exactly the cap, not one row either side. A reply that renders
// to the cap height is still content; one row taller is a pointer. This is what
// catches a <= turned into a < (or a stray offset in renderedRows) - the
// invariant test above never lands a fixture near the boundary, so on its own it
// cannot see that mutation.
func TestTheDecisionFlipsAtExactlyTheRowCap(t *testing.T) {
	const w = 60
	atCap := codeBlockRenderingTo(t, roomInlineRows, w)
	pastCap := codeBlockRenderingTo(t, roomInlineRows+1, w)

	at := roomBlock(core.Event{Kind: core.KindAssistantText, Text: atCap}, Agent{Name: "sydney"}, w, false)
	if strings.Contains(at.text, openDMHint) {
		t.Errorf("a reply rendering exactly %d rows (the cap) became a pointer: a reply the height of the cap is still content:\n%s", roomInlineRows, at.text)
	}
	past := roomBlock(core.Event{Kind: core.KindAssistantText, Text: pastCap}, Agent{Name: "sydney"}, w, false)
	if !strings.Contains(past.text, openDMHint) {
		t.Errorf("a reply rendering %d rows (one past the cap) was drawn whole: one row past the cap is past it:\n%s", roomInlineRows+1, past.text)
	}
}

// codeBlockRenderingTo returns a fenced code block whose rendered height at
// width w is exactly rows. A code block preserves its lines one-for-one, so the
// exact target is always reachable; failing loudly when it is not is what keeps
// the boundary test from silently going vacuous if glamour's row math shifts.
func codeBlockRenderingTo(t *testing.T, rows, w int) string {
	t.Helper()
	for n := 1; n <= rows+8; n++ {
		body := "```\n" + strings.Repeat("x\n", n) + "```"
		if renderedRows(render.Markdown(body, w)) == rows {
			return body
		}
	}
	t.Fatalf("no code block of up to %d lines renders to exactly %d rows at width %d", rows+8, rows, w)
	return ""
}

// The headline: one reply, two widths, two answers. A realistic plan is under
// the cap when the room has the full terminal and over it in a narrow column
// beside a DM - which is the whole of the "variable sizing" this change is. The
// test first asserts the fixture genuinely straddles the cap, so it cannot pass
// by both widths landing on the same side.
func TestTheSameReplyShowsAtWideWidthAndCollapsesInANarrowColumn(t *testing.T) {
	const wide, narrow = 150, 40
	plan := "## Plan\n\n" +
		strings.Repeat("- a step in the plan that says what to do next\n", 12) +
		"\nThis closing paragraph explains the approach and the tradeoffs involved in it, at some length.\n"

	wideRows := renderedRows(render.Markdown(plan, wide))
	narrowRows := renderedRows(render.Markdown(plan, narrow))
	straddles := wideRows <= roomInlineRows && narrowRows > roomInlineRows
	if !straddles {
		t.Fatalf("fixture no longer straddles the %d-row cap: %d rows at %d cols, %d rows at %d cols. Either the plan needs adjusting or roomInlineRows moved out of the band this fixture crosses - restore the straddle so the test still exercises width-adaptivity",
			roomInlineRows, wideRows, wide, narrowRows, narrow)
	}

	w := roomBlock(core.Event{Kind: core.KindAssistantText, Text: plan}, Agent{Name: "sydney"}, wide, false)
	if strings.Contains(w.text, openDMHint) {
		t.Errorf("a plan that renders %d rows at %d cols (cap %d) was collapsed, so the room hid what it had room to show:\n%s", wideRows, wide, roomInlineRows, w.text)
	}
	n := roomBlock(core.Event{Kind: core.KindAssistantText, Text: plan}, Agent{Name: "sydney"}, narrow, false)
	if !strings.Contains(n.text, openDMHint) {
		t.Errorf("the same plan renders %d rows at %d cols (cap %d) and was still drawn whole, so a narrow column got a wall of text:\n%s", narrowRows, narrow, roomInlineRows, n.text)
	}
}

// A short reply in a double-width script is shown whole, and the reason is now
// structural: the boundary is rows, and a couple hundred CJK glyphs are a
// handful of rows however many bytes they weigh. This is what retired the old
// byte-versus-character threshold rather than guarding it.
func TestAShortReplyInADoubleWidthScriptIsShownWhole(t *testing.T) {
	b := roomBlock(
		core.Event{Kind: core.KindAssistantText, Text: strings.Repeat("あ", 200)},
		Agent{Name: "kenji"}, roomWidth, false,
	)
	if strings.Contains(b.text, openDMHint) {
		t.Errorf("a short CJK reply collapsed:\n%s", b.text)
	}
}

// A pointer is worth opening or it is not, and the reader decides that from
// what it shows. The opening lines are what a reader would have seen if it had
// been short; the closing ones are what they would have read last, which
// answers nothing about whether to go and read the rest.
func TestThePointerPreviewsTheOpeningOfTheReplyAndNotItsEnd(t *testing.T) {
	// Paragraphs, so it renders past the cap regardless of wrap width, and the
	// opening and closing are their own rows.
	long := "OPENING three approaches to the token refresh.\n\n" +
		strings.Repeat("and then some middle reasoning.\n\n", roomInlineRows+5) + "CLOSING recommendation."
	b := roomBlock(
		core.Event{Kind: core.KindAssistantText, Text: long},
		Agent{Name: "sydney", Label: "auth-fix"}, roomWidth, false,
	)
	if !strings.Contains(b.text, "OPENING") {
		t.Errorf("the pointer does not show the opening of the reply:\n%s", b.text)
	}
	if strings.Contains(b.text, "CLOSING") {
		t.Errorf("the pointer previews the end of the reply. A reader judges whether to open it from what they would have seen first, not from what they would have read last:\n%s", b.text)
	}
}

func TestATurnThatSaidNothingDrawsAQuietMarkerAndNotAClaimOfCompletion(t *testing.T) {
	b := roomBlock(core.Event{Kind: core.KindTurnEnd}, Agent{Name: "john"}, roomWidth, false)
	if !strings.Contains(b.text, finishedMarker) {
		t.Errorf("a silent turn drew nothing at all:\n%s", b.text)
	}
	for _, claim := range []string{"finished working", "complete", "done"} {
		if strings.Contains(strings.ToLower(b.text), claim) {
			t.Errorf("the marker asserts %q. A turn ending does not mean the work stopped - an async subagent streams past its own result, and 3 of 46 recorded results are turns nobody asked for - so Wake may relay a claim and may not make one:\n%s", claim, b.text)
		}
	}
}

// roomSep is the separator between an agent and what it is working on, and it
// means that and nothing else. Gluing a marker on with the same separator
// reads as a third half of the agent's name - `sydney <> auth-fix <> ⤷ turn
// ended` - so the count is taken from speaker's own output rather than from a
// number here.
func TestTheQuietMarkerIsNotDrawnAsAThirdHalfOfTheAgentsName(t *testing.T) {
	a := Agent{Name: "sydney", Label: "auth-fix"}
	b := roomBlock(core.Event{Kind: core.KindTurnEnd}, a, roomWidth, false)

	want := strings.Count(speaker(a), roomSep)
	if got := strings.Count(b.text, roomSep); got != want {
		t.Errorf("the marker line spells %q %d times and the speaker spells it %d: it separates a name from a branch, and a marker is neither:\n%s", roomSep, got, want, b.text)
	}
	if !strings.Contains(b.text, "auth-fix") {
		t.Errorf("the marker does not say what the agent was working on:\n%s", b.text)
	}
}

// Your own room turn lines up with the agents' replies. It sits at bodyIndent -
// render.Markdown's two-column margin - so the shaded turn reads as part of the
// same column rather than a block shoved against the edge. The room shades
// without a label, so the indent is all that sets your words off the pane edge.
func TestYourOwnRoomTurnAlignsWithAnAgentsReply(t *testing.T) {
	forceColour(t)
	const w = roomWidth
	const body = "check the retry header on a 401 refresh path and then report what you find"

	// Where an agent's prose starts in the room, off the real agentSaid render.
	reply := agentSaid(body, 0, Agent{Name: "sydney"}, w, false)
	want := indentOf(stripANSI(reply), "check")
	if want != bodyIndent {
		t.Fatalf("precondition: an agent's room reply starts at column %d, not bodyIndent (%d)", want, bodyIndent)
	}

	out := youSaid("@sydney "+body, w)
	if got := indentOf(stripANSI(out), "check"); got != want {
		t.Errorf("your own room turn starts at column %d, an agent's reply at %d - they do not line up:\n%s", got, want, out)
	}

	// Solid shaded rectangle to end of line, wrapped lines included.
	bg := background(t)
	for i, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, bg) {
			t.Errorf("room turn line %d lost its shaded ground:\n%q", i, line)
		}
		if cells := lipgloss.Width(line); cells != w {
			t.Errorf("room turn line %d is %d cells against width %d - the ground is ragged, not a rectangle", i, cells, w)
		}
	}

	// The leading @mention keeps its colour at the indented position. It rides a
	// compound SGR beside Own's ground, so assert the mention colour parameter in
	// the escape directly before the handle rather than a bare escape.
	param := mentionParam(t)
	idx := strings.Index(out, "@sydney")
	if idx < 0 {
		t.Fatalf("the @handle vanished from your own turn:\n%q", out)
	}
	lead := out[:idx]
	esc := lead[strings.LastIndex(lead, "\x1b["):]
	if !strings.Contains(esc, param) {
		t.Errorf("the leading @mention lost its colour: the escape before it is %q, missing the mention parameter %q", esc, param)
	}
}

// mentionParam is the SGR parameter lipgloss emits for the mention colour at the
// forced profile - "94" for bright blue, say - derived rather than hard-coded so
// the assertion is about whether the colour is applied, not how it is spelled.
func mentionParam(t *testing.T) string {
	t.Helper()
	esc, _, ok := strings.Cut(lipgloss.NewStyle().Foreground(Mention).Render("x"), "x")
	if !ok || esc == "" {
		t.Fatalf("lipgloss emitted no escape for the mention colour at this profile")
	}
	return strings.TrimSuffix(strings.TrimPrefix(esc, "\x1b["), "m")
}

func TestYourOwnMessageIsDrawnAsYours(t *testing.T) {
	b := roomBlock(core.Event{Kind: core.KindUserText, Text: "who is stuck?"}, Agent{}, roomWidth, false)
	if !strings.Contains(b.text, "who is stuck?") {
		t.Errorf("your own message is not in your own conversation:\n%s", b.text)
	}
}

// Your own turn **wraps**, and nothing you typed is cut out of it.
//
// This used to cost exactly one row: a draft was flattened and truncated with
// an ellipsis, on the argument that it is an instruction you have already read
// and that a pasted broadcast would push the answers it asked for off the pane.
// The owner overruled it against the case that actually happens - the room is a
// narrow column beside a conversation, so an ordinary sentence reached the cut
// and what was lost was the half naming what was asked for.
//
// The cost that argument names is real and is now paid: a twenty-line paste is
// twenty rows of the room. What it buys is that `@john can you make a…` is no
// longer the whole record of what you said.
//
// The DM has always wrapped this, so the two surfaces used to disagree about
// one message; TestBothSurfacesShowTheWholeMessageYouTyped holds them together.
func TestYourOwnMessageWrapsRatherThanBeingCut(t *testing.T) {
	for name, text := range map[string]string{
		"a draft typed across three lines": "look at the retry header\nand the backoff\nand the jitter",
		"a broadcast pasted from a file":   strings.Repeat("check the retry header and the backoff too. ", 20),
		"one long sentence":                "can you make a hil thing asking my favorite colour and then wire it up",
	} {
		t.Run(name, func(t *testing.T) {
			b := roomBlock(core.Event{Kind: core.KindUserText, Text: text}, Agent{}, roomWidth, false)
			if strings.Contains(b.text, ellipsis) {
				t.Errorf("the room cut your own message with %q:\n%s", ellipsis, b.text)
			}
			if got := strings.Count(b.text, "\n") + 1; got < 2 {
				t.Errorf("%s drew %d row(s), so it did not wrap:\n%s", name, got, b.text)
			}
			// Every word survives, which is the whole point: a cut lost the end
			// of the sentence, which is where the ask is.
			for _, word := range strings.Fields(collapseWhitespaceOneLine(text)) {
				if !strings.Contains(b.text, word) {
					t.Fatalf("%s lost the word %q:\n%s", name, word, b.text)
				}
			}
		})
	}
}

// The room and the conversation show the same message.
//
// They disagreed for as long as the room cut and the DM wrapped, and the
// disagreement was invisible from either one on its own - you would have had to
// open the pane to find out that the room had been lying about what you asked.
func TestBothSurfacesShowTheWholeMessageYouTyped(t *testing.T) {
	const text = "can you make a hil thing asking my favorite colour and then wire it up to the form"

	room := roomBlock(core.Event{Kind: core.KindUserText, Text: text}, Agent{}, roomWidth, false)
	dm := userBlock(core.Event{Kind: core.KindUserText, Text: text}, roomWidth)

	for _, word := range strings.Fields(text) {
		if !strings.Contains(room.text, word) {
			t.Errorf("the room is missing %q from a message the conversation shows whole", word)
		}
		if !strings.Contains(dm, word) {
			t.Errorf("the conversation is missing %q", word)
		}
	}
}

func TestNoRoomBlockIsEverWiderThanThePaneItWasAskedFor(t *testing.T) {
	events := []core.Event{
		{Kind: core.KindAssistantText, Text: strings.Repeat("supercalifragilistic ", 30)},
		{Kind: core.KindAssistantText, Text: strings.Repeat("あ", 300)},
		{Kind: core.KindTurnEnd},
		{Kind: core.KindUserText, Text: "hi"},
	}
	for _, w := range []int{minBlockWidth, 40, 80, 120} {
		for _, ev := range events {
			b := roomBlock(ev, Agent{Name: "sydney", Label: "a-very-long-branch-name-indeed"}, w, false)
			assertFitsWidth(t, b.text, w)
		}
	}
}

// assertFitsWidth checks every line rather than the widest one. A block is
// joined into a column beside two sidebars and lipgloss sizes a joined row on
// its widest line, so one over-wide line shoves its neighbours out of place.
func assertFitsWidth(t *testing.T, text string, w int) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if got := ansi.StringWidth(line); got > w {
			t.Errorf("width %d: a line measured %d. The room is one column of a three-region layout, and one over-wide line shoves its neighbours out of place:\n%q", w, got, line)
		}
	}
}

// notDrawnByRoomBlock is every kind the fold admits to the room that this file
// deliberately draws nothing for, with the reason and the owner.
//
// Hand-written because it is an excuse table, so it carries the obligation
// decisions.md puts on one: it is checked in both directions below. An
// admitted kind missing from here is a line the room silently loses, and an
// excuse for a kind that is no longer admitted - or that roomBlock has since
// learned to draw - is dead text, which is what makes deleting one a
// three-place edit instead of a one-line one.
var notDrawnByRoomBlock = map[core.EventKind]string{
	core.KindRequestWithdrawn:  "the retirement of that card, and there is nothing to retire until the card exists. cards.go owns it",
}

// Everything Fleet.Observe hands the room has to arrive somewhere a reader can
// see, or be excused here on purpose.
//
// The set is derived twice over rather than restated: roomCases is checked
// against core/event.go's own constants by
// TestEveryEventKindTheAirlockCanProduceHasARoomDecision, and this reads that
// same table for what the fold actually admits. A kind that reaches the room
// and renders to nothing is a line that vanishes between two files, each of
// which looks correct on its own.
func TestEveryKindTheRoomAdmitsIsDrawnHereOrExcusedWithAReason(t *testing.T) {
	drawn := map[core.EventKind]bool{}
	for _, c := range roomCases() {
		if !c.want {
			continue
		}
		if roomBlock(c.ev, Agent{Name: "sydney", Label: "auth-fix"}, roomWidth, false).text != "" {
			drawn[c.ev.Kind] = true
			continue
		}
		if _, excused := notDrawnByRoomBlock[c.ev.Kind]; !excused {
			t.Errorf("the fold hands the room a %q (%s) and roomBlock draws nothing for it: the line is lost between two files that each look correct alone. Draw it, or add it to notDrawnByRoomBlock with the reason", c.ev.Kind, c.name)
		}
	}

	admitted := map[core.EventKind]bool{}
	for _, c := range roomCases() {
		admitted[c.ev.Kind] = admitted[c.ev.Kind] || c.want
	}
	for kind := range notDrawnByRoomBlock {
		if !admitted[kind] {
			t.Errorf("notDrawnByRoomBlock excuses %q, which the fold no longer admits to the room: a dead excuse is what makes deleting a kind a three-place edit", kind)
		}
		if drawn[kind] {
			t.Errorf("notDrawnByRoomBlock excuses %q and roomBlock draws it: the excuse outlived what it was excusing", kind)
		}
	}
}
