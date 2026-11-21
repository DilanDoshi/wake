package ui

// `/fork [@who]`: the fork ⌃F wrote until ⌃F became the jump picker. The frame, the
// wait on the id it minted and the two sentences are fork_test.go's; what is held
// here is the door - which agent each form names, and what refuses.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// forkAsk is a bare `/fork` asked of forkAgent directly: the model and command it
// came back with. The door itself - Enter, submit, slash - is typeCommand's.
func forkAsk(a App) (tea.Model, tea.Cmd) { return a.forkAgent("") }

// typeCommand types a draft into whatever holds the keys and sends it, the way a
// person does, and returns what came back.
func typeCommand(a App, draft string) (App, tea.Cmd) {
	a = a.withComposer(a.composer().WithDraft(draft))
	return pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
}

// forked is the parents of every fork frame a command wrote.
func forked(t *testing.T, a App, cmd tea.Cmd) []string {
	t.Helper()
	if cmd == nil {
		return nil
	}
	var parents []string
	for _, f := range writtenFrames(t, a, cmd) {
		if f.Kind == rpc.FrameFork {
			parents = append(parents, f.ParentID)
		}
	}
	return parents
}

func forkFleet(t *testing.T) App {
	t.Helper()
	return newRoomApp(t).withAgents("alex", "sydney", "robin").withSize(160, 30)
}

func TestSlashForkTypedInAConversationForksThatConversation(t *testing.T) {
	a := forkFleet(t).openDMWith("s2", "sydney").opened(t)
	a, cmd := typeCommand(a, "/fork")
	if got := forked(t, a, cmd); len(got) != 1 || got[0] != "s2" {
		t.Errorf("/fork in sydney's conversation forked %v, want [s2]", got)
	}
	if v := a.composer().Value(); v != "" {
		t.Errorf("the draft was left as %q: a command is not a message", v)
	}
}

// The conversation with the keys is the target even when the roster's cursor sits
// on another agent: a typed command must not branch an agent it was not typed to.
func TestBareSlashForkInAConversationIgnoresTheRostersCursor(t *testing.T) {
	a := forkFleet(t).openDMWith("s2", "sydney").opened(t)
	a.layout.ShowRoster = true
	a.roster = Roster{Selected: "s3"}
	a, cmd := typeCommand(a, "/fork")
	if got := forked(t, a, cmd); len(got) != 1 || got[0] != "s2" {
		t.Errorf("/fork typed in sydney's conversation with robin picked forked %v, want [s2]", got)
	}
}

// From the room a bare /fork is the roster's pick, as /status's is, and with
// nothing picked it asks which agent rather than guessing the first.
func TestBareSlashForkInTheRoomForksTheRostersPickAndNeverGuesses(t *testing.T) {
	a := forkFleet(t)
	a.layout.ShowRoster = true
	a.roster = Roster{Selected: "s3"}
	a, cmd := typeCommand(a, "/fork")
	if got := forked(t, a, cmd); len(got) != 1 || got[0] != "s3" {
		t.Errorf("/fork with robin picked forked %v, want [s3]", got)
	}
}

func TestBareSlashForkInTheRoomWithNothingPickedAsksWhichAgent(t *testing.T) {
	a := forkFleet(t)
	a.layout.ShowRoster = true
	notice.Reset()
	a, cmd := typeCommand(a, "/fork")
	if got := forked(t, a, cmd); len(got) != 0 {
		t.Errorf("/fork with nothing picked forked %v, want none", got)
	}
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), noForkTarget) {
		t.Errorf("it said %q, want %q", n.String(), noForkTarget)
	}
}

func TestSlashForkAtWhoForksThatAgentWhateverIsPicked(t *testing.T) {
	a := forkFleet(t)
	a.roster = Roster{Selected: "s3"}
	a, cmd := typeCommand(a, "/fork @sydney")
	if got := forked(t, a, cmd); len(got) != 1 || got[0] != "s2" {
		t.Errorf("/fork @sydney forked %v, want [s2]", got)
	}
}

// The mention form is the same command, aimed from the room.
func TestAMentionedSlashForkAimsFromTheRoom(t *testing.T) {
	a := forkFleet(t)
	a.roster = Roster{Selected: "s3"}
	a, cmd := typeCommand(a, "@sydney /fork")
	if got := forked(t, a, cmd); len(got) != 1 || got[0] != "s2" {
		t.Errorf("@sydney /fork forked %v, want [s2]", got)
	}
}

func TestSlashForkRefusesWhatItCannotName(t *testing.T) {
	for _, tc := range []struct{ draft, says string }{
		{"/fork @nobody", noSuchAgent},
		{"/fork sydney", forkUsage},
		{"/fork @alex @robin", forkUsage},
	} {
		a := forkFleet(t)
		notice.Reset()
		a, cmd := typeCommand(a, tc.draft)
		if got := forked(t, a, cmd); len(got) != 0 {
			t.Errorf("%q forked %v", tc.draft, got)
		}
		if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), tc.says) {
			t.Errorf("%q said %q, want it to say %q", tc.draft, n.String(), tc.says)
		}
	}
}

func TestSlashForkRefusesATeamAndAnEmptyFleet(t *testing.T) {
	a := forkFleet(t)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true,
		Teams: []string{"web"}, Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", Team: "web"}, {ID: "s2", Name: "sydney", Team: "web"}}}})
	for _, draft := range []string{"/fork @web", "@web /fork"} {
		a2, cmd := typeCommand(a, draft)
		if got := forked(t, a2, cmd); len(got) != 0 {
			t.Errorf("%q forked %v: a team is refused", draft, got)
		}
	}
	empty, cmd := typeCommand(newRoomApp(t).withSize(160, 30), "/fork")
	if got := forked(t, empty, cmd); len(got) != 0 {
		t.Errorf("/fork over nobody forked %v", got)
	}
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), noForkTarget) {
		t.Errorf("over nobody it said %q, want %q", n.String(), noForkTarget)
	}
}

// ⌃F is not fork any more, in the room or in a conversation: it writes nothing.
func TestCtrlFNoLongerForks(t *testing.T) {
	for _, a := range []App{forkFleet(t), forkFleet(t).openDMWith("s2", "sydney").opened(t)} {
		got, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlF})
		if f := forked(t, got, cmd); len(f) != 0 {
			t.Errorf("⌃F forked %v", f)
		}
	}
}
