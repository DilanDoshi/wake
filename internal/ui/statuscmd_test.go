package ui

// `/status`: one session's own facts in a read-only menu over the composer.
// Headless claude refuses the command (testdata/stream/bare-status.jsonl), so
// Wake draws what it already holds - and writes nothing, since a local command
// sent to a session stays in its context.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/version"
)

const statusID = "a11a0000-0000-4000-8000-00000000a11a"

// statusReport is a fleet holding alex, its facts filled in, and the manager.
func statusReport() rpc.Status {
	return rpc.Status{Running: true, ManagerClaude: "manager-harbor", Sessions: []rpc.SessionStatus{
		{ID: statusID, Name: "alex", State: rpc.StateIdle, Cwd: "/tmp/harbor/api", Team: "api",
			Effort: core.EffortHigh, ConfirmedModel: "Opus 5", ContextTokens: 50_000, ContextWindow: 200_000},
		{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle, Cwd: "/tmp/harbor"},
	}}
}

// statusIn opens /status in id's conversation and returns what it wrote.
func statusIn(t *testing.T, id, name string, st rpc.Status) (App, []rpc.Frame) {
	t.Helper()
	fresh(t)
	rec := newRecorder(t)
	a := dmApp(rec, Stream{}, id, name).withSize(140, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	rec.taken(t)
	m, cmd := typeAndSubmit(a, statusVerb)
	drainBatch(cmd)
	return m.(App), rec.taken(t)
}

// The conversation's own facts, every one Wake already holds, and nothing
// written: not to claude, whose context a local command would enter, and not
// to the daemon, which has nothing to add.
func TestSlashStatusShowsTheConversationsFactsAndWritesNothing(t *testing.T) {
	a, wrote := statusIn(t, statusID, "alex", statusReport())

	if len(wrote) != 0 {
		t.Errorf("/status wrote %v; it is drawn from what the window holds", sentKinds(wrote))
	}
	out := shown(a)
	for _, want := range []string{"alex", statusID, "/tmp/harbor/api", "Opus 5", core.EffortHigh, ctxLabel + "75%", "api", "v" + version.Version} {
		if !strings.Contains(out, want) {
			t.Errorf("/status does not show %q:\n%s", want, out)
		}
	}
	if a.composer().Value() != "" {
		t.Errorf("the command stayed in the draft: %q", a.composer().Value())
	}
}

// The manager is a session like any other, and its status also says what every
// Claude session calls it.
func TestTheManagersStatusNamesItAsClaudeSessionsKnowIt(t *testing.T) {
	a, _ := statusIn(t, "m1", core.ManagerName, statusReport())
	if out := shown(a); !strings.Contains(out, "manager-harbor") {
		t.Errorf("the manager's /status does not name it as claude knows it:\n%s", out)
	}
	if b, _ := statusIn(t, statusID, "alex", statusReport()); strings.Contains(shown(b), "manager-harbor") {
		t.Error("an ordinary agent's /status names the manager's claude name")
	}
}

// A daemon on another build is the fact an operator needs from a version line,
// so it is named only when it differs.
func TestStatusNamesTheDaemonsBuildOnlyWhenItDiffers(t *testing.T) {
	st := statusReport()
	st.Build = "0.0.1-elsewhere"
	if a, _ := statusIn(t, statusID, "alex", st); !strings.Contains(shown(a), st.Build) {
		t.Errorf("a daemon on another build is not named:\n%s", shown(a))
	}
	st.Build = version.Build()
	if a, _ := statusIn(t, statusID, "alex", st); strings.Contains(shown(a), "daemon on") {
		t.Errorf("a daemon on this build is named as if it differed:\n%s", shown(a))
	}
}

// Read-only: any key closes it and reaches nothing behind it.
func TestAnyKeyClosesTheStatusAndReachesNothing(t *testing.T) {
	for _, k := range []tea.KeyMsg{keyEsc, keyEnter, {Type: tea.KeyRunes, Runes: []rune{'x'}}} {
		a, _ := statusIn(t, statusID, "alex", statusReport())
		a = pressMenu(a, k)
		if a.statusUI.Open() || strings.Contains(shown(a), statusID) {
			t.Errorf("%q left /status open", k.String())
		}
		if a.composer().Value() != "" {
			t.Errorf("%q reached the draft under /status: %q", k.String(), a.composer().Value())
		}
	}
}

// From the room it is aimed with @who, as /mcp is; with nobody picked, a bare
// /status says how.
func TestTheRoomAimsStatusWithAMention(t *testing.T) {
	fresh(t)
	st := statusReport()
	a := NewRoomApp(newRecorder(t), Stream{}, nil).withSize(140, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})

	m, cmd := typeAndSubmit(a, "@alex "+statusVerb)
	drainBatch(cmd)
	if !strings.Contains(shown(m), statusID) {
		t.Errorf("@alex /status did not show alex's status in the room:\n%s", shown(m))
	}

	// With nobody in the roster to pick, a bare /status in the room says how.
	empty := NewRoomApp(newRecorder(t), Stream{}, nil).withSize(140, 40)
	m, cmd = typeAndSubmit(empty, statusVerb)
	drainBatch(cmd)
	if m.(App).statusUI.Open() || !strings.Contains(latestNotice(t), "whose status") {
		t.Errorf("a bare /status in an empty room opened something, or did not say how (%q)", latestNotice(t))
	}
}

// The view belongs to the pane that opened it: moving the keys away closes it,
// rather than leaving it to come back over whatever that pane draws next.
func TestStatusClosesWhenTheKeysLeaveItsPane(t *testing.T) {
	a, _ := statusIn(t, statusID, "alex", statusReport())
	if !a.statusUI.Open() {
		t.Fatal("/status did not open")
	}
	if a = a.refocus(""); a.statusUI.Open() {
		t.Error("/status stayed open after the keys left its conversation")
	}
}
