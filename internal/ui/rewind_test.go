package ui

// esc esc, from the outside: when it arms and does nothing, when it opens the
// rewind picker, and what the picker itself does with the keys it claims.
//
// The invariant this whole feature turns on is the first test below: mashing
// esc at a running agent must always interrupt, never open a picker. The
// other two esc cases - a non-empty composer, and today's arm-then-interrupt
// - are asserted unchanged in the same test, because the fix that broke
// either of them silently is exactly the kind of regression a picker feature
// invites.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// workingAgentFrame reports one agent as mid-turn, the way a fleet report
// does. Named apart from beat_test.go's workingStatus, which builds a
// multi-agent *rpc.Status rather than one addressed frame.
func workingAgentFrame(id, name string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: id, Name: name, State: rpc.StateWorking}},
	}}
}

func TestEscEscOpensRewindOnlyWhenIdleAndEmpty(t *testing.T) {
	t.Run("idle and empty: first esc arms, second asks, a reply opens it", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

		after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
		if after.rewind.Open() {
			t.Fatal("the first esc opened the rewind picker; it should only arm")
		}
		go func() { _ = runCmdQuietly(cmd) }()
		f := awaitFrame(t, sent)
		if f.Kind != rpc.FrameInterrupt || f.SessionID != "s1" {
			t.Errorf("the first esc wrote %+v, want a harmless FrameInterrupt for s1", f)
		}

		after2, cmd2 := pressKey(after, tea.KeyMsg{Type: tea.KeyEsc})
		if after2.rewind.Open() {
			t.Fatal("the second esc opened the picker synchronously; it must wait for the reply")
		}
		go func() { _ = runCmdQuietly(cmd2) }()
		f2 := awaitFrame(t, sent)
		if f2.Kind != rpc.FrameRewindTargets || f2.SessionID != "s1" {
			t.Errorf("the second esc wrote %+v, want a FrameRewindTargets for s1", f2)
		}

		reply := rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: "s1", RewindTargets: []rpc.RewindTarget{
			{UUID: "u1", Text: "first prompt"},
			{UUID: "u2", Text: "second prompt"},
		}}
		got := after2.applyFrame(reply)
		if !got.rewind.Open() {
			t.Fatal("the reply did not open the rewind picker")
		}
		if got.rewind.LastSeen != "u2" {
			t.Errorf("LastSeen = %q, want the newest uuid u2", got.rewind.LastSeen)
		}
		if len(got.rewind.Prompts) != 2 || len(got.rewind.UUIDs) != 2 {
			t.Fatalf("the picker holds %d prompts and %d uuids, want 2 and 2", len(got.rewind.Prompts), len(got.rewind.UUIDs))
		}
	})

	t.Run("a running agent always interrupts, mashed or not", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").applyFrame(workingAgentFrame("s1", "alex")).withSize(160, 30)

		for i := range 3 {
			after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
			if after.rewind.Open() {
				t.Fatalf("press %d opened the rewind picker against a running agent", i+1)
			}
			go func() { _ = runCmdQuietly(cmd) }()
			f := awaitFrame(t, sent)
			if f.Kind != rpc.FrameInterrupt || f.SessionID != "s1" {
				t.Errorf("press %d wrote %+v, want a FrameInterrupt for s1", i+1, f)
			}
			a = after
		}
	})

	t.Run("a non-empty composer still clears on the second press, never opens the picker", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
		a = a.withDraft("half a sentence")

		after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
		if after.rewind.Open() {
			t.Fatal("the first esc over a draft opened the rewind picker")
		}
		if after.composer().Value() == "" {
			t.Fatal("the first esc cleared the draft; it should only interrupt and arm")
		}
		go func() { _ = runCmdQuietly(cmd) }()
		f := awaitFrame(t, sent)
		if f.Kind != rpc.FrameInterrupt {
			t.Errorf("the first esc over a draft wrote %+v, want a FrameInterrupt", f)
		}

		after2, cmd2 := pressKey(after, tea.KeyMsg{Type: tea.KeyEsc})
		if after2.rewind.Open() {
			t.Fatal("the second esc over a draft opened the rewind picker")
		}
		if after2.composer().Value() != "" {
			t.Errorf("the second esc left %q in the draft, want it cleared", after2.composer().Value())
		}
		if cmd2 != nil {
			go func() { _ = runCmdQuietly(cmd2) }()
			select {
			case f := <-sent:
				t.Errorf("clearing the draft wrote %+v; it should send nothing", f)
			default:
			}
		}
	})

	t.Run("a report flipping the agent to running between presses falls through to interrupt", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

		after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
		go func() { _ = runCmdQuietly(cmd) }()
		awaitFrame(t, sent) // the first, harmless interrupt

		// A status push lands between the two presses - the agent is now
		// running, with no key or mouse event to disarm escArmed.
		running := after.applyFrame(workingAgentFrame("s1", "alex"))

		after2, cmd2 := pressKey(running, tea.KeyMsg{Type: tea.KeyEsc})
		if after2.rewind.Open() {
			t.Fatal("the second press opened the picker even though the agent started running")
		}
		go func() { _ = runCmdQuietly(cmd2) }()
		f2 := awaitFrame(t, sent)
		if f2.Kind != rpc.FrameInterrupt {
			t.Errorf("the second press wrote %+v, want a genuine FrameInterrupt", f2)
		}
	})

	// MINOR #3: a fast ⎋⎋ shares one read and arrives as a single alt+esc -
	// escape.go's own collapsed case. Before the fix this returned from
	// askRewindTargets without ever calling interrupt, so the slow path sent
	// a harmless interrupt on its first press and the collapsed path sent
	// none: the same two keystrokes disagreeing about whether ⎋ interrupts
	// depending on how fast they were typed.
	t.Run("idle and empty, collapsed: interrupts as well as asking, symmetric with the slow path", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

		after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc, Alt: true})
		if after.rewind.Open() {
			t.Fatal("a collapsed esc esc opened the picker synchronously; it must wait for the reply")
		}
		// The write is a two-member tea.Batch here (the ask and the
		// interrupt), which a plain runCmdQuietly does not run to completion
		// - see runLikeTheLoop in park_test.go for why.
		go func() { runLikeTheLoop(cmd) }()
		first := awaitFrame(t, sent)
		second := awaitFrame(t, sent)
		byKind := map[string]rpc.Frame{first.Kind: first, second.Kind: second}

		if f, ok := byKind[rpc.FrameInterrupt]; !ok || f.SessionID != "s1" {
			t.Errorf("a collapsed esc esc on an idle, empty conversation wrote %+v and %+v, want a FrameInterrupt for s1 among them", first, second)
		}
		if f, ok := byKind[rpc.FrameRewindTargets]; !ok || f.SessionID != "s1" {
			t.Errorf("a collapsed esc esc wrote %+v and %+v, want a FrameRewindTargets for s1 among them", first, second)
		}
	})
}

// TestRewindPickerNavigatesAndConfirms exercises the picker once it is open,
// built directly rather than through the ask/reply round trip above - that
// round trip is what the previous test proves.
func TestRewindPickerNavigatesAndConfirms(t *testing.T) {
	newPicker := func() RewindPicker {
		return RewindPicker{
			Session:  "s1",
			Prompts:  []string{"newest prompt", "older prompt"},
			UUIDs:    []string{"u2", "u1"},
			LastSeen: "u2",
		}
	}

	t.Run("arrows move the cursor without wrapping", func(t *testing.T) {
		fresh(t)
		conn, _ := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
		a.rewind = newPicker()

		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
		if a.rewind.Cursor != 1 {
			t.Fatalf("cursor = %d after one down, want 1", a.rewind.Cursor)
		}
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
		if a.rewind.Cursor != 1 {
			t.Fatalf("cursor = %d after a second down, want 1 (no wrap)", a.rewind.Cursor)
		}
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
		if a.rewind.Cursor != 0 {
			t.Fatalf("cursor = %d after up, want 0", a.rewind.Cursor)
		}
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
		if a.rewind.Cursor != 0 {
			t.Fatalf("cursor = %d after a second up, want 0 (no wrap)", a.rewind.Cursor)
		}
	})

	t.Run("enter sends the cursored uuid and the newest as last-seen, then closes", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
		a.rewind = newPicker()
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown}) // cursor onto the older prompt, u1

		after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
		if after.rewind.Open() {
			t.Error("enter left the picker up")
		}
		go func() { _ = runCmdQuietly(cmd) }()
		f := awaitFrame(t, sent)
		if f.Kind != rpc.FrameRewind || f.SessionID != "s1" || f.RewindTarget != "u1" || f.RewindLastSeen != "u2" {
			t.Errorf("enter wrote %+v, want a FrameRewind for s1 targeting u1 with last-seen u2", f)
		}
	})

	t.Run("esc closes and sends nothing", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
		a.rewind = newPicker()

		after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
		if after.rewind.Open() {
			t.Error("esc left the picker up")
		}
		if cmd != nil {
			go func() { _ = runCmdQuietly(cmd) }()
		}
		select {
		case f := <-sent:
			t.Errorf("esc wrote %+v; cancelling sends nothing", f)
		default:
		}
	})

	t.Run("an unclaimed key closes the picker and falls through to the draft", func(t *testing.T) {
		fresh(t)
		conn, _ := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
		a.rewind = newPicker()

		after, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		if after.rewind.Open() {
			t.Error("a rune left the rewind picker up")
		}
		if after.composer().Value() != "x" {
			t.Errorf("the rune did not reach the composer: %q", after.composer().Value())
		}
	})
}

// An empty reply is not a picker with nothing to choose: there is nothing to
// rewind to, so this reports rather than opening.
func TestEmptyRewindTargetsDoesNotOpen(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	got := a.applyFrame(rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: "s1"})
	if got.rewind.Open() {
		t.Fatal("an empty reply opened the rewind picker")
	}
	n, ok := notice.Latest()
	if !ok || !strings.Contains(n.Text, "rewind") {
		t.Errorf("no notice explained the empty reply: %+v (present=%v)", n, ok)
	}
}

// A reply naming a conversation this client no longer holds is dropped,
// historyArrived's own staleness rule: asking again after ⌃W would be a
// picker with no pane to draw it in.
func TestARewindReplyForAClosedConversationIsDropped(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := NewRoomApp(conn, Stream{}, nil)

	got := a.applyFrame(rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: "s1", RewindTargets: []rpc.RewindTarget{
		{UUID: "u1", Text: "prompt"},
	}})
	if got.rewind.Open() {
		t.Fatal("a reply for a conversation with no open DM opened the rewind picker")
	}
}

// CRITICAL #1, the invariant this whole feature turns on: mashing esc at a
// running agent always interrupts, and a rewind picker belongs to the
// conversation it was opened for - never to whichever pane happens to be
// focused when a key arrives.
//
// Deterministic repro, no race: alex (s1) is focused, idle and empty, and
// blair (s2) is already running. esc esc opens the picker for alex. The
// operator tabs to blair and presses esc, meaning to interrupt it.
//
// Before the fix, rewindKey read only a.rewind.Open() - true, because the
// picker opened for alex is still there - so it fired case tea.KeyEsc:
// closeRewind() and returned handled=true from App.key *before* the switch's
// own KeyEsc case (escape() -> interrupt()) ever ran. Blair was never
// interrupted: this test fails with a 2s awaitFrame timeout in that state,
// because closeRewind's cmd is nil and nothing ever reaches the daemon.
func TestATabAwayFromAnOpenRewindPickerStillLetsEscInterruptTheNewlyFocusedAgent(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "blair").withSize(160, 30)
	a = a.openDMWith("s2", "blair").applyFrame(workingAgentFrame("s2", "blair"))
	// Back onto alex: idle, empty, the picker's own conversation. Both s1 and
	// s2 stay in the ring (dmOrder) regardless of which one currently holds
	// the grid's one DM slot - see TestTabCyclesEveryConversationThatIsOpenInAFixedOrder.
	a = a.openDMWith("s1", "alex")
	if a.focus != "s1" {
		t.Fatalf("focus = %q after re-opening alex, want s1: this test is not exercising the case it claims to", a.focus)
	}
	// Opening blair queued a history ask (show -> askHistory) that only a
	// real Update drains - the two opens above are plain method calls, not
	// keystrokes, so it is still pending. Left alone it would ride inside
	// cmd's tea.Batch on the very next pressKey below, and the single-frame
	// awaitFrame calls in this test do not unwrap one.
	a, _ = a.takeHistoryAsks()

	after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc}) // arm
	go func() { _ = runCmdQuietly(cmd) }()
	awaitFrame(t, sent) // the harmless first interrupt

	after2, cmd2 := pressKey(after, tea.KeyMsg{Type: tea.KeyEsc}) // ask
	go func() { _ = runCmdQuietly(cmd2) }()
	ask := awaitFrame(t, sent)
	if ask.Kind != rpc.FrameRewindTargets || ask.SessionID != "s1" {
		t.Fatalf("the second esc wrote %+v, want a FrameRewindTargets for s1", ask)
	}

	reply := rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: "s1", RewindTargets: []rpc.RewindTarget{
		{UUID: "u1", Text: "first prompt"},
	}}
	opened := after2.applyFrame(reply)
	if !opened.rewind.Open() || opened.rewind.Session != "s1" {
		t.Fatalf("the rewind picker did not open for s1: %+v", opened.rewind)
	}

	moved := tab(opened)
	if moved.focus != "s2" {
		t.Fatalf("tab moved focus to %q, want s2: this test is not exercising the tab-away case", moved.focus)
	}
	if moved.rewind.Open() {
		// Belt and braces (CRITICAL #1, item 3): withFocus closes a picker
		// that does not belong to the pane it is moving to. Not required for
		// the invariant below - rewindKey's own scoping already refuses this
		// picker's keys once focus is s2 - but a stale picker left standing
		// is worth catching here too.
		t.Error("the rewind picker for alex was still open after tab moved focus to blair")
	}

	final, cmd3 := pressKey(moved, tea.KeyMsg{Type: tea.KeyEsc})
	go func() { _ = runCmdQuietly(cmd3) }()
	interrupted := awaitFrame(t, sent)
	if interrupted.Kind != rpc.FrameInterrupt || interrupted.SessionID != "s2" {
		t.Fatalf("esc while focused on blair wrote %+v, want a FrameInterrupt for s2 - "+
			"the rewind picker left open for alex swallowed the key instead of letting it interrupt blair", interrupted)
	}
	if final.focus != "s2" {
		t.Errorf("focus moved to %q after the interrupt, want it to stay on s2", final.focus)
	}
}

// CRITICAL #1's second half: the ask is written the moment the second esc
// lands, and the daemon's answer can arrive after the operator has moved on.
// rewindTargetsArrived used to check only "still held" - a reply for a
// conversation the operator left, or whose agent started a turn while the
// daemon was reading the transcript, still opened a picker that Enter would
// then rewind for real. The fix reuses rewindArmable, the same predicate
// that gated the ask.
func TestAStaleRewindReplyIsDropped(t *testing.T) {
	t.Run("focus moved to another conversation", func(t *testing.T) {
		fresh(t)
		conn, _ := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "blair").withSize(160, 30)
		// The ask was for s1; by the time the reply lands, the operator has
		// moved the keys to s2.
		a = a.openDMWith("s2", "blair")

		reply := rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: "s1", RewindTargets: []rpc.RewindTarget{
			{UUID: "u1", Text: "prompt"},
		}}
		got := a.applyFrame(reply)
		if got.rewind.Open() {
			t.Fatal("a rewind reply for a conversation the operator left opened the picker")
		}
	})

	t.Run("the agent started a turn while the reply was in flight", func(t *testing.T) {
		fresh(t)
		conn, _ := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
		// Still focused on s1, but no longer idle.
		a = a.applyFrame(workingAgentFrame("s1", "alex"))

		reply := rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: "s1", RewindTargets: []rpc.RewindTarget{
			{UUID: "u1", Text: "prompt"},
		}}
		got := a.applyFrame(reply)
		if got.rewind.Open() {
			t.Fatal("a rewind reply for an agent that started running opened the picker")
		}
	})
}

// IMPORTANT #2: picker.go's "one at a time" rule, extended to the second
// picker that can open here. Without this, a rewind reply arriving while an
// /effort or /model picker was up replaced it silently - menuBlock stacks
// both, pickerKey wins the keys, and the config picker's own keys (no digits,
// no runes) then silently take over rewindKey's shape.
func TestARewindReplyDoesNotOpenOverAnOpenConfigPicker(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
	a = a.openPicker(effortCommand, []string{"s1"})

	reply := rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: "s1", RewindTargets: []rpc.RewindTarget{
		{UUID: "u1", Text: "prompt"},
	}}
	got := a.applyFrame(reply)
	if got.rewind.Open() {
		t.Fatal("a rewind reply opened its picker over an open config picker")
	}
	if !got.picker.Open() {
		t.Fatal("the config picker closed on its own; it should be the one thing still open")
	}
}

// #4: confirmRewind lacks interrupt()'s own endedAgent guard. A picker can
// outlive the agent it was opened for - the reply and the ending can race the
// same way a reply and a moved focus do in TestAStaleRewindReplyIsDropped -
// and Enter on it must not write a frame with nobody left to read it.
func TestConfirmRewindOnAnEndedAgentSendsNothing(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withSize(160, 30)
	a = a.apply(rpc.Frame{
		Kind:   rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", State: rpc.StateEnded}}},
	})
	a.rewind = RewindPicker{Session: "s1", Prompts: []string{"prompt"}, UUIDs: []string{"u1"}, LastSeen: "u1"}

	after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if after.rewind.Open() {
		t.Error("enter on an ended agent's rewind picker left it open")
	}
	if cmd != nil {
		go func() { _ = runCmdQuietly(cmd) }()
	}
	select {
	case f := <-sent:
		t.Errorf("a rewind was sent to a session that has ended: %+v", f)
	case <-time.After(50 * time.Millisecond):
	}
}

// The receipt, Claude's answer to a FrameRewind confirmRewind already sent -
// see noteRewind in rewind.go.

// A successful rewind clears the focused conversation and re-asks for it, so
// the daemon's tree-aware FrameHistoryReply is what repopulates it - and
// prefills the composer, because the operator is looking at exactly the pane
// the rewound prompt belongs in.
func TestRewoundReceiptReReadsAndPrefills(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, Text: "the dead branch's own reply"})
	if a.dms["s1"].events.len() == 0 {
		t.Fatal("setup: the conversation holds nothing for the receipt to clear")
	}

	after := a.observe("s1", core.Event{
		Kind:   core.KindRewindReceipt,
		Rewind: &core.RewindResult{Rewound: true, PrefillText: "redo me"},
	})

	if got := after.composer().Value(); got != "redo me" {
		t.Errorf("composer draft = %q after a successful rewind, want the rewound prompt", got)
	}
	if n := after.dms["s1"].events.len(); n != 0 {
		t.Errorf("the conversation still holds %d events; a successful rewind should clear it pending a fresh, tree-aware read", n)
	}
	if len(after.pendingHistory) != 1 || after.pendingHistory[0] != "s1" {
		t.Fatalf("pendingHistory = %v, want a fresh ask queued for s1", after.pendingHistory)
	}

	// The daemon's tree-aware reply carries only the active branch - the dead
	// branch's own reply above is not in it.
	restored := after.historyArrived(rpc.Frame{
		Kind: rpc.FrameHistoryReply, SessionID: "s1",
		Events: []core.Event{{Kind: core.KindUserText, SessionID: "s1", Text: "redo me"}},
	})
	if n := restored.dms["s1"].events.len(); n != 1 {
		t.Fatalf("after the tree-aware reply the conversation holds %d events, want exactly the 1 active-branch event", n)
	}
}

// A refused rewind changes nothing. The daemon's write succeeded and the
// receipt is drawn by no view, so notice.Report is the only account of it -
// mode.go's own reasoning about a refused control request.
func TestFailedRewindReceiptReportsAndLeavesTheTranscript(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, Text: "still here"})
	before := a.dms["s1"].events.len()

	after := a.observe("s1", core.Event{
		Kind:   core.KindRewindReceipt,
		Rewind: &core.RewindResult{Rewound: false, Error: "unseen later turn"},
	})

	n, said := notice.Latest()
	if !said || !strings.Contains(n.Text, "unseen later turn") {
		t.Errorf("no notice carried the daemon's own refusal reason: %+v (said=%v)", n, said)
	}
	if !strings.Contains(n.Text, "alex") {
		t.Errorf("the refusal %q does not name the agent", n.Text)
	}
	if got := after.dms["s1"].events.len(); got != before {
		t.Errorf("a refused rewind changed the transcript: %d events, want the %d already there", got, before)
	}
	if got := after.composer().Value(); got != "" {
		t.Errorf("a refused rewind wrote %q into the draft, want it untouched", got)
	}
	if len(after.pendingHistory) != 0 {
		t.Errorf("a refused rewind queued a history ask: %v, want none", after.pendingHistory)
	}
}

// A rewind receipt for a conversation that is not focused still re-reads it -
// the operator will find the dead branch gone next time they look - but the
// prefill stays out of whichever pane they are actually looking at. Handing a
// rewound prompt to a composer nobody is reading is a draft that appears to
// have typed itself into the wrong conversation.
func TestRewoundReceiptForAnUnfocusedSessionReReadsButDoesNotPrefill(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex", "blair").withSize(160, 30)
	a = a.openDMWith("s2", "blair")
	if a.focus != "s2" {
		t.Fatalf("focus = %q after opening blair, want s2: this test is not exercising the unfocused case", a.focus)
	}
	a, _ = a.takeHistoryAsks() // drain blair's own open-time ask; see the tab-away test above

	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, Text: "alex's own dead-branch reply"})
	if a.dms["s1"].events.len() == 0 {
		t.Fatal("setup: alex's conversation holds nothing for the receipt to clear")
	}

	after := a.observe("s1", core.Event{
		Kind:   core.KindRewindReceipt,
		Rewind: &core.RewindResult{Rewound: true, PrefillText: "redo me"},
	})

	if got := after.composer().Value(); got != "" {
		t.Errorf("blair's composer holds %q; alex's rewind prefill reached the wrong pane", got)
	}
	if n := after.dms["s1"].events.len(); n != 0 {
		t.Errorf("alex's conversation still holds %d events; it should be re-read even though blair has the focus", n)
	}
	if len(after.pendingHistory) != 1 || after.pendingHistory[0] != "s1" {
		t.Fatalf("pendingHistory = %v, want a fresh ask queued for s1 even though it is not focused", after.pendingHistory)
	}
}

// openIdleRewindPicker drives the real esc-esc-then-reply sequence so the
// picker under test is the one a real operator would have gotten, not a
// struct literal - which is what makes the subtests below direct evidence
// for reconcileRewind (report.go) rather than only for rewindKey's own gate.
func openIdleRewindPicker(t *testing.T, a App, sent <-chan rpc.Frame) App {
	t.Helper()
	after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc}) // arm + a harmless interrupt
	go func() { _ = runCmdQuietly(cmd) }()
	awaitFrame(t, sent)

	after2, cmd2 := pressKey(after, tea.KeyMsg{Type: tea.KeyEsc}) // ask
	go func() { _ = runCmdQuietly(cmd2) }()
	ask := awaitFrame(t, sent)
	if ask.Kind != rpc.FrameRewindTargets {
		t.Fatalf("setup: the second esc wrote %+v, want a FrameRewindTargets", ask)
	}

	opened := after2.applyFrame(rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: ask.SessionID, RewindTargets: []rpc.RewindTarget{
		{UUID: "u1", Text: "first prompt"},
	}})
	if !opened.rewind.Open() {
		t.Fatal("setup: the reply did not open the rewind picker")
	}
	return opened
}

// Adversarial review, 2026-08-26, CRITICAL: the picker gates only on the
// session being idle *when it opens* - nothing closed or invalidated it if
// that same session started running afterwards, e.g. the manager's
// autonomous send_to_agent, or any other status transition. rewindKey read
// only Open() and Session == a.focus, and both stayed true: the operator
// never moved the keys, so esc kept matching rewindKey's own case
// tea.KeyEsc: closeRewind() and returned handled=true before the switch's
// KeyEsc case (escape()->interrupt()) ever ran. The running agent was never
// interrupted - this is what "mashing esc at a running agent always
// interrupts" failed to hold against.
//
// Distinct from the tab-away test above (also labelled CRITICAL #1, from an
// earlier review round): that one moves the focus away from the picker's own
// session. This one never does - the picker's own session is the one that
// started running - which is exactly why that guard (Session != a.focus)
// was not enough on its own.
func TestARewindPickerOverAnAgentThatStartsRunningDoesNotSwallowTheInterrupt(t *testing.T) {
	t.Run("esc interrupts, not merely closes the picker", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
		a = openIdleRewindPicker(t, a, sent)

		running := a.applyFrame(workingAgentFrame("s1", "alex"))

		_, cmd := pressKey(running, tea.KeyMsg{Type: tea.KeyEsc})
		go func() { _ = runCmdQuietly(cmd) }()
		f := awaitFrame(t, sent)
		if f.Kind != rpc.FrameInterrupt || f.SessionID != "s1" {
			t.Errorf("esc against a running agent's own open picker wrote %+v, want a FrameInterrupt for s1 - "+
				"the picker swallowed the key instead of letting it interrupt", f)
		}
	})

	t.Run("a collapsed esc esc interrupts too", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
		a = openIdleRewindPicker(t, a, sent)

		running := a.applyFrame(workingAgentFrame("s1", "alex"))

		_, cmd := pressKey(running, tea.KeyMsg{Type: tea.KeyEsc, Alt: true})
		go func() { _ = runCmdQuietly(cmd) }()
		f := awaitFrame(t, sent)
		if f.Kind != rpc.FrameInterrupt || f.SessionID != "s1" {
			t.Errorf("alt+esc against a running agent's own open picker wrote %+v, want a FrameInterrupt for s1", f)
		}
	})

	t.Run("enter sends no rewind once the picker's agent is running", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
		a = openIdleRewindPicker(t, a, sent)

		running := a.applyFrame(workingAgentFrame("s1", "alex"))

		_, cmd := pressKey(running, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil {
			go func() { _ = runCmdQuietly(cmd) }()
		}
		select {
		case f := <-sent:
			t.Errorf("enter sent %+v to a session that is running, want nothing", f)
		case <-time.After(50 * time.Millisecond):
		}
	})
}

// The CRITICAL fix's part 2 in isolation: rewindKey's own gate, for the
// window between a status push landing and a key arriving before
// reconcileRewind (report.go) has had that report to run against. Planted
// directly, the way TestRewindPickerNavigatesAndConfirms does, so the picker
// opens *after* the fleet already shows the agent running - nothing upstream
// has had a chance to close it, so this is rewindKey's own decline or
// nothing is.
func TestRewindKeyDeclinesEveryKeyOnceItsPickersAgentIsRunning(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
	a = a.applyFrame(workingAgentFrame("s1", "alex"))
	a.rewind = RewindPicker{Session: "s1", Prompts: []string{"prompt"}, UUIDs: []string{"u1"}, LastSeen: "u1"}

	_, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	go func() { _ = runCmdQuietly(cmd) }()
	f := awaitFrame(t, sent)
	if f.Kind != rpc.FrameInterrupt || f.SessionID != "s1" {
		t.Errorf("esc against a picker planted open on an already-running agent wrote %+v, want a FrameInterrupt for s1", f)
	}
}

// The CRITICAL fix's part 3 in isolation: confirmRewind's own guard, called
// directly rather than through rewindKey - which would itself decline first
// once part 2 is in place, so a test that only ever presses Enter could pass
// on part 2 alone and never prove this guard does anything. Defense in depth
// only earns the name if each layer holds up on its own.
func TestConfirmRewindRefusesARunningAgent(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
	a = a.applyFrame(workingAgentFrame("s1", "alex"))
	a.rewind = RewindPicker{Session: "s1", Prompts: []string{"prompt"}, UUIDs: []string{"u1"}, LastSeen: "u1"}

	after, cmd := a.confirmRewind()
	if after.rewind.Open() {
		t.Error("confirmRewind against a running agent left the picker open")
	}
	if cmd != nil {
		go func() { _ = runCmdQuietly(cmd) }()
	}
	select {
	case f := <-sent:
		t.Errorf("confirmRewind sent %+v to a session that is running, want nothing", f)
	case <-time.After(50 * time.Millisecond):
	}
}
