package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Dilan's 2026-10-25 ruling: a `!cmd`'s output reaches the agent it was typed
// to, as Claude Code's own `!` does, and the pane still draws it.

func TestABangsOutputReachesTheAgentItWasTypedTo(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(80, 24)
	a = loopEnter(a.withDraft("!echo bang-marker-7"))

	frames := recorderOf(t, a).taken(t)
	if len(frames) != 1 {
		t.Fatalf("a bang wrote %d frames, want the one shell run: %+v", len(frames), frames)
	}
	f := frames[0]
	if f.Kind != rpc.FrameSend || f.SessionID != "s1" || f.Text != "" || f.Shell == nil {
		t.Fatalf("a bang wrote %v to %q text %q shell %v, want a shell run to s1 and no text", f.Kind, f.SessionID, f.Text, f.Shell)
	}
	if f.Shell.Command != "echo bang-marker-7" || strings.TrimSpace(f.Shell.Output) != "bang-marker-7" {
		t.Errorf("the shell run carried %+v, want the command and what it printed", *f.Shell)
	}
	// Stamped, so a turn it opens behind a busy one is owed like any send's.
	if f.MessageID == "" {
		t.Error("the shell run is unstamped: a turn it opens behind another would read idle")
	}
	if !strings.Contains(shown(a), "bang-marker-7") {
		t.Errorf("the pane no longer draws the output:\n%s", shown(a))
	}
}

// Addressed at dispatch: a result that lands after the operator moved to the
// room still goes to the conversation it was typed in, and is drawn there.
func TestABangsOutputGoesWhereItWasTypedNotWhereFocusIs(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(80, 24)
	a = a.withDraft("!echo typed-in-alex")
	m, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a = m.(App).refocus("")
	a = loopMsgs(a, reproFlatten(cmd))

	frames := recorderOf(t, a).taken(t)
	if len(frames) != 1 || frames[0].SessionID != "s1" || frames[0].Shell == nil {
		t.Fatalf("frames = %+v, want one shell run to s1", frames)
	}
	if !strings.Contains(dmText(a, "s1"), "typed-in-alex") || strings.Contains(roomText(a), "typed-in-alex") {
		t.Errorf("the output was not drawn in alex's conversation alone")
	}
}

// An agent that cannot take it still has its command run and drawn, and the
// operator is told in plain words it was not sent - never that it was.
func TestABangToAnAgentThatCannotTakeItRunsAndSaysItWasNotSent(t *testing.T) {
	for _, state := range []string{rpc.StateEnded, rpc.StateParked} {
		fresh(t)
		a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(80, 24).
			withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: state})
		a = loopEnter(a.withDraft("!echo still-ran"))

		if frames := recorderOf(t, a).taken(t); len(frames) != 0 {
			t.Errorf("%s: a bang wrote %+v to an agent that cannot take it", state, frames)
		}
		if !strings.Contains(shown(a), "still-ran") {
			t.Errorf("%s: the command did not run, or its output was not drawn:\n%s", state, shown(a))
		}
		if n := notice.Count(fmt.Sprintf(bangNotSent, "@alex", bangGone[state])); n != 1 {
			t.Errorf("%s: the not-sent notice was reported %d times, want 1", state, n)
		}
	}
}

// What reaches the agent is the same bounded, labelled text the pane draws.
func TestABangsOutputIsBoundedOnTheWayToTheAgent(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(80, 24)
	a = loopEnter(a.withDraft("!head -c 200000 /dev/zero | tr '\\0' a"))

	frames := recorderOf(t, a).taken(t)
	if len(frames) != 1 || frames[0].Shell == nil {
		t.Fatalf("frames = %+v, want one shell run", frames)
	}
	out := frames[0].Shell.Output
	if len(out) > bangMaxBytes+len(bangTruncated)+16 || !strings.Contains(out, "truncated at") {
		t.Errorf("the agent was sent %d bytes ending %q, want at most the cap and the line saying so", len(out), out[max(0, len(out)-60):])
	}
}

// `@alex !cmd` in the room is a bang aimed at alex, in either mention mode: it
// runs in alex's directory, reaches alex alone as a shell run, and is drawn in
// the room in alex's thread and in alex's conversation when it is open.
func TestARoomMentionBangRunsInThatAgentsDirectoryAndReachesIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("alex's own file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []MentionMode{MentionDirect, MentionOpen} {
		a := newRoomApp(t).withSize(200, 40).withRoster(
			rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle, Cwd: dir},
			rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle},
		).openDMWith("s1", "alex").opened(t).refocus("")
		a.mention = mode
		a = loopEnter(a.withDraft("@alex !cat note.txt"))

		frames := recorderOf(t, a).taken(t)
		if len(frames) != 1 {
			t.Fatalf("%s: @alex !cat wrote %d frames, want one shell run to alex: %+v", mode, len(frames), frames)
		}
		f := frames[0]
		if f.SessionID != "s1" || f.Text != "" || f.Shell == nil || !strings.Contains(f.Shell.Output, "alex's own file") {
			t.Errorf("%s: wrote %q text %q shell %+v, want alex's file sent to alex as a shell run", mode, f.SessionID, f.Text, f.Shell)
		}
		if !strings.Contains(roomText(a), "alex's own file") || !strings.Contains(dmText(a, "s1"), "alex's own file") {
			t.Errorf("%s: the output was not drawn in both the room and alex's conversation", mode)
		}
	}
}

// The room's line for `@alex !cmd` is alex's: a room narrowed to alex shows it,
// one narrowed to john does not.
func TestARoomMentionBangIsInThatAgentsThread(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("alex", "john")
	a = loopEnter(a.withDraft("@alex !echo alex-thread"))

	alex, john := idOfAgentNamed(t, a, "alex"), idOfAgentNamed(t, a, "john")
	a.room = a.room.WithFocus(alex, "alex")
	if !strings.Contains(roomText(a), "alex-thread") {
		t.Error("a room narrowed to alex does not show the bang aimed at alex")
	}
	a.room = a.room.WithFocus(john, "john")
	if strings.Contains(roomText(a), "alex-thread") {
		t.Error("a room narrowed to john shows the bang aimed at alex")
	}
}

// The router routes only live agents, so a parked one's name used to fall
// through to the manager as the literal text. It is still one agent with a
// directory: the command runs there and the operator is told it was not sent.
func TestARoomBangAtAParkedAgentRunsAndSaysItWasNotSent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("parked alex's file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Cwd: dir},
		rpc.SessionStatus{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle},
	)
	a = loopEnter(a.withDraft("@alex !cat note.txt"))

	if frames := recorderOf(t, a).taken(t); len(frames) != 0 {
		t.Errorf("a bang at parked alex wrote %+v: the manager must not be sent the literal text", frames)
	}
	if !strings.Contains(roomText(a), "parked alex's file") {
		t.Error("the command did not run in alex's directory, or was not drawn")
	}
	if n := notice.Count(fmt.Sprintf(bangNotSent, "@alex", bangGone[rpc.StateParked])); n != 1 {
		t.Errorf("the not-sent notice was reported %d times, want 1", n)
	}
}

// A parked agent's name can be reissued to a live one; the router's answer - the
// live agent - is the one a bang is aimed at, whatever order the roster holds.
func TestARoomBangAtAReissuedNameReachesTheLiveAgent(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "alex", State: rpc.StateIdle},
	)
	a = loopEnter(a.withDraft("@alex !echo for-the-live-alex"))

	frames := recorderOf(t, a).taken(t)
	if len(frames) != 1 || frames[0].SessionID != "s2" || frames[0].Shell == nil {
		t.Errorf("frames = %+v, want one shell run to the live alex (s2)", frames)
	}
}

// A team whose members are all parked is still a team: refused, not relayed.
func TestARoomBangAtATeamWithNoLiveMemberIsRefused(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Team: "backend"},
		rpc.SessionStatus{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle},
	).withDraft("@backend !ls")
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("@backend !ls with no live member ran or sent something: %+v", reproFlatten(cmd))
	}
	if n := notice.Count(fmt.Sprintf(bangOneAgent, "@backend")); n != 1 {
		t.Errorf("the refusal was reported %d times, want 1", n)
	}
}

// The room draws a bang as what was said - `@alex !cmd`, then what it printed -
// on the operator's own line, never with a fence's backticks.
func TestTheRoomDrawsABangAsABlockHeadedByItsAgent(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("alex", "john")
	a = loopEnter(a.withDraft("@alex !echo aimed-output"))
	a = loopEnter(a.withDraft("!echo bare-output"))

	room := roomText(a)
	if strings.Contains(room, "```") {
		t.Errorf("the room drew a bang's fence as text:\n%s", room)
	}
	head := strings.Index(room, "@alex")
	if head < 0 || head > strings.Index(room, "!echo aimed-output") {
		t.Errorf("the aimed bang is not headed by @alex:\n%s", room)
	}
	if !strings.Contains(room, "bare-output") {
		t.Errorf("the bare bang was not drawn:\n%s", room)
	}
}

// The manager is one agent with a directory, so `@manager !cmd` is aimed at it
// rather than sent as the literal text.
func TestAManagerMentionBangReachesTheManager(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("alex", core.ManagerName)
	manager := idOfAgentNamed(t, a, core.ManagerName)
	a = loopEnter(a.withDraft("@manager !echo for-the-manager"))

	frames := recorderOf(t, a).taken(t)
	if len(frames) != 1 || frames[0].SessionID != manager || frames[0].Shell == nil {
		t.Fatalf("frames = %+v, want one shell run to the manager (%s)", frames, manager)
	}
}

// A bang runs in one directory and reaches one agent, so a team or @all is
// refused before anything runs, and the draft is kept.
func TestABangAimedAtATeamOrEveryoneIsRefused(t *testing.T) {
	for _, draft := range []string{"@backend !ls", "@all !ls"} {
		a := newRoomApp(t).withSize(200, 40).withRoster(
			rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle, Team: "backend"},
			rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle, Team: "backend"},
		).withDraft(draft)
		a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil {
			t.Errorf("%s ran or sent something: %+v", draft, reproFlatten(cmd))
		}
		if n := notice.Count(fmt.Sprintf(bangOneAgent, strings.Fields(draft)[0])); n != 1 {
			t.Errorf("%s: the refusal was reported %d times, want 1", draft, n)
		}
		if a.room.Composer().Value() != draft {
			t.Errorf("%s: the draft was not kept: %q", draft, a.room.Composer().Value())
		}
	}
}

// A bare room bang is addressed to nobody, as before: it runs and is drawn, and
// no agent hears it.
func TestABareRoomBangStillReachesNobody(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("alex", core.ManagerName)
	a = loopEnter(a.withDraft("!echo for-the-room"))

	if frames := recorderOf(t, a).taken(t); len(frames) != 0 {
		t.Errorf("a bare room bang wrote %+v", frames)
	}
	if !strings.Contains(roomText(a), "for-the-room") {
		t.Error("a bare room bang was not drawn in the room")
	}
}

// Open mode widens a message, never a command: the composer promises the one
// agent a bang reaches, and the completion menu reads the same route.
func TestABangStaysDirectInOpenMode(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
	a.mention = MentionOpen
	a = a.withDraft("@alex !ls")
	if got := a.room.Composer().Target(); got != "→ @alex · direct" {
		t.Errorf("the target line reads %q, want → @alex · direct", got)
	}
	if agent, ok := a.addressedAgent(); !ok || agent.Name != "alex" {
		t.Errorf("the completion menu addresses %q (%v), want alex", agent.Name, ok)
	}
}

// What the agent is sent reads back off its transcript as exactly the block the
// pane drew live, and neither ↑ nor a rewind takes it for something typed.
func TestABangReadsBackAsTheBlockThePaneDrew(t *testing.T) {
	m := bangResultMsg{ID: "s1", To: "s1", Cmd: "cat note.txt", Text: "hello\n… exited 1"}
	line, err := core.EncodeShellRun(m.Cmd, m.Text, "")
	if err != nil {
		t.Fatal(err)
	}
	evs, err := core.DecodeLine(line)
	if err != nil || len(evs) != 1 {
		t.Fatalf("decoded %d events (err %v)", len(evs), err)
	}
	live := bangEvent(m)
	if evs[0].Kind != live.Kind || evs[0].Echoed != live.Echoed || evs[0].Text != live.Text {
		t.Errorf("read back as %v echoed=%v %q, want the live %v echoed=%v %q", evs[0].Kind, evs[0].Echoed, evs[0].Text, live.Kind, live.Echoed, live.Text)
	}
	if typedByHand(evs[0]) {
		t.Error("a bang read back off disk counts as a typed prompt: ↑ would offer it")
	}
}

// dmText is one conversation as drawn, wherever the focus is.
func dmText(a App, id string) string { return stripANSI(a.dms[id].View(120, 40)) }

// roomText is the room as drawn.
func roomText(a App) string { return stripANSI(a.room.View(120, 40)) }

// loopEnter presses ↵ and runs what it hands back the way Bubble Tea's loop does.
func loopEnter(a App) App {
	m, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return loopMsgs(m.(App), reproFlatten(cmd))
}

// loopMsgs folds messages and runs the commands they hand back, a bounded number
// of rounds.
func loopMsgs(a App, msgs []tea.Msg) App {
	var m tea.Model = a
	for round := 0; round < 8 && len(msgs) > 0; round++ {
		var next []tea.Msg
		for _, msg := range msgs {
			var cmd tea.Cmd
			m, cmd = m.Update(msg)
			next = append(next, reproFlatten(cmd)...)
		}
		msgs = next
	}
	return m.(App)
}

// reproFlatten runs a command, opening batches and sequences.
func reproFlatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, reproFlatten(c)...)
		}
		return out
	}
	if v := reflect.ValueOf(msg); v.IsValid() && v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
		var out []tea.Msg
		for i := range v.Len() {
			out = append(out, reproFlatten(v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}
