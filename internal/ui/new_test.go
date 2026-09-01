package ui

// `/new`, from the outside: what reaches the daemon, and what the room does
// with the agent when it arrives.

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The room can start an agent, which is the gap docs/goals.md §3 calls the
// largest single one between the founding message and the build: *"Wake can
// manage agents but cannot create or name them from inside itself."*
//
// The frame is asserted field by field rather than by shape, because three of
// them are refusals on the daemon's side and each fails differently. A
// non-UUID id is refused by maySpawn - the reaper's only proof of a process
// group is that id in an argv. A relative directory is refused too, and an
// **absent** one is worse than refused: spawnDir falls back to the daemon's own
// working directory, which is whichever repository forked it, so the agent
// edits the wrong tree and claude persists the transcript under the wrong path.
func TestSlashNewAsksTheDaemonToStartAnAgent(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40)

	m, cmd := typeAndSubmit(a, "/new")
	a = m.(App)
	f := sentFrame(t, a, cmd)

	if f.Kind != rpc.FrameSpawn {
		t.Fatalf("`/new` wrote a %q frame, want %q", f.Kind, rpc.FrameSpawn)
	}
	if _, err := uuid.Parse(f.SessionID); err != nil {
		t.Errorf("the frame's SessionID is %q and is not a UUID (%v): maySpawn refuses anything else, "+
			"because the reaper identifies a process group by finding that id in an argv", f.SessionID, err)
	}
	if f.Text != "" {
		t.Errorf("`/new` with no name asked for the name %q: an empty Text is what tells the daemon to "+
			"draw one from the pool, and the daemon is the only process that can see the whole fleet", f.Text)
	}
	if !filepath.IsAbs(f.Dir) {
		t.Errorf("the frame's Dir is %q. An absent or relative directory is not refused into safety: "+
			"spawnDir falls back to the daemon's own working directory, so the agent runs in whichever "+
			"repository happened to fork it", f.Dir)
	}
	if f.ParentID != "" || f.Role != "" {
		t.Errorf("`/new` asked for a fork or a role (%+v): it starts an ordinary agent and nothing else", f)
	}
	// The wait, which is what makes the arrival below reach anything.
	if _, waiting := a.pendingStarts[f.SessionID]; !waiting {
		t.Errorf("`/new` is not waiting on the id it minted (%d pending): the confirmation is a report "+
			"naming that id, so a client that forgot it opens nothing and says nothing", len(a.pendingStarts))
	}
}

// The two arguments the founding message spells, and the noun it spells them
// around: *"`/new agent(session) in <project dir or by default at root dir>`"*.
func TestSlashNewCarriesTheNameAndTheDirectoryThatWereTyped(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		draft, name, dir string
	}{
		{draft: "/new sydney", name: "sydney"},
		{draft: "/new in " + dir, dir: dir},
		{draft: "/new sydney in " + dir, name: "sydney", dir: dir},
		// The founding message's own spelling. `agent` is the noun it wraps
		// the verb in, not a name, and a build that took it as one would name
		// the agent `agent` on the exact string the product was asked for in.
		{draft: "/new agent in " + dir, dir: dir},
		{draft: "/new session in " + dir, dir: dir},
		// Multiple words are one name joined with a hyphen: `/new john doe`
		// asks for `john-doe`, a token normalizeName accepts and `@john-doe`
		// routes to. There is no way to type a space into a name, by design.
		{draft: "/new john doe", name: "john-doe"},
		{draft: "/new john doe in " + dir, name: "john-doe", dir: dir},
		{draft: "/new one two in " + dir, name: "one-two", dir: dir},
		// A noun is skipped only when it stands alone. `/new agent smith` is a
		// name somebody chose, so the multi-word reading wins over the shortcut.
		{draft: "/new agent smith", name: "agent-smith"},
	} {
		t.Run(tc.draft, func(t *testing.T) {
			fresh(t)
			a := newRoomApp(t).withSize(200, 40)

			m, cmd := typeAndSubmit(a, tc.draft)
			f := sentFrame(t, m.(App), cmd)

			if f.Text != tc.name {
				t.Errorf("%q asked the daemon for the name %q, want %q", tc.draft, f.Text, tc.name)
			}
			if tc.dir != "" && f.Dir != tc.dir {
				t.Errorf("%q asked for the directory %q, want %q", tc.draft, f.Dir, tc.dir)
			}
		})
	}
}

// A directory that is not a directory anybody typed is a refusal, not a guess.
//
// `/new x in` is the shape that matters: a trailing `in` with nothing after it
// is a sentence somebody started and did not finish, and the two available
// wrong answers are both silent - start in the default directory, which is the
// one thing they said they did not want, or send `in` as the name.
func TestSlashNewRefusesAnUnfinishedDirectory(t *testing.T) {
	for _, draft := range []string{"/new in", "/new sydney in", "/new one two in"} {
		t.Run(draft, func(t *testing.T) {
			fresh(t)
			a := newRoomApp(t).withSize(200, 40)

			m, cmd := typeAndSubmit(a, draft)
			if cmd != nil {
				t.Fatalf("%q started something anyway: %+v", draft, sentFrames(t, m.(App), cmd))
			}
			if !strings.Contains(shown(m.(App)), newUsage) {
				t.Errorf("%q was refused without saying what the command takes:\n%s", draft, shown(m.(App)))
			}
		})
	}
}

// The payoff: after the daemon confirms, the room's draft names the agent that
// just started - `@sydney ` - so the operator can message it at once. `/new`
// opens no pane: it has no conversation to open, and replacing whatever pane
// was on screen for one that has said nothing yet is the wrong trade at fleet
// scale. The operator's own focus - on alex's conversation here - must not
// move, and neither must the grid or the ring ⇥ walks.
//
// The sentence is not the fork's. A fork is a snapshot of somebody else's
// conversation and says so; a fresh agent has no conversation to be a snapshot
// of, and telling an operator that nothing the parent does next reaches it
// would be a claim about a parent that does not exist.
func TestTheNewAgentPrefillsTheRoomsMentionInsteadOfOpeningAPane(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	mustBeALiveConversation(t, a)
	beforeGrid, beforeOrder := a.grid, slices.Clone(a.dmOrder)

	m, cmd := typeAndSubmit(a, "/new")
	a = m.(App)
	go func() { _ = runCmdQuietly(cmd) }()
	id := awaitFrame(t, sent).SessionID

	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: id, Name: "sydney", Label: "dev-5748", State: rpc.StateIdle},
	}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})

	if a.focus != "s1" {
		t.Errorf("a fresh /new spawn moved the keys from %q to %q: it opens no pane, so the operator's "+
			"own focus must not move", "s1", a.focus)
	}
	if _, held := a.dms[id]; held {
		t.Errorf("a fresh /new spawn opened a DM for %q: it drafts a mention instead", id)
	}
	if !reflect.DeepEqual(a.grid, beforeGrid) {
		t.Errorf("a fresh /new spawn changed the grid from %+v to %+v", beforeGrid, a.grid)
	}
	if !slices.Equal(a.dmOrder, beforeOrder) {
		t.Errorf("a fresh /new spawn changed the ring ⇥ walks from %v to %v", beforeOrder, a.dmOrder)
	}
	if len(a.pendingStarts) != 0 {
		t.Errorf("%d starts are still pending after the agent arrived: the next session given that id "+
			"would steal the draft", len(a.pendingStarts))
	}
	if want, got := "@sydney ", a.room.Composer().Value(); got != want {
		t.Errorf("the room's draft is %q after a fresh spawn arrived, want %q", got, want)
	}
	if a.room.focus != id || a.room.focusName != "sydney" {
		t.Errorf("the room did not narrow to the agent it just drafted a mention to (focus=%q, name=%q)",
			a.room.focus, a.room.focusName)
	}
	got := shown(a)
	if !strings.Contains(got, "sydney") || !strings.Contains(got, "dev-5748") {
		t.Errorf("nothing says which agent started, or where:\n%s", got)
	}
	if strings.Contains(got, "fork") || strings.Contains(got, "snapshot") {
		t.Errorf("a fresh agent was announced as a fork:\n%s", got)
	}
}

// A room draft already in progress survives a fresh spawn's arrival
// untouched. WithDraft replaces rather than inserts, and a spawn takes
// seconds - long enough to start typing a room message before the
// confirmation lands - so draftMention gates itself on an empty draft rather
// than silently discarding whatever the operator was writing.
func TestAFreshSpawnLeavesAnInProgressRoomDraftAlone(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("alex")

	m, cmd := typeAndSubmit(a, "/new")
	a = m.(App)
	id := sentFrame(t, a, cmd).SessionID

	// The room's own draft, typed while the spawn is still in flight - through
	// Update, the real key path, so this is what the operator's keystrokes
	// would actually leave in the box.
	const inProgress = "do not clobber this draft"
	a = a.withDraft(inProgress)

	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: id, Name: "sydney", Label: "dev-5748", State: rpc.StateIdle},
	}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})

	if want, got := inProgress, a.room.Composer().Value(); got != want {
		t.Errorf("a fresh spawn's arrival changed the room's in-progress draft: got %q, want %q", got, want)
	}
	if _, held := a.dms[id]; held {
		t.Errorf("a fresh /new spawn opened a DM for %q even with a draft in progress", id)
	}
	if len(a.pendingStarts) != 0 {
		t.Errorf("%d starts are still pending after the agent arrived", len(a.pendingStarts))
	}
}

// A refusal is addressed to the id this client minted, so it is what stops the
// client waiting. Without it a `/new` the daemon refused - a name already
// taken, a directory that is not there - leaves the wait set, and the next
// unrelated session given that id steals the pane.
func TestARefusedNewAgentStopsTheClientWaitingAndSaysWhy(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40)

	m, cmd := typeAndSubmit(a, "/new sydney")
	a = m.(App)
	id := sentFrame(t, a, cmd).SessionID

	const why = `a live session is already called "sydney"`
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameError, SessionID: id, Text: why})

	if len(a.pendingStarts) != 0 {
		t.Errorf("the refusal left %d starts pending", len(a.pendingStarts))
	}
	if notice.Count(why) != 1 {
		t.Errorf("the daemon's refusal was not reported: it is the only thing that says why nothing " +
			"started, and the client asked for a name the daemon owns")
	}
}

// An error about a *different* session leaves the wait alone. One unrelated
// crash while an agent is starting would otherwise cancel it, and the agent's
// arrival would then draft nothing at all.
func TestAnErrorAboutAnotherSessionLeavesTheNewAgentComing(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("alex")

	m, cmd := typeAndSubmit(a, "/new")
	a = m.(App)
	id := sentFrame(t, a, cmd).SessionID

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameError, SessionID: "s1", Text: "exit status 1"})
	if _, waiting := a.pendingStarts[id]; !waiting {
		t.Fatal("an error about another agent cancelled the wait for the new one")
	}

	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: id, Name: "maya", State: rpc.StateIdle}}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	if want, got := "@maya ", a.room.Composer().Value(); got != want {
		t.Errorf("the agent arrived and the room's draft is %q, want %q", got, want)
	}
}

// `/new` costs one command, which is the rule App.write exists for: bubbletea
// runs every tea.Cmd on its own goroutine and rpc's write lock is process-wide.
// A spawn is the command in this package most likely to grow a second write
// beside it - a spawn, and then a status request to find out what it was
// called - and the answer to that is the fleet report the daemon already sends.
//
// That it is one *frame* is asserted by sentFrame above, which fatals on any
// other count. It is not asserted here as well: commandCount runs the command a
// second time, so a test that did both would be counting two runs of one write.
func TestSlashNewCostsOneCommand(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40)

	_, cmd := typeAndSubmit(a, "/new sydney")
	if n := commandCount(cmd); n != 1 {
		t.Errorf("`/new` cost %d commands, want 1", n)
	}
}

// And it is typed into a conversation as readily as into the room. `/new` is
// addressed to Wake, so where the keys happen to be does not change what it
// does - including which directory the agent starts in, which is `wake`'s own
// and not the focused agent's. A default that followed the focus would be a
// hidden mode: the same command, typed in two panes, starting agents in two
// repositories with nothing on screen saying so.
func TestSlashNewFromAConversationStartsAnAgentTheSameWay(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	mustBeALiveConversation(t, a)

	roomDir := sentFrameDir(t, newRoomApp(t).withSize(200, 40))

	_, cmd := typeAndSubmit(a, "/new")
	go func() { _ = runCmdQuietly(cmd) }()
	f := awaitFrame(t, sent)

	if f.Kind != rpc.FrameSpawn {
		t.Fatalf("`/new` in a conversation wrote a %q frame, want %q", f.Kind, rpc.FrameSpawn)
	}
	if f.Dir != roomDir {
		t.Errorf("`/new` in a conversation starts an agent in %q and in the room in %q: one command "+
			"with two answers, decided by which pane has the keys", f.Dir, roomDir)
	}
}

// sentFrameDir is the directory one `/new` asks for, so a test can compare two
// of them without spelling the answer.
func sentFrameDir(t *testing.T, a App) string {
	t.Helper()
	m, cmd := typeAndSubmit(a, "/new")
	return sentFrame(t, m.(App), cmd).Dir
}

// `~` is expanded here, and it is the arm the founding message's own example
// would take: *"`/new agent(session) in <project dir or by default at root
// dir>`"* is typed by somebody who types `~/project` everywhere else.
//
// A composer is not a shell, so nothing expands it on the way; and the daemon
// refuses a path that is not absolute rather than resolving one, because it
// would resolve against the *daemon's* directory - one process for the whole
// machine, started from whichever repository forked it. So the expansion has to
// happen on the side that knows what the operator meant.
//
// Asserted against os.UserHomeDir rather than against a spelled-out path: the
// claim is "the same directory the shell would have meant", and a literal would
// be this test agreeing with itself.
func TestSlashNewExpandsTheHomeShorthandTheWayAShellWould(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	for _, tc := range []struct{ typed, want string }{
		{typed: "~", want: home},
		{typed: "~/project", want: filepath.Join(home, "project")},
		{typed: "~/a b/c", want: filepath.Join(home, "a b", "c")},
	} {
		t.Run(tc.typed, func(t *testing.T) {
			fresh(t)
			a := newRoomApp(t).withSize(200, 40)

			m, cmd := typeAndSubmit(a, "/new sydney in "+tc.typed)
			f := sentFrame(t, m.(App), cmd)

			if f.Dir != tc.want {
				t.Errorf("`in %s` asked for %q, want %q", tc.typed, f.Dir, tc.want)
			}
			if !filepath.IsAbs(f.Dir) {
				t.Errorf("%q is not absolute, and the daemon refuses one that is not", f.Dir)
			}
		})
	}
	// And a name that merely begins with the shorthand is a path, not a home:
	// `~alex` is another user's home to a shell and this build does not resolve
	// one, so it stays relative to where wake is and the daemon is handed
	// something absolute either way.
	fresh(t)
	a := newRoomApp(t).withSize(200, 40)
	m, cmd := typeAndSubmit(a, "/new in ~alex")
	if f := sentFrame(t, m.(App), cmd); !filepath.IsAbs(f.Dir) || strings.HasPrefix(filepath.Base(f.Dir), home) {
		t.Errorf("`in ~alex` asked for %q, which is neither absolute nor left alone", f.Dir)
	}
}
