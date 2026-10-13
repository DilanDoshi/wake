package ui

// The completion menu's ↵ and ⇥ as the owner ruled them on 2026-10-12, reversing
// 2026-09-27's "the menu never takes ↵, and ⇥ is the only accept": while a menu is
// up ↵ accepts what is highlighted, and while a `/` or `@` word is being typed ⇥
// never leaves the draft - a misspelled skill used to fall through to ⇥'s
// conversation ring and land somewhere unexpected.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

var tabKey = tea.KeyMsg{Type: tea.KeyTab}

// typedMenu is alex's conversation with a draft that draws a menu over the
// advertised commands.
func typedMenu(t *testing.T, draft string, commands ...string) App {
	t.Helper()
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.advertising("s1", commands...).withDraft(draft)
	if !a.completionUp() {
		t.Fatalf("the fixture drew no menu for %q, so this asserts nothing", draft)
	}
	return a
}

// sentTexts is the text of every FrameSend a keypress wrote.
func sentTexts(frames []rpc.Frame) []string {
	var out []string
	for _, f := range frames {
		if f.Kind == rpc.FrameSend {
			out = append(out, f.Text)
		}
	}
	return out
}

func TestEnterAcceptsTheHighlightedOffer(t *testing.T) {
	a := typedMenu(t, "/clea", "clearly", "clear")
	want := a.completion.offers[a.completion.cursor] + " "

	a, cmd := pressKey(a, confirm)
	if got := a.composer().Value(); got != want {
		t.Errorf("↵ left the draft %q, want the highlighted offer %q", got, want)
	}
	if sent := sentTexts(writtenFrames(t, a, cmd)); len(sent) != 0 {
		t.Errorf("↵ over a menu sent %q: it should have completed the word, not routed it", sent)
	}
}

// The offer ↵ takes is the one the cursor is on, which is what ↑↓ walk.
func TestEnterAcceptsTheOfferTheCursorIsOn(t *testing.T) {
	a := typedMenu(t, "/clea", "clearly", "clear")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if a.completion.cursor != 1 {
		t.Fatalf("↓ left the cursor on %d, want the second offer", a.completion.cursor)
	}
	want := a.completion.offers[1] + " "

	a, _ = pressKey(a, confirm)
	if got := a.composer().Value(); got != want {
		t.Errorf("↵ after ↓ left the draft %q, want the second offer %q", got, want)
	}
}

// A word that is already exactly what is highlighted has nothing to complete, so
// ↵ sends it. Otherwise every `/clear` typed in full would need a second ↵.
func TestEnterSendsAWordThatIsAlreadyFullyTyped(t *testing.T) {
	a := typedMenu(t, "/clear", "clear")
	if got := a.completion.offers[a.completion.cursor]; got != "/clear" {
		t.Fatalf("the cursor is on %q, so this asserts nothing about a typed-out word", got)
	}

	a, cmd := pressKey(a, confirm)
	if sent := sentTexts(writtenFrames(t, a, cmd)); len(sent) != 1 || sent[0] != "/clear" {
		t.Errorf("↵ sent %q, want the one command as typed", sent)
	}
}

// Once the accept has closed the menu - a word with a space after it is no
// longer being typed - ↵ is the send again.
func TestEnterSendsOnceTheMenuIsGone(t *testing.T) {
	a := typedMenu(t, "/clea", "clear")
	a, _ = pressKey(a, confirm)
	if a.completionUp() {
		t.Fatal("the menu survived its own accept, so the next ↵ is not the send under test")
	}

	a, cmd := pressKey(a, confirm)
	if sent := sentTexts(writtenFrames(t, a, cmd)); len(sent) != 1 || strings.TrimSpace(sent[0]) != "/clear" {
		t.Errorf("↵ after the accept sent %q, want the completed command", sent)
	}
}

// The room's `@` menu is the same: ↵ completes the highlighted name.
func TestEnterAcceptsAMentionInTheRoom(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("alex", "maya").withSize(200, 40).withDraft("@ma")
	if !a.completionUp() {
		t.Fatal("the fixture drew no menu, so this asserts nothing")
	}
	want := a.completion.offers[a.completion.cursor] + " "

	a, _ = pressKey(a, confirm)
	if got := a.composer().Value(); got != want {
		t.Errorf("↵ over `@ma` left the draft %q, want %q: it sent the half-typed mention or did nothing", got, want)
	}
}

// A finished command that is also the start of a longer one (`/design` beside
// `/design-sync`, `/team` beside `/team-onboarding`, as real sessions advertise
// them) must not be completed to the sibling, however the session ordered them:
// the word that is exactly what is typed goes first, so ↵ sends it, and walking
// down to the sibling is what takes the sibling.
func TestAFinishedCommandBeatsALongerSiblingForTheCursor(t *testing.T) {
	a := typedMenu(t, "/design", "design-sync", "design")
	if got := a.completion.offers[a.completion.cursor]; got != "/design" {
		t.Fatalf("the cursor starts on %q among %q, want the offer that is exactly what is typed", got, a.completion.offers)
	}

	sent, cmd := pressKey(a, confirm)
	if texts := sentTexts(writtenFrames(t, sent, cmd)); len(texts) != 1 || texts[0] != "/design" {
		t.Errorf("↵ sent %q, want the finished command as typed", texts)
	}

	down, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	taken, _ := pressKey(down, confirm)
	if got := taken.composer().Value(); got != "/design-sync " {
		t.Errorf("↵ after ↓ left the draft %q, want the sibling the cursor was walked to", got)
	}
}

// The bound must not cut the finished word either: a session with more skills
// than the menu draws can push `/new` out of the first rows behind its siblings.
func TestAFinishedCommandIsNotCutByTheBound(t *testing.T) {
	var skills []string
	for i := range completionRows + 4 {
		skills = append(skills, "new-skill"+string(rune('a'+i)))
	}
	skills = append(skills, "new")
	a := typedMenu(t, "/new", skills...)
	if got := a.completion.offers[a.completion.cursor]; got != "/new" {
		t.Fatalf("the cursor is on %q among %d offers, want /new kept inside the bound and first", got, len(a.completion.offers))
	}
}

// The word is compared behind whatever came before it: `look at /clear` is
// already typed in full, and ↵ sends it rather than completing a command that
// was already finished.
func TestAFinishedCommandBehindProseSends(t *testing.T) {
	a := typedMenu(t, "look at /clear", "clear")
	if got := a.completion.offers[a.completion.cursor]; got != "/clear" {
		t.Fatalf("the cursor is on %q, so this asserts nothing about a word behind prose", got)
	}

	a, cmd := pressKey(a, confirm)
	if sent := sentTexts(writtenFrames(t, a, cmd)); len(sent) != 1 || sent[0] != "look at /clear" {
		t.Errorf("↵ sent %q, want the draft as typed", sent)
	}
}

// An open quote is one word being typed: ↵ completes it to the quoted name, and
// once the closing quote is typed there is no menu left, so ↵ sends.
func TestEnterCompletesAnOpenQuotedMention(t *testing.T) {
	a := spacedListing(t).withDraft(`ask @"foo b`)
	if !a.completionUp() {
		t.Fatal("the fixture drew no menu for the open quote, so this asserts nothing")
	}
	took, _ := pressKey(a, confirm)
	if got, want := took.composer().Value(), `ask @"foo bar" `; got != want {
		t.Errorf("↵ over an open quote left %q, want %q", got, want)
	}

	closed := spacedListing(t).withDraft(`ask @"foo bar"`)
	if closed.completionUp() {
		t.Fatal("a closed quote drew a menu, so ↵ is not the send under test")
	}
}

// A working agent queues what is sent, so with a menu up the first ↵ completes the
// word and only the second queues it.
func TestTheFirstEnterCompletesAndTheSecondQueuesForAWorkingAgent(t *testing.T) {
	a := busyDM(t).advertising("s1", "clear").withDraft("/clea")
	if !a.completionUp() {
		t.Fatal("the fixture drew no menu, so this asserts nothing")
	}

	a, cmd := pressKey(a, confirm)
	if sent := sentTexts(writtenFrames(t, a, cmd)); len(sent) != 0 || len(a.queued["s1"]) != 0 {
		t.Fatalf("the first ↵ sent %q and queued %d: it should only have completed", sent, len(a.queued["s1"]))
	}

	a, cmd = pressKey(a, confirm)
	if sent := sentTexts(writtenFrames(t, a, cmd)); len(sent) != 1 || len(a.queued["s1"]) != 1 {
		t.Errorf("the second ↵ sent %q and queued %d, want the completed command written and pinned once", sent, len(a.queued["s1"]))
	}
}

// A directory completes without a trailing space - what is typed next goes inside
// it - so it is no longer "already offered" once typed in full: the menu then
// lists its children and ↵ takes the cursored one. A bare directory is sent by
// typing a space, which closes the menu.
func TestEnterStepsIntoADirectoryThenTakesItsChild(t *testing.T) {
	dir := workdir(t)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.go"), nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	a, _ := typedAsking(t, peerFleet(t, dir), runes("@sr")...)
	if !a.completionUp() {
		t.Fatal("the fixture drew no menu for `@sr`, so this asserts nothing")
	}

	a, _ = typedAsking(t, a, confirm)
	if got := a.composer().Value(); got != "@src/" {
		t.Fatalf("↵ over `@sr` left %q, want the directory with no trailing space", got)
	}
	a, _ = typedAsking(t, a, confirm)
	if got := a.composer().Value(); got != "@src/main.go " {
		t.Errorf("↵ over `@src/` left %q, want its child completed", got)
	}
}

// `/r` abbreviates one of Wake's own commands, and the router keys on the whole
// first word, so it reaches the agent. A bare ↵ over its menu now completes it,
// which is the point; closing the menu first - a space - sends what was typed.
// This is the guard TestAnOperatorsOwnCommandStillReachesTheAgent held for `/r`.
func TestAnAbbreviationOfAWakeCommandStillReachesTheAgentOnceTheMenuIsClosed(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	_, cmd := typeAndSubmit(a, "/r ")
	go func() { _ = runCmdQuietly(cmd) }()
	if f := awaitFrame(t, sent); f.Kind != rpc.FrameSend || strings.TrimSpace(f.Text) != "/r" {
		t.Errorf("submitting %q wrote %+v, want the abbreviation sent to the agent as typed", "/r ", f)
	}
}

// ⌥↵ is a newline, which the composer owns, and a menu does not take it.
func TestAltEnterStillMakesANewlineOverAMenu(t *testing.T) {
	a := typedMenu(t, "/clea", "clear")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got := a.composer().Value(); !strings.Contains(got, "\n") || strings.Contains(got, "/clear") {
		t.Errorf("⌥↵ left the draft %q, want a newline after what was typed and no accept", got)
	}
}

// An armed detach's ↵ is the confirm, whatever menu is up under it.
func TestAnArmedDetachConfirmIsNotTakenByAMenu(t *testing.T) {
	a := typedMenu(t, "/clea", "clear")
	a, _ = pressKey(a, ctrlO)

	if _, cmd := pressKey(a, confirm); !quits(cmd) {
		t.Fatal("↵ after ⌃O did not detach with a menu up: the accept took the confirm")
	}
}

// The bug: a skill misspelled so nothing matches leaves no menu, and ⇥ then fell
// through to the conversation ring. While a `/` or `@` word is being typed it
// stays where it is - in a conversation and in the room.
func TestTabStaysPutOnAWordNothingCompletes(t *testing.T) {
	for name, tc := range map[string]struct {
		draft string
		room  bool
	}{
		"a misspelled skill":              {draft: "/zzzskil"},
		"a skill after some prose":        {draft: "look at /zzzskil"},
		"a mention nothing matches":       {draft: "@zzznobody"},
		"the /team argument":              {draft: "/team zzznone"},
		"a misspelled skill in the room":  {draft: "/zzzskil", room: true},
		"a mention in the room":           {draft: "@zzznobody", room: true},
		"a bare marker before any letter": {draft: "@"},
	} {
		t.Run(name, func(t *testing.T) {
			fresh(t)
			a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
			if tc.room {
				a = a.refocus("")
			}
			a = a.withDraft(tc.draft)
			if a.completionUp() {
				t.Fatalf("%q draws a menu here, which ⇥ accepts; the case is the one with none, so this asserts nothing", tc.draft)
			}
			focus := a.focus

			a, _ = pressKey(a, tabKey)
			if a.focus != focus {
				t.Errorf("⇥ moved the keys from %q to %q while a word was being typed", focus, a.focus)
			}
			if got := a.composer().Value(); got != tc.draft {
				t.Errorf("⇥ changed the draft %q to %q", tc.draft, got)
			}
		})
	}
}

// ⇥ stays put only where a menu could describe the word. `/team <name>` in the
// room has no menu to offer - the command is not the room's - so its argument is
// plain text, and ⇥ is the ring's, where in a conversation the same draft keeps it.
func TestTabCyclesOverATeamArgumentTheRoomCannotComplete(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40).refocus("")
	a = a.withDraft("/team zzznone")
	if a.completionUp() {
		t.Fatal("the room drew a /team menu, so this is not the gated-out case")
	}

	a, _ = pressKey(a, tabKey)
	if a.focus != "s1" {
		t.Errorf("⇥ left the keys on %q, want the next conversation in the ring: the room has no /team menu", a.focus)
	}
}

// The word is the one the cursor is at the end of, which is the menu's own rule:
// with the cursor moved back off it there is no word being typed, and ⇥ is the
// ring's again.
func TestTabCyclesWhenTheCursorIsNotAtTheEndOfTheWord(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40).withDraft("/zzzskil")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyLeft})
	if a.composer().AtEnd() {
		t.Fatal("← left the cursor at the end of the draft, so this asserts nothing")
	}

	a, _ = pressKey(a, tabKey)
	if a.focus != "" {
		t.Errorf("⇥ left the keys on %q with the cursor off the word, want the next conversation in the ring", a.focus)
	}
}

// Only a word being typed keeps ⇥ in the draft: on anything else it is still the
// conversation ring, the one keyboard route back to the room.
func TestTabStillCyclesConversationsOnAnOrdinaryDraft(t *testing.T) {
	for name, draft := range map[string]string{
		"an empty draft":              "",
		"plain text":                  "hello",
		"a finished word":             "/clear ",
		"an argument after a command": "/clear now",
	} {
		t.Run(name, func(t *testing.T) {
			fresh(t)
			a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40).withDraft(draft)
			if a.focus != "s1" {
				t.Fatalf("the fixture is focused on %q, want alex's conversation", a.focus)
			}

			a, _ = pressKey(a, tabKey)
			if a.focus != "" {
				t.Errorf("⇥ left the keys on %q, want the next conversation in the ring (the room)", a.focus)
			}
		})
	}
}
