package ui

// Settling a card takes two presses of the same key, and this file is why.
//
// A settled card cannot be unsettled. An allow runs the tool; a deny reaches
// the model verbatim and it has then been told. Neither has a retraction on
// this wire, so the only side of the socket on which the operator can still
// change their mind is the one before the frame is written - and the only way
// to create a moment there is to make the keystroke take two steps.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// press delivers one rune the way Bubble Tea does.
func press(a App, r rune) (App, tea.Cmd) {
	return pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// settleKey presses the two keys a settle takes: the card's own rune to arm it,
// then ↵ to confirm. Every test that wants the frame goes through here, so the
// pair is stated once.
//
// ↵ and not the rune again. The rune is what the accident produces - a
// character that vanished is retyped, and `add` is three card keys in a row -
// so a confirm on the same rune is defeated by the very thing the arm exists to
// catch.
func settleKey(a App, r rune) (App, tea.Cmd) {
	a, _ = press(a, r)
	return pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
}

// blockedPane is one agent stopped on a permission ask with its conversation
// open and holding the keys, which is the only place a card is drawn: the room
// draws none. See App.cardOf.
func blockedPane(t *testing.T) App {
	t.Helper()
	return newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateBlocked},
	).openDMWith("s2", "sydney").opened(t).applyFrame(rpc.Frame{
		Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
			Kind: core.KindPermissionRequest, RequestID: "r1",
			Tool: &core.ToolCall{Name: "Bash", Display: "rm -rf build/"},
		},
	})
}

// opened lets go of what opening a conversation queued.
//
// The transcript ask rides out on the *next* Update - App.Update's one drain
// seam - so a fixture that opens a conversation and then presses a key gets
// that ask back as the keystroke's own command, and `cmd != nil` would stop
// meaning "this keystroke wrote something". One benign Update instead: a
// window size that has not changed applies the geometry and writes nothing.
func (a App) opened(t *testing.T) App {
	t.Helper()
	m, _ := a.Update(tea.WindowSizeMsg{Width: a.layout.Width, Height: a.layout.Height})
	recorderOf(t, a).taken(t)
	return m.(App)
}

// asked is a second ask, on a second agent.
func askOn(sessionID, requestID string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: sessionID, Event: &core.Event{
		Kind: core.KindPermissionRequest, RequestID: requestID,
		Tool: &core.ToolCall{Name: "Write", Display: "notes.md"},
	}}
}

// reporting is a fleet report naming exactly these (agent, ask) pairs as
// blocked. It is what Cards.Reconcile squares the open list against.
func reporting(pairs ...[2]string) rpc.Frame {
	st := rpc.Status{Running: true}
	for _, p := range pairs {
		st.Sessions = append(st.Sessions, rpc.SessionStatus{
			ID: p[0], RequestIDs: []string{p[1]}, State: rpc.StateBlocked, Tool: "Bash", ToolArg: "rm -rf build/",
		})
	}
	return rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st}
}

// The first character of every draft is typed into an empty composer, so the
// empty-composer gate defends the second character onward and leaves the first
// one - which is the whole exposure. `a` and `d` begin a great many things an
// operator types at a room full of agents ("add the tests", "do it in the other
// directory"), and one keystroke used to be a tool call granted or a refusal the
// model was told about.
func TestOnePressOfASettleKeyArmsItRatherThanSendingIt(t *testing.T) {
	a := blockedPane(t)

	a2, cmd := press(a, cardDenyKey)

	if cmd != nil {
		t.Error("one press of the deny key wrote something. The frame is the thing that cannot be taken back, so it may not leave on the keystroke that could have been the first letter of a draft")
	}
	if a2.cards.Len() != 1 {
		t.Fatal("one press of the deny key settled the card")
	}
	if got := a2.composer().Value(); got != "" {
		t.Errorf("the draft is %q: an armed settle consumes its rune, so the card can say what a second press would do", got)
	}
}

// The other half, and it has to be in the same file: a settle that took two
// presses and never arrived would be worse than one that took one.
func TestConfirmingAnArmedSettleWritesTheFrame(t *testing.T) {
	a, cmd := settleKey(blockedPane(t), cardDenyKey)

	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameDeny || f.RequestID != "r1" || f.SessionID != "s2" {
		t.Errorf("confirming the deny sent %+v, want a deny for r1 on s2", f)
	}
	if a.cards.Len() != 0 {
		t.Error("the card stayed up after being settled")
	}
}

// The confirm may not be the rune that armed it.
//
// The universal recovery from "the character I typed did not appear" is to type
// it again. A same-rune confirm turns that recovery into the irreversible
// frame - no mouse, no delay, no second actor - so the arm would be defeated by
// the exact accident it exists to catch.
func TestRetypingTheArmingRuneDoesNotConfirmTheSettle(t *testing.T) {
	for _, key := range []rune{cardAllowKey, cardDenyKey} {
		a := blockedPane(t)
		a, _ = press(a, key)
		a, cmd := press(a, key)

		if cmd != nil {
			t.Errorf("[%c] pressed twice settled the card: a vanished character is retyped, and that retype may not be the thing that cannot be taken back", key)
		}
		if a.cards.Len() != 1 {
			t.Fatalf("[%c] pressed twice took the card down", key)
		}
	}
}

// And the word that makes it worse than a retype, because it needs no
// confusion at all: `add` is three card keys in a row. Under a same-rune
// confirm it denies. `a`, `d`, `d` is the whole of the failure.
func TestTypingAWordMadeOfCardKeysSettlesNothing(t *testing.T) {
	a := blockedPane(t)

	var cmds []tea.Cmd
	for _, r := range "add the tests" {
		var cmd tea.Cmd
		a, cmd = press(a, r)
		cmds = append(cmds, cmd)
	}

	if a.cards.Len() != 1 {
		t.Fatal("typing `add the tests` at a room with a card up settled it")
	}
	for i, cmd := range cmds {
		if cmd != nil && i < 3 {
			t.Errorf("keystroke %d of `add the tests` wrote something", i)
		}
	}
	if got := a.composer().Value(); !strings.HasSuffix(got, " the tests") {
		t.Errorf("the draft is %q: everything after the card keys has to reach it unchanged", got)
	}
}

// A card whose keys are not on screen is not answerable.
//
// roomPane draws no card at all below the room's own floor, and clips the card
// to what is left above it - and the key line is the last row, so it is the
// first thing cut. An arm the operator cannot see, on a card they cannot see,
// is a confirmation that is not one: the only account of what the first press
// did is the row that was cut.
//
// So the rune is handed back instead, which is what it would be if there were
// no card - and there is, as far as this pane is concerned, no card.
func TestACardWhoseKeysAreNotOnScreenIsNotAnswerable(t *testing.T) {
	a := blockedPane(t)
	if !a.cardFullyDrawn() {
		t.Fatal("the card is not drawn at the size this test starts from, so it proves nothing")
	}
	a = a.withSize(200, a.room.minHeight()+noticeHeight)
	if a.cardFullyDrawn() {
		t.Fatalf("the card still fits at pane height %d, so this test asserts nothing", a.paneHeight())
	}

	top, _ := a.cards.Top()
	a, _ = press(a, cardAllowKey)

	if _, armed := a.cards.armedKey(top); armed {
		t.Error("a card nobody can see was armed: the only account of an arm is the row roomPane cut")
	}
	if got := a.composer().Value(); got != string(cardAllowKey) {
		t.Errorf("the draft is %q: a card that is not drawn holds no keys, so the rune is the character somebody typed", got)
	}

	// And ↵ behind it is the composer's own key again, not a confirm of
	// something that was never armed.
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if a.cards.Len() != 1 {
		t.Error("↵ settled a card that had not been armed")
	}
}

// The accident this whole mechanism is for: somebody typing "do the thing"
// presses `d` and then `o`. The `o` goes to the draft, which is what it always
// was - and it has to take the denial back on its way past, or an arm sits
// live behind that draft for as long as it lasts.
//
// Asserted on the card, because a rune that reached the draft makes the
// composer non-empty and no card key is read at all after that: the card's own
// account of itself is the only place the arm is still visible.
func TestARuneThatReachesTheDraftTakesBackAnArmedSettle(t *testing.T) {
	a := blockedPane(t)

	// The allow key, because the refusal now opens a box for its reason rather
	// than arming - see cardanswer.go. The mechanism under test is the arm,
	// and on a permission card the allow key is what reaches it.
	a, _ = press(a, cardAllowKey)
	if !strings.Contains(shown(a), cardArmedCancel) {
		t.Fatalf("the allow key did not arm anything, so this test asserts nothing:\n%s", shown(a))
	}

	a, _ = press(a, 'o')

	if strings.Contains(shown(a), cardArmedCancel) {
		t.Error("the settle is still armed after a keystroke that went to the draft: the operator was typing a message, not answering an agent")
	}
	if got := a.composer().Value(); got != "o" {
		t.Errorf("the draft is %q rather than %q: the rune that took the arm back is still the character somebody typed", got, "o")
	}
}

// And the slip an operator actually makes: `d` on a question when they meant
// to choose. The refusal opens a box for its reason now rather than arming, so
// the old disarm-on-pick cannot be what saves them - the digit is a character
// of the reason, and what saves them is that it is *visible* as one and that ⎋
// takes the whole thing back.
//
// The property is the same as it ever was: a slip does not settle a card. Only
// the mechanism moved, from an arm nothing drew to a box that says what it is.
func TestASlipIntoTheReasonBoxIsVisibleAndRecoverable(t *testing.T) {
	a := paneAsking(t)

	a, _ = press(a, cardDenyKey)
	a, _ = press(a, cardFirstOption)

	// The digit went into the reason, where it can be seen, rather than
	// quietly choosing an option behind an open refusal.
	if got := a.composer().Value(); got != string(cardFirstOption) {
		t.Errorf("the digit landed as %q rather than in the reason box: it reached the options while a refusal was open, which is the ↵ that denies a question somebody was answering", got)
	}
	if card := topCard(t, a); card.chosen(card.Cursor) != noChoice {
		t.Errorf("the digit chose option %d while the reason box was open", card.chosen(card.Cursor))
	}
	// And the card says which mode is live, so the next ↵ is not a surprise.
	if !strings.Contains(shown(a), denyModeKeys) {
		t.Errorf("the card does not say a refusal is being written:\n%s", shown(a))
	}
	// And it names its own way out. The armed line's rule, one mode over: the
	// only visible exit may not read as pressing the key again.
	for _, want := range []string{cardConfirmGlyph, escGlyph, cardArmedFinal} {
		if !strings.Contains(denyModeKeys, want) {
			t.Errorf("the reason box's key line %q does not name %q", denyModeKeys, want)
		}
	}

	// ⎋ is the way out, and it leaves the question answerable.
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if a.cards.Len() != 1 {
		t.Fatal("⎋ out of the reason box settled the card")
	}
	if got := a.room.Composer().Value(); got != "" {
		t.Errorf("⎋ left %q in the draft", got)
	}
	a, _ = press(a, cardFirstOption)
	if card, _ := a.cards.Top(); card.chosen(0) != 0 {
		t.Errorf("after ⎋ the digit still does not choose an option: chosen=%d", card.chosen(0))
	}
}

// A settled card's arm has to go with it, and that is not covered by the arm
// being matched against the ask: Reconcile puts an ask back whenever the next
// report still names it, which it does until the daemon has acted on the frame
// just written. A surviving arm on the returning card settles it twice.
func TestSettlingACardTakesItsArmWithIt(t *testing.T) {
	a := blockedPane(t)

	a, cmd := settleKey(a, cardDenyKey)
	if f := sentFrame(t, a, cmd); f.Kind != rpc.FrameDeny {
		t.Fatalf("the settle sent %+v, so this test is not starting from a settled card", f)
	}

	// The daemon has not acted yet, so the next report still names the ask.
	a = a.applyFrame(reporting([2]string{"s2", "r1"}))
	if a.cards.Len() != 1 {
		t.Fatal("the report did not put the ask back, so this test asserts nothing")
	}

	a, again := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if again != nil {
		t.Error("a ↵ settled the returning card a second time: the arm outlived the settle it belonged to")
	}
	if a.cards.Len() != 1 {
		t.Fatal("the returning card was settled by an arm nobody re-armed")
	}
}

// And the keys that are not runes at all, which is the half the card's own
// switch cannot see: they go nowhere near cardKey, so without a disarm above it
// an arm survives every other thing the operator does and is confirmed by the
// next ↵ they press.
//
// Three classes, because they take three different paths out of App.key and a
// disarm on one is invisible to a test that only walks another: a chord the
// switch claims, an editing key it does not, and a mouse event that never
// reaches App.key at all. ⌫ is the likeliest of the three after a character
// has vanished, and the mouse is the likeliest of all - the operator reaches
// for it to scroll the room and read the card they are being asked about.
func TestAnythingThatIsNotTheConfirmTakesBackAnArmedSettle(t *testing.T) {
	for what, interrupt := range map[string]tea.Msg{
		"a chord App.key's own switch claims": tea.KeyMsg{Type: tea.KeyCtrlG},
		"an editing key it does not":          tea.KeyMsg{Type: tea.KeyBackspace},
		"a mouse wheel":                       tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp},
		"a click":                             tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
	} {
		a := blockedPane(t)
		a, _ = press(a, cardAllowKey)

		m, _ := a.Update(interrupt)
		a = m.(App)

		a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil {
			t.Errorf("%s did not take the settle back: the ↵ after it confirmed a settle armed before it", what)
		}
		if a.cards.Len() != 1 {
			t.Fatalf("%s left an arm that the next ↵ settled", what)
		}
	}
}

// The arm names one ask on one agent, and a confirm that could land on
// whichever card happens to be on top would be the accident it exists to
// prevent, arriving through the fix for it. Reachable without any keystroke:
// the top card is taken down by a fleet report that no longer names it, which
// is Reconcile's own job, and the ask behind it comes forward.
//
// Both halves of the pair are walked, because an ask id is unique per agent and
// nothing promises it is unique across them - which is why Reconcile matches on
// the pair too. A confirm that read either half alone would be answerable by a
// card the operator never armed.
func TestAConfirmOnlySettlesTheAskItWasArmedFor(t *testing.T) {
	for what, next := range map[string][2]string{
		"another agent, another ask":            {"s3", "r2"},
		"another agent holding the same ask id": {"s3", "r1"},
		"the same agent, another ask":           {"s2", "r2"},
	} {
		a := blockedPane(t).applyFrame(askOn(next[0], next[1]))
		if top, _ := a.cards.Top(); top.AgentID != "s2" || top.RequestID != "r1" {
			t.Fatalf("%s: the top card is %s/%s rather than the one this test arms", what, top.AgentID, top.RequestID)
		}

		a, _ = press(a, cardDenyKey)
		a = a.applyFrame(reporting(next))
		if top, ok := a.cards.Top(); !ok || top.AgentID != next[0] || top.RequestID != next[1] {
			t.Fatalf("%s: the report did not bring the second ask to the top, so this case asserts nothing: %+v", what, a.cards)
		}

		a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil {
			t.Errorf("%s: a settle armed on one ask was confirmed against another. An arm is a promise about one card, and a confirm that reads less than the whole of it denies whichever agent came forward", what)
		}
		if a.cards.Len() != 1 {
			t.Fatalf("%s: the second ask was settled by a ↵ armed against the first", what)
		}
	}
}

// The other direction, and it is what makes the confirm usable at all: reports
// arrive whenever any of thirty agents changes state, so an arm dropped by an
// unrelated one would be a confirm the operator could not reliably reach.
func TestAnArmedSettleSurvivesAFleetReportThatKeepsTheAsk(t *testing.T) {
	a := blockedPane(t)

	// The first press has to be the arm rather than the settle, or the report
	// puts the card back and the second press settles a card the arm had
	// nothing to do with - which passes this test in a build that drops the arm
	// on every report.
	a, armed := press(a, cardDenyKey)
	if armed != nil {
		t.Fatal("the first press wrote something, so this test cannot tell a surviving arm from a card the report put back")
	}
	a = a.applyFrame(reporting([2]string{"s2", "r1"}))
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameDeny || f.RequestID != "r1" {
		t.Errorf("the settle sent %+v: a report that kept this ask should have kept the arm with it", f)
	}
}

// The gate comes before the arm. A question short one choice is refused beneath
// the daemon, so its allow key is not offered - and an arm would swallow the
// rune while advertising nothing, which is a key that appears to do something
// and does not.
func TestAnUnansweredQuestionsAllowKeyIsHandedBackRatherThanArmed(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: questionAsk(t)})
	if top, _ := a.cards.Top(); top.Answered() {
		t.Fatal("the recorded question arrives already answered, so this test proves nothing")
	}

	a, _ = press(a, cardAllowKey)

	if got := a.composer().Value(); got != string(cardAllowKey) {
		t.Errorf("the draft is %q: a rune the card declines is the first character of what somebody is about to type, and arming it would eat it", got)
	}
}

// The card is its own legend, so an affordance that changed has to say so on
// the card. Before the first press it advertises the settle; after it, what a
// second press would do and what takes it back.
func TestTheCardNamesTheKeyThatConfirmsAnArmedSettleAndSaysWhatTakesItBack(t *testing.T) {
	a := blockedPane(t)
	if !strings.Contains(shown(a), cardAllowLabel) {
		t.Fatalf("an unarmed card does not advertise the allow key:\n%s", shown(a))
	}

	// The allow key, because the refusal opens a box for its reason now rather
	// than arming - see cardanswer.go, and TestASlipIntoTheReasonBox for the
	// line that box draws. This is about the arm, which the allow key is what
	// reaches on a permission card.
	a, _ = press(a, cardAllowKey)

	out := shown(a)
	// The verb without its bracket: the bracket is a claim about which key to
	// press, and while a settle is armed that key is ↵.
	if !strings.Contains(out, unbracket(cardAllowLabel)) {
		t.Errorf("an armed card does not name what the confirm would do:\n%s", out)
	}
	if strings.Contains(out, cardAllowLabel) {
		t.Errorf("an armed card still advertises %q, which is the key that no longer confirms it:\n%s", cardAllowLabel, out)
	}
	if !strings.Contains(out, cardConfirmGlyph) {
		t.Errorf("an armed card does not name the key that confirms it:\n%s", out)
	}
	if !strings.Contains(out, cardArmedCancel) {
		t.Errorf("an armed card does not say what takes it back, so the only visible way out reads as pressing the key again:\n%s", out)
	}
	// The fourth candidate shape in the brief was "record plainly that this
	// cannot be undone". decisions.md is not a surface an operator reads; this
	// is the one place the fact the whole ruling rests on can be said to the
	// person about to act on it.
	if !strings.Contains(out, cardArmedFinal) {
		t.Errorf("an armed card reads as an are-you-sure and never says the second press cannot be taken back:\n%s", out)
	}
}

// And the unarmed line has to say the key takes a second one, or nothing warns
// until the first press has already vanished.
//
// Asserted on the card's own key line rather than on the frame: the composer's
// legend draws ↵ too, so a frame-wide search for that glyph is satisfied by a
// surface this test is not about - which is what the first draft did.
func TestTheUnarmedCardSaysASettleTakesAConfirm(t *testing.T) {
	a := blockedPane(t)
	top, _ := a.cards.Top()
	line := a.cards.keyLine(top, wideRoom, false)

	if !strings.Contains(line, cardConfirmGlyph) {
		t.Errorf("the unarmed key line is %q: it advertises the settle keys as though one press did it", line)
	}
	if !strings.Contains(shown(a), line) {
		t.Errorf("the key line %q is not on the card that draws it:\n%s", line, shown(a))
	}
}

// An armed settle names what a second press would do, and the word depends on
// the shape: a tool call, a choice, or a plan. The unarmed labels already make
// that distinction - a plan approved by a key labelled "allow" reads as though
// something is about to be run - and the armed line has to make the same one
// rather than a second, differently-wrong one.
//
// Asserted against the *unarmed* line for the same card rather than against a
// list of the labels, so the two accounts of one key cannot disagree. And
// enumerated from core's own AskKind constants for
// TestOneMechanismCarriesThreeShapes' reason: a fourth kind falls to the
// default and would arm with a word nobody chose.
func TestAnArmedSettleNamesWhatASecondPressWouldDoOnEveryShape(t *testing.T) {
	kinds := constValuesOfType(t, filepath.Join("..", "core", "event.go"), "AskKind")
	if len(kinds) < 3 {
		t.Fatalf("found %d AskKind constants in core: the scan is broken and this test is asserting nothing", len(kinds))
	}

	for name, value := range kinds {
		card := answerableCard(core.AskKind(value))
		unarmed := card.keys(wideRoom)

		// Which settle keys this shape offers at all, rather than an
		// assumption that every shape offers both. A question is settled on
		// its review step - which names every answer before the press, where
		// an arm named only a verb - so it advertises the refusal and no
		// allow. Held as an iff rather than skipped, so a *different* shape
		// losing its allow key is still a failure here.
		offered := []rune{cardDenyKey}
		if allows := card.Shape() != ShapeQuestion; allows {
			offered = append(offered, cardAllowKey)
		}
		if _, has := labelIn(unarmed, cardAllowKey); has != (card.Shape() != ShapeQuestion) {
			t.Errorf("core.%s draws %q unarmed, which advertises [%c]=%v - want %v for this shape", name, unarmed, cardAllowKey, has, card.Shape() != ShapeQuestion)
		}

		for _, key := range offered {
			want, ok := labelIn(unarmed, key)
			if !ok {
				t.Errorf("core.%s draws %q unarmed, which does not advertise [%c] - so this shape offers the armed line no label to be checked against", name, unarmed, key)
				continue
			}
			armed := (Cards{open: []Card{card}}).arm(card, key).keyLine(card, wideRoom, false)
			if !strings.Contains(armed, unbracket(want)) {
				t.Errorf("core.%s armed on [%c] draws %q, which does not name %q - the verb the same card advertises unarmed", name, key, armed, unbracket(want))
			}
			if strings.Contains(armed, unarmed) {
				t.Errorf("core.%s armed on [%c] still draws the whole unarmed line %q, so the card says nothing changed", name, key, armed)
			}
		}
	}
}

// answerableCard is one card of the given kind that offers both settle keys: a
// question needs every question chosen before its allow key is advertised at
// all, and the other two ignore the payload.
func answerableCard(kind core.AskKind) Card {
	return Card{
		AgentID: "s1", RequestID: "r1", Tool: "Bash", Ask: kind,
		Detail: &core.AskDetail{
			Plan:      "step one",
			Questions: []core.Question{{Text: "which?", Options: []core.Option{{Label: "this one"}}}},
		},
		Choice: []int{0},
	}
}

// labelIn is the affordance a key line advertises for one key: the field that
// opens with that key in brackets.
func labelIn(line string, key rune) (string, bool) {
	bracket := cardKeyOpen + string(key) + cardKeyClose
	for _, field := range strings.Split(line, cardKeySep) {
		if strings.HasPrefix(field, bracket) {
			return field, true
		}
	}
	return "", false
}

// A card advertising a key nothing binds is the legend rule broken on the one
// surface the legend guard cannot see, and the drawn bracket is the only thing
// an operator has to go on.
//
// So the bound set is read out of cardKey's own switch rather than restated
// here - which is what TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed
// does with App.key, and what the first draft of this test did not do. A
// hand-written pair of constants pins those two and enforces nothing: `case r
// == cardDenyKey && top.Shape() != ShapePlan` left a plan card drawing [d]eny
// with the key doing nothing, and the whole suite green.
func TestEveryRuneTheCardBracketsIsOneCardKeysBinds(t *testing.T) {
	bound := settleKeyCasesIn(t, "cardkeys.go", "cardKey")
	if len(bound) == 0 {
		t.Fatal("cardKey's switch compares against no card key constant: the scan is broken and this test is asserting nothing")
	}

	drawn := map[string]bool{}
	for _, line := range []string{cardAllowKeys, cardAnswerKeys, cardPlanKeys, cardDenyLabel} {
		for _, r := range bracketed(line) {
			drawn[string(r)] = true
		}
	}
	if len(drawn) == 0 {
		t.Fatal("no card line brackets a key at all: this test is asserting nothing")
	}

	for r := range drawn {
		if !bound[r] {
			t.Errorf("the card advertises [%s] and cardKey's switch names no constant holding it", r)
		}
	}
	for r := range bound {
		if !drawn[r] {
			t.Errorf("cardKey binds %q and no card line advertises it", r)
		}
	}
}

// settleKeyCasesIn is every `card…Key` constant a method's switch compares
// against, as the rune it holds. Read out of the AST, so the set is what the
// code decides rather than what a test restates.
func settleKeyCasesIn(t *testing.T, file, method string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	runes := map[string]rune{"cardAllowKey": cardAllowKey, "cardDenyKey": cardDenyKey}

	out := map[string]bool{}
	found := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != method || fn.Recv == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if r, known := runes[id.Name]; known {
				out[string(r)] = true
			}
			return true
		})
	}
	if !found {
		t.Fatalf("%s has no method %q: the scan is looking at the wrong thing", file, method)
	}
	return out
}

// The other direction, and the one an AST scan cannot make: a key the switch
// names may still be excluded for some shapes. Every rune a shape's own line
// brackets has to be taken by cardKey on a card of that shape.
//
// Enumerated from core's AskKind constants rather than the three shapes that
// exist today, for TestOneMechanismCarriesThreeShapes' reason.
func TestEveryKeyACardDrawsIsTakenOnThatShape(t *testing.T) {
	kinds := constValuesOfType(t, filepath.Join("..", "core", "event.go"), "AskKind")
	if len(kinds) < 3 {
		t.Fatalf("found %d AskKind constants in core: the scan is broken", len(kinds))
	}

	for name, value := range kinds {
		card := answerableCard(core.AskKind(value))
		drawn := bracketed(card.keys(wideRoom))
		if len(drawn) == 0 {
			t.Errorf("core.%s draws no bracketed key at all, so this case asserts nothing", name)
			continue
		}
		for _, r := range drawn {
			// The card's own agent's conversation, holding the keys: a card is
			// drawn on no other surface, so a room-focused App would be
			// asserting about keys nothing offers.
			a := newRoomApp(t).withSize(200, 40).withAgents("john", "sydney").
				openDMWith("s1", "john").opened(t)
			a.cards = Cards{open: []Card{card}}

			a, cmd := press(a, r)
			if cmd != nil {
				t.Errorf("core.%s: [%c] wrote a frame on one press", name, r)
			}
			// Taken, rather than armed: a digit chooses and a settle key arms,
			// and what both have in common is that the rune does not fall
			// through to the draft. A key the card draws and does not take is
			// one the operator presses to no effect while it appears in what
			// they are writing.
			if got := a.composer().Value(); got != "" {
				t.Errorf("core.%s draws [%c] and cardKey handed it back (draft is %q): the card advertises a key that is dead on that shape", name, r, got)
			}
		}
	}
}

// An ask arriving from another agent is not a keystroke, and the arm may not
// be taken back by one. At thirty agents that is most of the time, so an arm
// dropped by it is a confirm the operator cannot reach - which is the argument
// Reconcile already carries, applied to the seam beside it.
func TestAnUnrelatedAgentsAskDoesNotTakeBackAnArmedSettle(t *testing.T) {
	a := blockedPane(t)

	a, _ = press(a, cardDenyKey)
	a = a.applyFrame(askOn("s3", "r2"))
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameDeny || f.SessionID != "s2" || f.RequestID != "r1" {
		t.Errorf("the settle sent %+v: another agent blocking is not a keystroke and may not take an arm back", f)
	}
}

// The M12 lesson, audited across the type: an ask id is unique per agent and
// nothing promises it across them, so settling one agent's ask may not take
// another agent's card down. Locally only - the next report puts it back - but
// what it costs meanwhile is the card on a blocked agent nobody is looking at.
func TestSettlingOneAgentsAskLeavesAnotherAgentsCardWithTheSameIdAlone(t *testing.T) {
	a := blockedPane(t).applyFrame(askOn("s3", "r1"))
	if a.cards.Len() != 2 {
		t.Fatalf("two agents holding one ask id produced %d cards, so this test asserts nothing", a.cards.Len())
	}

	a, _ = settleKey(a, cardDenyKey)

	if a.cards.Len() != 1 {
		t.Fatalf("settling s2/r1 left %d cards: it took down s3's card, which holds the same ask id and is a different ask", a.cards.Len())
	}
	if top, _ := a.cards.Top(); top.AgentID != "s3" {
		t.Errorf("the card left standing is %s's rather than s3's", top.AgentID)
	}
}

// ⎋ and ⌃C from the room act on the roster's pick, which is the only thing on
// that surface naming an agent - the room draws no card. They used to prefer
// the agent whose card the room drew, and that preference went with the card:
// an ask is in its own conversation now, and a conversation is the *first*
// case interruptTarget answers.
//
// The arrangement is what tells the versions apart: a blocked agent with an
// ask, and a different agent under the cursor.
func TestEscAndCtrlCFromTheRoomActOnTheRosterPick(t *testing.T) {
	for what, key := range map[string]tea.KeyMsg{
		"⎋":  {Type: tea.KeyEsc},
		"⌃C": {Type: tea.KeyCtrlC},
	} {
		a := newRoomApp(t).withSize(200, 40).withRoster(
			rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateBlocked},
			rpc.SessionStatus{ID: "s2", Name: "robin", State: rpc.StateWorking},
		)
		a.roster.Selected = "s2"
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
			Kind: core.KindPermissionRequest, RequestID: "r1",
			Tool: &core.ToolCall{Name: "Bash", Display: "rm -rf build/"},
		}})
		if a.focus != "" {
			t.Fatal("the keys are not in the room, so this test is not exercising the room's rule")
		}
		if top, ok := a.cards.Top(); !ok || top.AgentID != "s1" {
			t.Fatal("the ask this test arms is not on top, so the pick and the card name the same agent")
		}

		a2, cmd := pressKey(a, key)

		frames := framesFrom(t, a2, cmd)
		if len(frames) == 0 {
			t.Fatalf("%s from the room wrote nothing, so nothing here says who it reached", what)
		}
		for _, f := range frames {
			if f.SessionID != "s2" {
				t.Errorf("%s wrote %s to %s, want the picked robin (s2): the room has no card to claim the key", what, f.Kind, f.SessionID)
			}
		}
	}
}

// framesFrom runs a command if there is one and returns whatever reached the
// daemon, which may be nothing.
func framesFrom(t *testing.T, a App, cmd tea.Cmd) []rpc.Frame {
	t.Helper()
	if cmd == nil {
		return nil
	}
	if msg, ok := cmd().(errMsg); ok {
		t.Fatalf("the write failed: %v", msg.Err)
	}
	return recorderOf(t, a).taken(t)
}

// bracketed is every single rune a line puts in brackets. A range like [1-4]
// is not one of them - it names digits rather than a settle - so only a
// one-rune bracket counts.
func bracketed(line string) []rune {
	var out []rune
	for i, r := range line {
		if r != '[' {
			continue
		}
		rest := line[i+len("["):]
		if len(rest) < 2 || rest[1] != ']' {
			continue
		}
		out = append(out, rune(rest[0]))
	}
	return out
}
