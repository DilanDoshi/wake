package ui

// The completion menu, from the outside: what it offers, what it never claims,
// and the keys that walk it.

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The whole path for what a session advertises: an init frame's command set,
// decoded by core, folded onto the agent, offered by the menu.
func TestTheMenuOffersWhatTheSessionAdvertises(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.advertising("s1", "new-oscar", "new-victor", "kilo-check").withDraft("/new-")

	got := a.completion.offers
	for _, want := range []string{"/new-oscar", "/new-victor"} {
		if !slices.Contains(got, want) {
			t.Errorf("the menu offers %q for `/new-`, want %q among them: the init frame carries the "+
				"operator's own commands and Wake was throwing them away", got, want)
		}
	}
	if slices.Contains(got, "/kilo-check") {
		t.Errorf("the menu offers %q, which does not start with what was typed: a menu that ignores "+
			"the draft is a list, not a completion", "/kilo-check")
	}
}

// A result frame names no commands. Blanking on one would empty the menu once
// per turn - the trap Model and the context window are guarded against.
func TestATurnEndingDoesNotEmptyTheAdvertisedSet(t *testing.T) {
	a := Agent{ID: "s1"}.withFacts(&core.SessionFacts{
		Model:         "claude-opus-4-6",
		SlashCommands: []string{"clear", "compact"},
	})
	if len(a.advertised.words()) != 2 {
		t.Fatalf("the init did not establish the set: %q", a.advertised.words())
	}

	after := a.withFacts(&core.SessionFacts{ContextTokens: 900, ContextWindow: 200_000, OutputTokens: 12})
	if got := after.advertised.words(); len(got) != 2 {
		t.Errorf("a turn ending left the advertised set as %q, want the two the init named: the list "+
			"rides on init and a frame carrying none says nothing about it", got)
	}
}

// Fleet.Observe decides whether a frame moved anything by comparing two Agents,
// so an init that re-advertises the same words must compare equal - otherwise
// every turn costs a fleet-sized copy for a set that did not change.
func TestReAdvertisingTheSameCommandsMovesNothing(t *testing.T) {
	facts := &core.SessionFacts{Model: "claude-opus-4-6", SlashCommands: []string{"clear", "compact"}}
	first := Agent{ID: "s1"}.withFacts(facts)
	if again := first.withFacts(facts); again != first {
		t.Error("an init re-advertising the same commands produced a different Agent, so every turn " +
			"would copy the whole fleet for a set nobody changed")
	}
}

// Wake's own commands are in the menu, and they are derived from the router's
// own vocabulary rather than written out here - a menu that omits them teaches
// the operator the wrong set.
func TestTheMenuOffersEveryCommandWakeOwns(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("alex").withSize(200, 40).withDraft("/")

	offered := append([]string(nil), a.completion.offers...)
	for _, verb := range wakeVerbs() {
		if !slices.Contains(offered, verb) {
			t.Errorf("the menu does not offer %q. Wake's own commands are the ones claude cannot "+
				"advertise, so a menu without them is the only place they are invisible: %q", verb, offered)
		}
	}
}

// In the room, `@iris /lima-rep` completes IRIS's advertised commands - the agent
// the draft addresses - even when the roster cursor rests on somebody else.
//
// The room used to offer whichever agent the cursor was on, ignoring the @name
// typed in front of the command: `@iris /lima-rep` with the cursor on alex offered
// alex's set, matched nothing, and drew no menu at all - the skill autocomplete
// a person watches vanish the moment they address it. `@iris /command` configures
// iris (mention.go's direct route), so the menu that completes it resolves the
// same agent.
func TestTheRoomCompletesTheAddressedAgentsCommands(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("alex", "iris").withSize(200, 40)
	a = a.advertising("s2", "lima-report-sync", "kilo-check") // iris is s2
	a = pick(a, "s1")                                         // cursor on alex, not iris
	a = a.withDraft("@iris /lima-rep")

	if !a.completionUp() {
		t.Fatalf("`@iris /lima-rep` drew no menu with the cursor on alex: the room offered the cursor's "+
			"commands rather than the addressed agent's, so the skill autocomplete disappears the moment it is "+
			"routed. offers=%q", a.completion.offers)
	}
	if !slices.Contains(a.completion.offers, "/lima-report-sync") {
		t.Errorf("`@iris /lima-rep` offered %q, want iris's own /lima-report-sync", a.completion.offers)
	}
	// And it reaches the screen: the reported symptom is a menu that never
	// appears, which is the room pane drawing nothing above its composer.
	if drawn := a.roomPane(200, 40); !strings.Contains(drawn, "/lima-report-sync") {
		t.Errorf("the room pane drew no completion menu:\n%s", drawn)
	}
}

// Open mention mode widens a message, not a knob: `@iris /lima-rep` in open mode
// still completes iris's own commands, not the fleet's, because the menu reads
// the direct route the way a configure command does.
func TestOpenMentionModeStillCompletesTheOneAddressedAgent(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("alex", "iris").withSize(200, 40)
	a = a.advertising("s2", "lima-report-sync") // iris is s2
	a.mention = MentionOpen
	a = pick(a, "s1") // cursor on alex
	a = a.withDraft("@iris /lima-rep")

	if !slices.Contains(a.completion.offers, "/lima-report-sync") {
		t.Errorf("`@iris /lima-rep` in open mode offered %q, want iris's /lima-report-sync: open "+
			"mode widens what is said, not which session a command configures", a.completion.offers)
	}
}

// The session's own commands and skills come first, so a bare `/` surfaces what
// the operator configured in Claude Code - their skills and `.claude/commands` -
// rather than burying every one of them behind Wake's twelve verbs. Owner's
// 2026-08-28 override of the old "Wake first" ordering: skills ride in
// `slash_commands`, so a menu that Wake's own verbs fill to the bound is a menu
// in which the operator's skills are never seen at a bare slash.
func TestTheSessionsCommandsComeBeforeWakes(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	// More advertised commands than the bound, so if Wake's verbs came first
	// they would push every one of these into the overflow.
	skills := []string{"deep-research", "dataviz", "code-review", "simplify",
		"verify", "debug", "commit-push", "graphify", "learn", "plan",
		"review", "test", "market-research", "product-lens"}
	a = a.advertising("s1", skills...).withDraft("/")

	got := a.completion.offers
	if len(got) == 0 {
		t.Fatal("`/` offered nothing at all")
	}
	if slices.Contains(wakeVerbs(), got[0]) {
		t.Errorf("`/` offers Wake's %q first: the session's own skills must come first so a bare slash "+
			"surfaces the operator's Claude Code skills rather than Wake's verbs. offers=%q", got[0], got)
	}
	if !slices.Contains(got, "/deep-research") {
		t.Errorf("`/deep-research` is not visible at a bare slash: %q (%d more) - a skill the session "+
			"advertised was pushed into the overflow behind Wake's verbs", got, a.completion.more)
	}
}

// A word both Wake and the session advertise is offered once, whichever comes
// first: `/effort`, `/model` and `/mcp` are claude's words Wake also claims, and
// two identical rows is a menu the operator cannot choose between. The dedup
// outlived the ordering reversal above.
func TestAWordBothSidesAdvertiseIsOfferedOnce(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.advertising("s1", "model", "moo", "monkey").withDraft("/mo")

	got := a.completion.offers
	if !slices.Contains(got, configureVerb(modelCommand)) {
		t.Errorf("`/mo` did not offer %q at all: %q", configureVerb(modelCommand), got)
	}
	if n := strings.Count(strings.Join(got, " "), configureVerb(modelCommand)+" "); n > 1 {
		t.Errorf("%q offers the same word twice: claude advertises it and Wake claims its bare form, "+
			"and a menu with two identical rows is one the operator cannot choose between", got)
	}
}

// The menu is bounded however many match. 133 advertised commands is a menu
// taller than a pane, and a pane taller than the terminal scrolls the alt
// screen away on every draw.
func TestTheMenuIsBoundedHoweverManyMatch(t *testing.T) {
	fresh(t)
	many := make([]string, 0, 200)
	for _, r := range "abcdefghijklmnopqrstuvwxyz" {
		for i := range 8 {
			many = append(many, string(r)+string(rune('0'+i)))
		}
	}
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.advertising("s1", many...).withDraft("/")

	if got := len(a.completion.offers); got > completionRows {
		t.Errorf("the menu drew %d rows for %d commands, want at most %d", got, len(many), completionRows)
	}
	if a.completion.more == 0 {
		t.Fatalf("%d commands matched and the menu says nothing was left out: a bound that is silent "+
			"reads as a complete list", len(many))
	}
	if drawn := a.completionView(80, a.focus); !strings.Contains(drawn, "more") {
		t.Errorf("the menu drew\n%s\nand said nothing about what it left out", drawn)
	}
}

// A draft with an argument is not a command being typed - it is a message, and
// the fence passes it through byte for byte.
func TestADraftWithAnArgumentGetsNoMenu(t *testing.T) {
	// `look at /clear` is deliberately absent: the word at the cursor is a
	// command there, so it gets a menu - TestACommandCompletesMidText. A
	// finished command (`/clear ` and `/clear now`) and plain prose do not.
	for _, draft := range []string{"/clear now", "/clear ", "hello"} {
		t.Run(draft, func(t *testing.T) {
			// A fresh model per draft: composers share one text area by pointer,
			// so two drafts typed into one App are one accumulating draft.
			fresh(t)
			a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
			a = a.advertising("s1", "clear", "compact").withDraft(draft)
			if a.completionUp() {
				t.Errorf("%q opened a menu offering %q; only the word at the cursor being a command is one",
					draft, a.completion.offers)
			}
		})
	}
}

// A finished command is a message: `/clear now` ends in an argument, so the word
// at the cursor is not a command and commandStem refuses it - which is what the
// menu reads to stay down over a command the operator has already finished.
func TestACommandStemEndsAtAnyWhitespace(t *testing.T) {
	for _, draft := range []string{"/clear now", "/clear\tnow", "/clear\nnow"} {
		if _, word, ok := commandStem(draft); ok {
			t.Errorf("commandStem(%q) = %q: the word at the cursor is the argument `now`, not a command, "+
				"and the `@` half has always read a tab and a newline as ending a word", draft, word)
		}
	}
}

// A `/command` completes when it is the word at the cursor, not only when it is
// the whole draft - the `@` half has read the last token since it shipped, and
// Claude Code offers a command the same way. `look at /cle` is a message with a
// command being typed at the end of it.
func TestACommandCompletesMidText(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.advertising("s1", "clear", "compact").withDraft("look at /cle")
	if !a.completionUp() {
		t.Fatal("a /command after other text drew no menu; the word at the cursor is the one being typed")
	}
	if got, want := a.completion.head, "look at "; got != want {
		t.Errorf("the menu's head is %q, want %q so an accept keeps the text before the command", got, want)
	}
	if !slices.Contains(a.completion.offers, "/clear") {
		t.Errorf("the mid-text menu offered %q, want it to include /clear", a.completion.offers)
	}
}

// And ⇥ over a mid-text command keeps what came before it, inserting the offer
// where the word was rather than replacing the whole draft.
func TestTabTakesAMidTextOffer(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("alex").withSize(200, 40).withDraft("look at /resum")
	next, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := next.composer().Value(), "look at "+resumeVerb+" "; got != want {
		t.Errorf("⇥ over a mid-text `/resum` left the draft %q, want %q", got, want)
	}
}

// The cursored offer wears the completion's own purple, not the orange accent a
// card and the picker use. Colour is only observable with a profile forced on,
// which go test has none of by default.
func TestTheCursoredOfferIsThePurpleNotTheAccent(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(2) // termenv.ANSI - any colour profile proves the style applied
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	offer := "/lima-report-sync"
	first := strings.SplitN(completion{offers: []string{offer}, cursor: 0}.View(60), "\n", 2)[0]

	if want := optionRow(offer, 60, true, false, CompletionStyle); first != want {
		t.Errorf("the cursored offer is not the completion purple:\n got %q\nwant %q", first, want)
	}
	if orange := optionRow(offer, 60, true, false, AccentStyle); first == orange {
		t.Error("the cursored offer is still the orange accent a card and the picker wear")
	}
}

// The menu describes the word under the cursor, and claims its keys only there.
//
// ⌃N/⌃P are the *only* vertical movement inside a multi-line draft - ↑↓ are the
// roster's on every path - so a menu that claims them from a cursor three lines
// away from the mention it describes is a draft with no way to move in it.
func TestTheMenuLeavesTheLineKeysToACursorSomewhereElse(t *testing.T) {
	dir := workdir(t, "alexander.md")
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	)
	a = a.withDraft("one")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlJ})
	a = a.withDraft("two")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlJ})
	a = a.withDraft("three @al")
	if !a.completionUp() {
		t.Fatal("the fixture drew no menu with the cursor on the mention, so this asserts nothing")
	}

	// alt+< is the text area's own "go to the beginning of the input".
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}, Alt: true})
	if a.completionUp() {
		t.Errorf("the menu still offers %q with the cursor at the start of a three-line draft: it "+
			"describes a word nobody is typing", a.completion.offers)
	}

	// ⌃N is not the menu's while the menu is down, which is the claim this test
	// can still make. Where the key goes *instead* stopped being the text area's
	// next-line when the dispatch list took ⌃N/⌃P unconditionally (keys.go, and
	// taskkeys.go for why not ↑↓) - so asserting the cursor moved a line would be
	// asserting against a ruling this file does not own.
	if _, _, handled := a.completionKey(tea.KeyMsg{Type: tea.KeyCtrlN}); handled {
		t.Error("the menu took ⌃N with no menu on screen: it answers keys only while it is up, or " +
			"every draft in the build loses a key to a menu nobody opened")
	}
	if _, _, handled := a.completionKey(tea.KeyMsg{Type: tea.KeyCtrlP}); handled {
		t.Error("the menu took ⌃P with no menu on screen")
	}
}

// A draft long enough to wrap is still one line, and a cursor at the end of it
// is still at the end. LineInfo describes a *soft* line, so reading its column
// without its start column would put the cursor at the end of a wrap - and
// every draft past one row would silently lose its menu.
func TestAWrappedDraftStillHasItsCursorAtTheEnd(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("alex").withSize(60, 40)
	a = a.withDraft(strings.Repeat("word ", 12) + agentPrefix + "al")
	if !a.completionUp() {
		t.Errorf("a draft long enough to wrap offers %q, so the menu is gone: the cursor is at the "+
			"end of it, and only the end of a wrapped row looks otherwise", a.completion.offers)
	}
}

// The menu is built for one pane's session and may not survive a move to
// another. Two conversations whose directories share a README.md would
// otherwise complete one from the other's - and resolve, silently, to the
// wrong file.
func TestTheMenuDoesNotFollowTheKeysToAnotherPane(t *testing.T) {
	alex, sydney := workdir(t, "onlyA.md"), workdir(t, "onlyB.md")
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: alex, State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "sydney", Dir: sydney, State: rpc.StateIdle},
	).withDraft(agentPrefix)

	a = a.openRight("s2", "sydney").withDraft(agentPrefix)
	if !slices.Contains(a.completion.offers, agentPrefix+"onlyB.md") {
		t.Fatalf("sydney's own pane offers %q, so this asserts nothing about carrying it", a.completion.offers)
	}

	// The keys move back to alex. Every focus change routes through refocus -
	// ⇥, ⌃D, ⌃Y, ⌃B, ⌃W and a click - and the key itself is not pressable here,
	// because ⇥ with a menu up is the accept.
	back := a.refocus("s1")
	if back.completionUp() {
		t.Errorf("alex's pane draws sydney's menu, offering %q: both drafts are %q, so the draft is "+
			"not what tells two panes apart", back.completion.offers, agentPrefix)
	}
	next, _ := pressKey(back, tea.KeyMsg{Type: tea.KeyTab})
	if got := next.composer().Value(); strings.Contains(got, "onlyB") {
		t.Errorf("⇥ in alex's pane inserted %q, which is a file in another session's directory", got)
	}
}

// ⇥ finishes the cursored word. It is the accept because it is the accept
// everywhere else, and because taking ↵ would give send a second meaning.
func TestTabTakesTheCursoredOffer(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("alex").withSize(200, 40).withDraft("/resum")

	next, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := next.composer().Value(), resumeVerb+" "; got != want {
		t.Errorf("⇥ over `/resum` left the draft %q, want %q", got, want)
	}
	if next.completionUp() {
		t.Error("the menu stayed up over a finished word, and its ⇥ would then move the keys nowhere")
	}
}

// The menu walks without ↑↓, which belong to the roster. ⌃N and ⌃P are the
// text area's line keys, and a single-token draft has no second line to move
// to - which is what makes them free exactly while this menu is up.
func TestTheMenuWalksWithoutTheArrowKeys(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.advertising("s1", "zebra-one", "zebra-two", "zebra-three").withDraft("/zebra-")
	if len(a.completion.offers) < 3 {
		t.Fatalf("the fixture offers %q, which is too few to walk", a.completion.offers)
	}

	down, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	if down.completion.cursor != 1 {
		t.Errorf("⌃N left the cursor at %d, want 1", down.completion.cursor)
	}
	up, _ := pressKey(down, tea.KeyMsg{Type: tea.KeyCtrlP})
	if up.completion.cursor != 0 {
		t.Errorf("⌃P left the cursor at %d, want 0", up.completion.cursor)
	}
	if got := up.completion.offers; !slices.Equal(got, a.completion.offers) {
		t.Errorf("walking rebuilt the offers as %q, want %q unchanged: the draft did not move", got, a.completion.offers)
	}

	// And ⇥ takes whatever the walk landed on.
	took, _ := pressKey(down, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := took.composer().Value(), down.completion.offers[1]+" "; got != want {
		t.Errorf("⇥ after ⌃N left the draft %q, want %q", got, want)
	}
}

// ↑↓ are the roster's and stay the roster's. Their rebinding is an open ruling,
// and a menu that took them would decide it.
func TestTheMenuNeverTakesTheArrowKeys(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)
	a = a.advertising("s1", "zebra-one", "zebra-two").withDraft("/zebra-")
	if !a.completion.open() {
		t.Fatal("the fixture drew no menu, so this asserts nothing about the keys")
	}

	for _, k := range []tea.KeyType{tea.KeyUp, tea.KeyDown} {
		next, _ := pressKey(a, tea.KeyMsg{Type: k})
		if next.completion.cursor != 0 {
			t.Errorf("%v moved the menu's cursor to %d: ↑↓ pick an agent", k, next.completion.cursor)
		}
		if next.roster.Selected == "" {
			t.Errorf("%v with a menu up picked no agent, so the menu swallowed the roster's key", k)
		}
	}
}

// ↵ still sends. The menu appears while somebody types rather than because they
// asked for it, so it may not change what the one irreversible key does.
func TestEnterStillSendsWhileTheMenuIsUp(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.advertising("s1", "clearly", "clear").withDraft("/clea")
	if !a.completion.open() {
		t.Fatal("the fixture drew no menu, so this asserts nothing")
	}

	m, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("↵ with a menu up wrote nothing: the menu took the send")
	}
	go func() { _ = runCmdQuietly(cmd) }()
	f := <-sent
	if f.Text != "/clea" {
		t.Errorf("↵ sent %q, want the draft as typed: a menu that completed on ↵ would send a "+
			"command nobody chose", f.Text)
	}
	if m.(App).completionUp() {
		t.Error("the menu survived the send, so the next ⇥ would complete a word that is gone")
	}
}

// The menu belongs to the pane holding the keys, and to the draft it was built
// for. A pane change leaves a menu describing somebody else's draft.
func TestTheMenuIsDrawnOnlyForTheDraftItWasBuiltFor(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.advertising("s1", "clear", "compact").withDraft("/c")
	if a.completionView(60, a.focus) == "" {
		t.Fatal("the menu is not drawn in the pane whose draft built it")
	}
	if got := a.completionView(60, "somebody-else"); got != "" {
		t.Errorf("another pane draws the menu:\n%s", got)
	}

	stale := a.withComposer(a.composer().WithDraft("something else entirely"))
	if got := stale.completionView(60, stale.focus); got != "" {
		t.Errorf("a menu built for another draft is still drawn:\n%s", got)
	}
}

// advertising is an init frame naming a session's command set, the way claude
// announces one at the start of every turn.
func (a App) advertising(sessionID string, names ...string) App {
	return a.observe(sessionID, core.Event{
		Kind:      core.KindSystem,
		SessionID: sessionID,
		Session:   &core.SessionFacts{Model: "claude-opus-4-6", SlashCommands: names},
	})
}
