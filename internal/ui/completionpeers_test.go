package ui

// A conversation's `@` menu offers what Claude Code's own `@` typeahead does: the
// fleet's other live agents, the machine's other Claude sessions, the agent's
// subagent types, and paths. Nothing it offers routes - the DM sends what was
// typed, and claude resolves the mention.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// peerFleet is jade's conversation in a fleet holding every kind of name a `@j`
// could be tempted by: a live peer on a team, a parked one, an ended one, the
// manager, and a session only the park book names. types are jade's subagent
// types; jane has her own, which jade's menu must never offer.
func peerFleet(t *testing.T, dir string, types ...string) App {
	t.Helper()
	fresh(t)
	return dmApp(newRecorder(t), Stream{}, "s1", "jade").withSize(200, 40).applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{
			Running: true,
			Teams:   []string{"jets"},
			Sessions: []rpc.SessionStatus{
				{ID: "s1", Name: "jade", Dir: dir, State: rpc.StateIdle, Agents: types},
				{ID: "s2", Name: "jane", Team: "jets", State: rpc.StateIdle, Agents: []string{"janes-own"}},
				{ID: "s3", Name: "jack", State: rpc.StateParked},
				{ID: "s4", Name: "joy", State: rpc.StateEnded},
				{ID: "s5", Name: "alex", State: rpc.StateIdle},
				{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle},
			},
			Parked: []rpc.SessionStatus{{ID: "p1", Name: "jinx", State: rpc.StateParked}},
		},
	})
}

// peersReply is the daemon's answer to a FramePeers, as it reaches the client.
func peersReply(peers ...core.Peer) rpc.Frame {
	return rpc.Frame{Kind: rpc.FramePeersReply, Peers: &rpc.PeersFrame{Peers: peers}}
}

// typedAsking presses keys through Update and runs each one's command the way
// the loop would, a directory read folded back in, and returns how many
// FramePeers those commands wrote.
func typedAsking(t *testing.T, a App, keys ...tea.KeyMsg) (App, int) {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		a, cmd = pressKey(a, k)
		a = a.looped(t, cmd)
	}
	n := 0
	for _, f := range recorderOf(t, a).taken(t) {
		if f.Kind != rpc.FramePeers {
			continue
		}
		if f.SessionID != "" {
			t.Errorf("the ask names session %q: the listing is the machine's, not an agent's", f.SessionID)
		}
		n++
	}
	return a, n
}

// looped runs a command and folds back what the loop would: a directory read.
func (a App) looped(t *testing.T, cmd tea.Cmd) App {
	t.Helper()
	for _, msg := range drainBatch(cmd) {
		switch m := msg.(type) {
		case errMsg:
			t.Fatalf("a keystroke's write failed: %v", m.Err)
		case pathScanMsg:
			next, more := a.Update(m)
			a = next.(App).looped(t, more)
		}
	}
	return a
}

// runes is text as the keystrokes that type it.
func runes(text string) []tea.KeyMsg {
	keys := make([]tea.KeyMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return keys
}

var backspace = tea.KeyMsg{Type: tea.KeyBackspace}

// A conversation's `@j` offers the fleet's live agents that match: not the
// conversation's own, not a parked or ended one, not the manager, no team and
// no `@all` - the router's live set, less the agent being talked to.
func TestAConversationOffersItsLivePeers(t *testing.T) {
	for draft, want := range map[string][]string{
		"@j": {"@jane"},
		"@a": {"@alex"},
		"@m": nil,
	} {
		t.Run(draft, func(t *testing.T) {
			a := peerFleet(t, "").withDraft(draft)
			if got := a.completion.offers; !slices.Equal(got, want) {
				t.Errorf("%q in jade's conversation offered %q, want %q: only a live agent other than jade "+
					"is a peer claude can message", draft, got, want)
			}
		})
	}
}

// The room's `@` is Wake's routing words and nothing else: a reply's sessions and
// an agent's subagent types are a conversation's, and the room asks for neither.
func TestTheRoomOffersNoOutsideSessionsOrSubagents(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "jade", State: rpc.StateIdle, Agents: []string{"Explore"}},
	).applyFrame(peersReply(core.Peer{Name: "wf-alpha", Dir: "/tmp/wf-a"}))

	a, asked := typedAsking(t, a, runes("@wf")...)
	if asked != 0 {
		t.Errorf("the room's `@wf` asked for the machine's sessions %d times, want none", asked)
	}
	if slices.Contains(a.completion.offers, "@wf-alpha") {
		t.Errorf("the room offered the outside session @wf-alpha: %q - `@name` routes there, and "+
			"core.Resolve knows no session outside the fleet", a.completion.offers)
	}
	fresh(t)
	room := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "jade", State: rpc.StateIdle, Agents: []string{"Explore"}},
	).withDraft("@agent-")
	if room.completion.open() {
		t.Errorf("the room's `@agent-` offered %q, want nothing: a subagent is one agent's, and the "+
			"room addresses the fleet", room.completion.offers)
	}
}

// The machine's other sessions arrive on the reply to the menu's own ask, and
// the reply rebuilds the open menu: each is labelled with its directory, and ⇥
// inserts the bare mention, never the label.
func TestOutsideSessionsFromAReplyAreOfferedWithTheirDirectory(t *testing.T) {
	a, _ := typedAsking(t, peerFleet(t, ""), runes("@wf")...)
	if a.completion.open() {
		t.Fatalf("`@wf` offered %q before any reply, so this asserts nothing about the reply", a.completion.offers)
	}
	a = a.applyFrame(peersReply(
		core.Peer{Name: "wf-alpha", Dir: "/tmp/wf-a"},
		core.Peer{Name: "wf-beta", Dir: "/tmp/wf-b"},
	))

	if got, want := a.completion.offers, []string{"@wf-alpha", "@wf-beta"}; !slices.Equal(got, want) {
		t.Fatalf("after the reply `@wf` offers %q, want %q: the reply must rebuild the open menu", got, want)
	}
	if got, want := a.completion.rowLabel("@wf-alpha", 200), "@wf-alpha (/tmp/wf-a)"; got != want {
		t.Errorf("the outside session is labelled %q, want %q", got, want)
	}
	if drawn := a.completionView(200, a.focus); !strings.Contains(drawn, "@wf-beta (/tmp/wf-b)") {
		t.Errorf("the drawn menu does not label @wf-beta with its directory:\n%s", drawn)
	}
	took, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := took.composer().Value(), "@wf-alpha "; got != want {
		t.Errorf("⇥ inserted %q, want %q: the directory is a label, not part of the mention", got, want)
	}
}

// A directory under the home directory is drawn with `~`, as the status bar
// draws one: the tail is the interesting half, and a screenshot of the menu
// should not carry the operator's name.
func TestAnOutsideSessionsDirectoryIsShortenedUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a := peerFleet(t, "").applyFrame(peersReply(core.Peer{Name: "wf-alpha", Dir: filepath.Join(home, "src", "wf")}))
	a = a.withDraft("@wf")

	if got, want := a.completion.rowLabel("@wf-alpha", 200), "@wf-alpha ("+filepath.Join("~", "src", "wf")+")"; got != want {
		t.Errorf("the outside session is labelled %q, want %q", got, want)
	}
}

// Review Focus 5: an outside session sharing a fleet agent's name is dropped, so
// the fleet agent is offered once and untagged - live, parked, in the park book
// or the manager. jade's own claude name is jade (renamesync keeps the two in
// step), so the listing's own row for jade is never offered to jade. An ended
// agent's name went back to the pool, so a session holding it now is outside;
// and a listing naming one session twice is offered once.
func TestAnOutsideSessionNamedLikeAFleetAgentIsDropped(t *testing.T) {
	a := peerFleet(t, "").applyFrame(peersReply(
		core.Peer{Name: "jane", Dir: "/tmp/elsewhere"},
		core.Peer{Name: "jack", Dir: "/tmp/p"},
		core.Peer{Name: "jinx", Dir: "/tmp/book"},
		core.Peer{Name: "jade", Dir: "/tmp/self"},
		core.Peer{Name: "jalen", Dir: "/tmp/j1"},
		core.Peer{Name: "jalen", Dir: "/tmp/j2"},
		core.Peer{Name: "joy", Dir: "/tmp/joy"},
		core.Peer{Name: core.ManagerName, Dir: "/tmp/m"},
	)).withDraft("@j")

	if got, want := a.completion.offers, []string{"@jane", "@jalen", "@joy"}; !slices.Equal(got, want) {
		t.Errorf("`@j` offered %q, want %q: a name any fleet agent holds is the fleet's", got, want)
	}
	if got := a.completion.rowLabel("@jane", 200); got != "@jane" {
		t.Errorf("the fleet's jane is drawn %q, want it untagged: the outside row of that name was "+
			"dropped, not merged into hers", got)
	}
	if got, want := a.completion.rowLabel("@jalen", 200), "@jalen (/tmp/j1)"; got != want {
		t.Errorf("the twice-listed jalen is drawn %q, want the first listing's %q", got, want)
	}

	manager := peerFleet(t, "").applyFrame(peersReply(core.Peer{Name: core.ManagerName, Dir: "/tmp/m"}))
	if manager = manager.withDraft("@ma"); manager.completion.open() {
		t.Errorf("`@ma` offered %q: the manager is a fleet agent, so claude's listing of it is dropped",
			manager.completion.offers)
	}
}

// The conversation's own agent's subagent types are offered as `@agent-<type>`,
// matched against either spelling and either case. The label says it is an
// agent; ⇥ inserts the mention claude resolves to an Agent call.
func TestSubagentTypesAreOfferedAsAgentMentions(t *testing.T) {
	for draft, want := range map[string][]string{
		"@agent-":    {"@agent-Explore", "@agent-general-purpose", "@agent-Plan"},
		"@Exp":       {"@agent-Explore"},
		"@exp":       {"@agent-Explore"},
		"@AGENT-gen": {"@agent-general-purpose"},
		"@jan":       {"@jane"},
	} {
		t.Run(draft, func(t *testing.T) {
			a := peerFleet(t, "", "Explore", "general-purpose", "Plan").withDraft(draft)
			if got := a.completion.offers; !slices.Equal(got, want) {
				t.Errorf("%q offered %q, want %q: jade's own types, never jane's", draft, got, want)
			}
		})
	}

	a := peerFleet(t, "", "Explore").withDraft("@Exp")
	if got, want := a.completion.rowLabel("@agent-Explore", 200), "@agent-Explore (agent)"; got != want {
		t.Errorf("the subagent is labelled %q, want %q", got, want)
	}
	took, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := took.composer().Value(), "@agent-Explore "; got != want {
		t.Errorf("⇥ inserted %q, want %q: `(agent)` is a label, not part of the mention", got, want)
	}
}

// A bare `@` has no letter to match a name on, so it offers paths only - and it
// is not an opening, so it asks the daemon for nothing.
func TestABareMentionOffersPathsOnlyAndAsksNothing(t *testing.T) {
	dir := workdir(t, "notes.md")
	a, asked := typedAsking(t, peerFleet(t, dir, "Explore"), runes("@")...)
	if asked != 0 {
		t.Errorf("a bare `@` asked for the machine's sessions %d times, want none", asked)
	}
	if got, want := a.completion.offers, []string{"@notes.md"}; !slices.Equal(got, want) {
		t.Errorf("a bare `@` in a conversation offered %q, want the paths alone %q", got, want)
	}
}

// Opening the menu - a conversation's `@` gaining a letter - writes one
// FramePeers. The keystrokes after it write none, nor do a fleet report's or the
// reply's rebuilds; closing it and opening it again writes one more.
func TestOpeningTheMenuAsksForTheMachinesSessionsOnce(t *testing.T) {
	a := peerFleet(t, "")
	a, asked := typedAsking(t, a, runes("@j")...)
	if asked != 1 {
		t.Fatalf("`@j` asked %d times, want once: the letter is what opens a conversation's names", asked)
	}
	if a, asked = typedAsking(t, a, append(runes("an"), backspace)...); asked != 0 {
		t.Errorf("typing on in the same menu asked %d more times, want none: it is one opening", asked)
	}
	a = a.withRoster(rpc.SessionStatus{ID: "s1", Name: "jade", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "jane", State: rpc.StateIdle})
	a = a.applyFrame(peersReply(core.Peer{Name: "jalen", Dir: "/tmp/j"}))
	if a, asked = typedAsking(t, a, runes("e")...); asked != 0 {
		t.Errorf("a keystroke after a report and a reply asked %d times, want none: neither rebuild "+
			"is an opening, so neither may re-arm the ask", asked)
	}
	a, asked = typedAsking(t, a, backspace, backspace, backspace)
	if asked != 0 || a.composer().Value() != "@" {
		t.Fatalf("backing out to %q asked %d times, want `@` and none", a.composer().Value(), asked)
	}
	if _, asked = typedAsking(t, a, runes("j")...); asked != 1 {
		t.Errorf("reopening with `j` asked %d times, want once more: every opening asks", asked)
	}
}

// Another conversation's menu is its own opening, even when its draft already
// held a letter before the keys moved there: the ask belongs to one pane's menu.
func TestAnotherConversationsMenuIsAnotherOpening(t *testing.T) {
	a := peerFleet(t, "").openRight("s2", "jane")
	a, asked := typedAsking(t, a, runes("@k")...)
	if asked != 1 {
		t.Fatalf("jane's `@k` asked %d times, want once", asked)
	}
	a, asked = typedAsking(t, a.refocus("s1"), runes("@j")...)
	if asked != 1 {
		t.Fatalf("jade's `@j` asked %d times, want once", asked)
	}
	if _, asked = typedAsking(t, a.refocus("s2"), runes("i")...); asked != 1 {
		t.Errorf("back in jane's pane, `@ki` asked %d times, want once: jade's opening is not hers", asked)
	}
}

// Only a keystroke asks. A directory read's answer rebuilds the menu, and it is
// not an opening; a menu opened off the keystroke path - a paste, a deleted
// selection - is asked for by the first keystroke that finds it.
func TestADirectoryReadsAnswerDoesNotAsk(t *testing.T) {
	dir := workdir(t, "jot.md")
	a, _ := pressKey(peerFleet(t, dir), runes("@")[0]) // the read is out, unanswered
	a = a.withComposer(a.composer().WithDraft("@j")).recompleted()
	next, cmd := a.Update(scanPaths(dir)())
	if a, asked := typedAsking(t, next.(App).looped(t, cmd)); asked != 0 {
		t.Errorf("a directory read's answer asked for the machine's sessions %d times, want none; offers %q",
			asked, a.completion.offers)
	}
	if _, asked := typedAsking(t, next.(App), runes("o")...); asked != 1 {
		t.Errorf("the first keystroke in a menu nothing had asked for yet asked %d times, want once", asked)
	}
}

// Review Focus 1: with no reply yet - every agent busy, none live, a slow
// one-shot - the menu shows the fleet's peers and the paths, and waits for nothing.
func TestWithNoReplyTheMenuShowsFleetPeersAndPaths(t *testing.T) {
	a := peerFleet(t, workdir(t, "jot.md")).withDraft("@j")
	if got, want := a.completion.offers, []string{"@jane", "@jot.md"}; !slices.Equal(got, want) {
		t.Errorf("`@j` with no reply offered %q, want %q", got, want)
	}
}

// Review Focus 2: a reply replaces the listing whole. An empty one - what the
// daemon answers for a listing it could not parse - leaves no outside rows, and
// none left over from the reply before it.
func TestAReplyReplacesTheListingWhole(t *testing.T) {
	a := peerFleet(t, "").applyFrame(peersReply(core.Peer{Name: "wf-alpha", Dir: "/tmp/a"})).withDraft("@wf")
	if !slices.Contains(a.completion.offers, "@wf-alpha") {
		t.Fatalf("the first reply's session is not offered, so this asserts nothing: %q", a.completion.offers)
	}
	if next := a.applyFrame(peersReply(core.Peer{Name: "wf-beta", Dir: "/tmp/b"})); !slices.Equal(next.completion.offers, []string{"@wf-beta"}) {
		t.Errorf("a second reply left %q, want only its own @wf-beta: replies replace, never merge", next.completion.offers)
	}
	for name, reply := range map[string]rpc.Frame{
		"empty":   peersReply(),
		"no body": {Kind: rpc.FramePeersReply},
	} {
		if next := a.applyFrame(reply); next.completion.open() {
			t.Errorf("an %s reply left %q offered, want nothing: a stale row names a session that "+
				"may be gone", name, next.completion.offers)
		}
	}
}

// The manager's own conversation offers its fleet peers (owner's decision 1) and
// paths, and nothing else: it runs with `--tools ""`, so it has no SendMessage
// and no Agent tool. It reaches a peer by Wake name through its send tool, and it
// can reach neither an outside session nor a subagent - so it offers neither,
// even from a listing another conversation asked for, and asks for none.
func TestTheManagersConversationOffersOnlyItsFleetPeers(t *testing.T) {
	manager := func(t *testing.T) App {
		t.Helper()
		fresh(t)
		return dmApp(newRecorder(t), Stream{}, "m1", core.ManagerName).withSize(200, 40).withRoster(
			rpc.SessionStatus{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle, Agents: []string{"Explore"}},
			rpc.SessionStatus{ID: "s2", Name: "jane", State: rpc.StateIdle},
		).applyFrame(peersReply(core.Peer{Name: "jalen", Dir: "/tmp/j"}))
	}
	a, asked := typedAsking(t, manager(t), runes("@j")...)
	if got, want := a.completion.offers, []string{"@jane"}; !slices.Equal(got, want) {
		t.Errorf("the manager's `@j` offered %q, want its fleet peer alone %q", got, want)
	}
	if asked != 0 {
		t.Errorf("the manager's `@j` asked %d times, want none: it cannot message what the listing names", asked)
	}
	if a, _ = typedAsking(t, manager(t), runes("@agent-")...); a.completion.open() {
		t.Errorf("the manager's `@agent-` offered %q, want nothing: it has no Agent tool", a.completion.offers)
	}
}

// ↵ still sends, and sends what was typed: the menu offers and never routes.
func TestEnterStillSendsOverAConversationsNames(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "jade").withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "jade", State: rpc.StateIdle},
	).applyFrame(peersReply(core.Peer{Name: "wf-alpha", Dir: "/tmp/a"})).withDraft("@wf")
	if !a.completionUp() {
		t.Fatal("the fixture drew no menu, so this asserts nothing")
	}

	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	go func() { _ = runCmdQuietly(cmd) }()
	if f := awaitFrame(t, sent); f.Kind != rpc.FrameSend || f.Text != "@wf" {
		t.Errorf("↵ sent %s %q, want the draft %q as typed", f.Kind, f.Text, "@wf")
	}
}

// A long directory is what the width cuts, never the name ⇥ inserts: a tag
// keeps its room only up to half the row.
func TestALongDirectoryNeverPushesOutTheName(t *testing.T) {
	long := "/tmp/" + strings.Repeat("deep/", 12) + "wf"
	a := peerFleet(t, "").applyFrame(peersReply(core.Peer{Name: "wf-alpha", Dir: long})).withDraft("@wf")

	row := strings.SplitN(a.completion.View(30), "\n", 2)[0]
	if !strings.Contains(row, "@wf-alpha") {
		t.Errorf("at 30 columns the row is %q: the directory pushed out the name the accept inserts", row)
	}
}

// The ask is for a name, and a typed text that cannot begin one is a path: a
// Wake name starts with a letter (daemon/names.go), and none holds a separator.
// So none of these asks, pasted or typed, nor does ⇥ stepping into a directory.
func TestAPathShapedMentionAsksNothing(t *testing.T) {
	paste := func(text string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune(text)} }
	for name, keys := range map[string][]tea.KeyMsg{
		"dot":           runes("@."),
		"slash":         runes("@/"),
		"tilde":         runes("@~"),
		"pasted path":   {paste("@src/")},
		"tab into src/": append(runes("@"), tea.KeyMsg{Type: tea.KeyTab}),
	} {
		t.Run(name, func(t *testing.T) {
			dir := workdir(t)
			if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			a, asked := typedAsking(t, peerFleet(t, dir), keys...)
			if asked != 0 {
				t.Errorf("%q asked for the machine's sessions %d times, want none: it is a path", a.composer().Value(), asked)
			}
			if name == "tab into src/" && a.composer().Value() != "@src/" {
				t.Fatalf("⇥ left %q, want @src/: the fixture did not step into the directory", a.composer().Value())
			}
		})
	}
}

// A name holding whitespace is not offered - an outside session's or a
// subagent type's. The mention ends at the first space claude reads, so ⇥ would
// insert one mention and some prose; and the row collapses whitespace, so it
// would not even be drawn as what it inserts.
func TestANameHoldingWhitespaceIsNotOffered(t *testing.T) {
	spaced := func(t *testing.T) App {
		t.Helper()
		return peerFleet(t, "", "my helper", "my-helper").applyFrame(peersReply(
			core.Peer{Name: "foo bar", Dir: "/tmp/a"},
			core.Peer{Name: "foo\u00a0baz", Dir: "/tmp/b"},
			core.Peer{Name: "foobar", Dir: "/tmp/c"},
		))
	}
	if got, want := spaced(t).withDraft("@f").completion.offers, []string{"@foobar"}; !slices.Equal(got, want) {
		t.Errorf("`@f` offered %q, want %q: a spaced name is not one mention", got, want)
	}
	if got, want := spaced(t).withDraft("@agent-my").completion.offers, []string{"@agent-my-helper"}; !slices.Equal(got, want) {
		t.Errorf("`@agent-my` offered %q, want %q: a spaced type is not one mention", got, want)
	}
}

// ⎋⎋ closes the menu for real: the next `@` is a new opening, and asks.
func TestClearingTheDraftEndsTheOpening(t *testing.T) {
	a, asked := typedAsking(t, peerFleet(t, ""), runes("@j")...)
	if asked != 1 {
		t.Fatalf("`@j` asked %d times, want once", asked)
	}
	esc := tea.KeyMsg{Type: tea.KeyEsc}
	a, _ = typedAsking(t, a, esc, esc)
	if a.composer().Value() != "" {
		t.Fatalf("⎋⎋ left %q, want the draft cleared", a.composer().Value())
	}
	if _, asked = typedAsking(t, a, tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("@wf")}); asked != 1 {
		t.Errorf("a pasted `@wf` after ⎋⎋ asked %d times, want once: a cleared draft is a closed menu", asked)
	}
}

// The fleet's names are lower-case and claude's listing need not be, so the
// fleet wins a name whatever its case - and a listing naming one session twice
// in two cases offers it once.
func TestTheFleetWinsANameWhateverItsCase(t *testing.T) {
	a := peerFleet(t, "").applyFrame(peersReply(
		core.Peer{Name: "Jane", Dir: "/tmp/elsewhere"},
		core.Peer{Name: "Jalen", Dir: "/tmp/j1"},
		core.Peer{Name: "jalen", Dir: "/tmp/j2"},
	)).withDraft("@j")
	if got, want := a.completion.offers, []string{"@jane", "@Jalen"}; !slices.Equal(got, want) {
		t.Errorf("`@j` offered %q, want %q", got, want)
	}
}

// A directory is cut from the left, so its tail - the part naming the project -
// survives, and the name keeps all the row but the parentheses' own room.
func TestADirectoryKeepsItsTailAndTheNameItsWidth(t *testing.T) {
	const name = "a-rather-long-session-name-x"
	a := peerFleet(t, "").applyFrame(peersReply(core.Peer{Name: name, Dir: "/tmp/deep/deep/deep/wf"})).withDraft("@a-")
	want := "@" + name + " (…ep/wf)"
	if got := a.completion.rowLabel("@"+name, 40); got != want {
		t.Errorf("at 40 columns the row is %q, want %q", got, want)
	}
	if drawn := a.completion.View(40); !strings.Contains(drawn, want) {
		t.Errorf("the drawn menu cut the label further:\n%s", drawn)
	}
}

// A rebuild that leaves the draft alone - the peers reply landing while
// somebody walks the menu, a fleet report - keeps the cursor on the offer it was
// on, or clamps where that offer went. ⇥ then takes what was walked to.
func TestTheCursorStaysOnItsOfferAcrossARebuild(t *testing.T) {
	a, _ := typedAsking(t, peerFleet(t, workdir(t, "jot.md", "jump.md")), runes("@j")...)
	if got, want := a.completion.offers, []string{"@jane", "@jot.md", "@jump.md"}; !slices.Equal(got, want) {
		t.Fatalf("the fixture offers %q, want %q", got, want)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN}) // on @jump.md

	a = a.applyFrame(peersReply(core.Peer{Name: "jalen", Dir: "/tmp/j"}))
	if got := a.completion.offers[a.completion.cursor]; got != "@jump.md" {
		t.Errorf("the reply moved the cursor to %q, want it still on @jump.md: offers %q", got, a.completion.offers)
	}
	a = a.withRoster(
		rpc.SessionStatus{ID: "s1", Name: "jade", Dir: a.completionAgent().Cwd, State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "jane", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s6", Name: "jasper", State: rpc.StateIdle},
	)
	if got := a.completion.offers[a.completion.cursor]; got != "@jump.md" {
		t.Errorf("a fleet report moved the cursor to %q, want it still on @jump.md: offers %q", got, a.completion.offers)
	}
	if took, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyTab}); took.composer().Value() != "@jump.md " {
		t.Errorf("⇥ inserted %q, want the walked-to @jump.md", took.composer().Value())
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlP})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlP}) // on @jalen, index 2
	if a.completion.offers[a.completion.cursor] != "@jalen" {
		t.Fatalf("the walk landed on %q, want @jalen: offers %q", a.completion.offers[a.completion.cursor], a.completion.offers)
	}
	a = a.applyFrame(peersReply())
	if got := a.completion.cursor; got != 2 {
		t.Errorf("with @jalen gone the cursor is at %d, want it clamped where it was (2): offers %q", got, a.completion.offers)
	}
}

// A keystroke's menu is a menu for a new draft, and starts at the top as it
// always has: the cursor is held only across a rebuild that left the draft alone.
func TestANewDraftsMenuStartsAtTheTop(t *testing.T) {
	a, _ := typedAsking(t, peerFleet(t, workdir(t, "jolt.md", "jot.md", "jump.md")), runes("@j")...)
	for range 3 {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	}
	if got := a.completion.offers[a.completion.cursor]; got != "@jump.md" {
		t.Fatalf("the walk landed on %q, want @jump.md: offers %q", got, a.completion.offers)
	}
	if a, _ = typedAsking(t, a, runes("o")...); a.completion.cursor != 0 {
		t.Errorf("`@jo` put the cursor at %d, want the top: offers %q", a.completion.cursor, a.completion.offers)
	}
}

// A directory too narrow to show still shows that it was cut - `(…)`, never `()`
// - and a double-width rune at the cut never pushes the label past the row.
func TestADirectoryTagNeverEmptiesOrOverflows(t *testing.T) {
	for _, width := range []int{24, 40} {
		avail := width - lipgloss.Width(cardCursor)
		for name, tc := range map[string]struct {
			offer, dir, prefix string
		}{
			"name of avail-4": {"@" + strings.Repeat("n", avail-5), "/tmp/wf", "@" + strings.Repeat("n", avail-5) + " (…)"},
			"longer name":     {"@" + strings.Repeat("n", avail), "/tmp/wf", ""},
			"wide directory":  {"@wide", "/tmp/" + strings.Repeat("ト", 20), "@wide (…"},
		} {
			t.Run(fmt.Sprintf("%s at %d", name, width), func(t *testing.T) {
				got := completion{tags: map[string]offerTag{tc.offer: {dir: tc.dir}}}.rowLabel(tc.offer, width)
				if w := lipgloss.Width(got); w > avail {
					t.Errorf("the label %q is %d cells in a row of %d", got, w, avail)
				}
				if !strings.HasPrefix(got, tc.prefix) || !strings.HasSuffix(got, ")") {
					t.Errorf("the label is %q, want it to start %q and close its parenthesis", got, tc.prefix)
				}
				if tc.dir == "/tmp/wf" && !strings.HasSuffix(got, " (…)") {
					t.Errorf("the label is %q, want the directory drawn as (…) when there is no room for it", got)
				}
			})
		}
	}
}
