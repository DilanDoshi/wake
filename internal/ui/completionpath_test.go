package ui

// `@` is overloaded exactly as it is in Claude Code: a live session name wins,
// and anything else is a file path.

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The overload, as an ordering: a name that resolves is a name, and the same
// characters are a path only when nobody answers to them.
func TestAMentionOffersLiveNamesBeforePaths(t *testing.T) {
	dir := workdir(t, "alexander.md", "zebra.txt")
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	).withDraft("@alex")

	got := a.completion.offers
	if len(got) < 2 {
		t.Fatalf("`@alex` offered %q, want the agent and the file that share the prefix", got)
	}
	if got[0] != agentPrefix+"alex" {
		t.Errorf("`@alex` offers %q first, want %q: a live session name wins, which is the rule the "+
			"router already keeps", got[0], agentPrefix+"alex")
	}
	if !slices.Contains(got, agentPrefix+"alexander.md") {
		t.Errorf("`@alex` offers %q and not the file beside it: anything that is not a name is a path", got)
	}
}

// Paths are relative to the session the draft would reach, because that is the
// directory the agent resolves the reference in.
func TestPathsAreRelativeToTheTargetSessionsDirectory(t *testing.T) {
	mine := workdir(t, "mine.md")
	theirs := workdir(t, "theirs.md")
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: mine, State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "sydney", Dir: theirs, State: rpc.StateIdle},
	).withDraft("@")

	got := a.completion.offers
	if !slices.Contains(got, agentPrefix+"mine.md") {
		t.Errorf("a conversation with alex offers %q, want the file in alex's own directory", got)
	}
	if slices.Contains(got, agentPrefix+"theirs.md") {
		t.Errorf("a conversation with alex offers %q, which is in another session's directory", got)
	}
}

// The room's `@` path half follows the addressed agent too: `@sydney @` offers
// files from sydney's directory, not whichever agent the roster cursor rests on,
// because that is the directory the reference resolves in. This is
// TestPathsAreRelativeToTheTargetSessionsDirectory's rule reaching the room
// through the same completionAgent that fixed the command half.
func TestTheRoomsPathHalfFollowsTheAddressedAgent(t *testing.T) {
	mine := workdir(t, "mine.md")
	theirs := workdir(t, "theirs.md")
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: mine, State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "sydney", Dir: theirs, State: rpc.StateIdle},
	)
	a = pick(a, "s1") // cursor on alex, not the addressed sydney
	a = a.withDraft("@sydney @")

	got := a.completion.offers
	if !slices.Contains(got, agentPrefix+"theirs.md") {
		t.Errorf("`@sydney @` offered %q, want the file in sydney's directory: the draft addresses "+
			"sydney, so `@file` resolves there", got)
	}
	if slices.Contains(got, agentPrefix+"mine.md") {
		t.Errorf("`@sydney @` offered %q, which is in the cursor's directory, not the addressed agent's", got)
	}
}

// A session Wake knows no directory for offers names and nothing else, rather
// than paths relative to whatever the TUI happens to be running in.
func TestASessionWithNoDirectoryOffersNoPaths(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withAgents("alex").withSize(200, 40).withDraft("@")
	want := []string{agentPrefix + "alex", agentPrefix + core.BroadcastName}
	if got := a.completion.offers; !slices.Equal(got, want) {
		t.Errorf("a session with no directory offered %q, want %q: anything else can only have come "+
			"from whatever directory the TUI happens to be running in", got, want)
	}
}

// A DM offers paths and no names. `@name` is Wake's routing and the room is
// where it routes; a DM sends what was typed verbatim, so a name accepted there
// is one claude's own CLI reads as a file reference.
func TestAConversationOffersPathsAndNotNames(t *testing.T) {
	dir := workdir(t, "alexander.md")
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	).withDraft("@alex")

	got := a.completion.offers
	if slices.Contains(got, agentPrefix+"alex") {
		t.Errorf("a conversation offered the agent's own name: %q", got)
	}
	if !slices.Contains(got, agentPrefix+"alexander.md") {
		t.Errorf("a conversation offered %q, want the path that is what `@` means to the agent", got)
	}
}

// One directory, never a walk. `@` at a repository root must not cost a
// recursive read on a keystroke.
func TestThePathScanReadsOneDirectoryAndNeverDescends(t *testing.T) {
	dir := workdir(t, "top.md")
	if err := os.MkdirAll(filepath.Join(dir, "inner"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inner", "buried.md"), nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	).withDraft("@")

	for _, offer := range a.completion.offers {
		if strings.Contains(offer, "buried") {
			t.Errorf("a bare `@` offered %q: the scan descended, which is a recursive read per "+
				"keystroke in a repository", offer)
		}
	}
	if !slices.Contains(a.completion.offers, agentPrefix+"inner"+string(os.PathSeparator)) {
		t.Errorf("a bare `@` offered %q and not the directory itself, so there is no way to reach "+
			"what is inside it", a.completion.offers)
	}
}

// Descending is what ⇥ on a directory does, and it leaves the menu up: a
// directory is not a finished reference.
func TestAcceptingADirectoryOpensIt(t *testing.T) {
	dir := workdir(t)
	if err := os.MkdirAll(filepath.Join(dir, "inner"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inner", "buried.md"), nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	).withDraft("@inn")

	// The accept is what asks for the new directory, so the read it hands back
	// is answered here the way Bubble Tea answers it.
	next, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	next = next.scanned()
	want := agentPrefix + "inner" + string(os.PathSeparator)
	if got := next.composer().Value(); got != want {
		t.Fatalf("⇥ over `@inn` left the draft %q, want %q with no trailing space: a directory is "+
			"not finished", got, want)
	}
	if !slices.Contains(next.completion.offers, want+"buried.md") {
		t.Errorf("stepping into a directory offered %q, want what is in it", next.completion.offers)
	}
}

// A dotfile is offered when it is asked for and not before. `@` at a repository
// root would otherwise open on .git.
func TestADotfileIsOfferedOnlyWhenAsked(t *testing.T) {
	dir := workdir(t, ".env", "readme.md")
	// A fresh model per draft: composers share one text area by pointer, so two
	// drafts typed into one App are one accumulating draft.
	offers := func(draft string) []string {
		fresh(t)
		return newRoomApp(t).withSize(200, 40).withRoster(
			rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
		).withDraft(draft).completion.offers
	}

	if got := offers(agentPrefix); slices.Contains(got, agentPrefix+".env") {
		t.Errorf("a bare `@` offered a dotfile: %q", got)
	}
	if got := offers(agentPrefix + "."); !slices.Contains(got, agentPrefix+".env") {
		t.Errorf("`@.` offered %q, want the dotfile it names", got)
	}
}

// The read is bounded, which is the whole reason a TUI may do it on a
// keystroke: a directory nobody bounded costs the same as a small one.
func TestThePathScanIsBoundedByTheNumberOfEntriesItReads(t *testing.T) {
	dir := workdir(t)
	for i := range pathScanMax + 64 {
		if err := os.WriteFile(filepath.Join(dir, "f"+strconv.Itoa(i)), nil, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	).withDraft("@f")

	if got := len(a.completion.offers); got > completionRows {
		t.Errorf("the menu drew %d rows over a directory of %d files, want at most %d",
			got, pathScanMax+64, completionRows)
	}
	if a.completion.more == 0 {
		t.Error("a directory of thousands matched and the menu said nothing was left out")
	}
}

// One read at a time. A directory that never answers must cost one goroutine
// rather than one per character typed into it - and the read that lands is what
// starts the one the draft has moved on to.
func TestOneDirectoryReadIsInFlightAtATime(t *testing.T) {
	dir := workdir(t)
	if err := os.MkdirAll(filepath.Join(dir, "inner"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inner", "buried.md"), nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	)

	// Typed without answering anything: the `@` asks for the session's own
	// directory and the `/` moves the menu to another one before it lands.
	var m tea.Model = a
	for _, r := range agentPrefix + "inner" + string(os.PathSeparator) {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	held := m.(App)
	if got := held.completion.paths.out; got != dir {
		t.Errorf("the read out is of %q, want %q: a second one started while the first had not "+
			"answered", got, dir)
	}

	if got := held.scanned().completion.offers; !slices.Contains(got, agentPrefix+"inner"+string(os.PathSeparator)+"buried.md") {
		t.Errorf("answering the first read left the menu offering %q: the directory the draft moved "+
			"to while it was out is never asked for", got)
	}
}

// A read is tagged with the directory it was of, so one that outlived the menu
// that asked for it is dropped rather than folded into whatever is on screen.
func TestAReadThatOutlivedItsMenuIsDropped(t *testing.T) {
	mine, theirs := workdir(t, "mine.md"), workdir(t, "theirs.md")
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: mine, State: rpc.StateIdle},
	).withDraft(agentPrefix)
	if !slices.Contains(a.completion.offers, agentPrefix+"mine.md") {
		t.Fatalf("the fixture offers %q, so this asserts nothing about replacing them", a.completion.offers)
	}

	late, _ := a.Update(pathScanMsg{dir: theirs, entries: []pathEntry{{name: "theirs.md"}}})
	if got := late.(App).completion.offers; slices.Contains(got, agentPrefix+"theirs.md") {
		t.Errorf("a read of a directory this menu is not waiting on landed in it: %q", got)
	}
}

// A path naming a directory that does not exist offers nothing and reports
// nothing. Most keystrokes of a path being typed name one, so a notice row here
// would be one line per character.
func TestAPathThatNamesNoDirectoryIsSilent(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: workdir(t, "readme.md"), State: rpc.StateIdle},
	).withDraft("@nowhere" + string(os.PathSeparator) + "at-all")

	if got := a.completion.offers; len(got) != 0 {
		t.Errorf("a path under a directory that does not exist offered %q", got)
	}
	if n, ok := notice.Latest(); ok {
		t.Errorf("it reported %q on the notice row: a failed read here is one per keystroke", n.String())
	}
}

// workdir is a session's directory with files in it.
func workdir(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}
