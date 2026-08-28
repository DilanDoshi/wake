package ui

// `/name` and `/task` — the founding message's *"you can either rename or
// assign a 'task' so they are called like `sydney <> dev-5748` or
// `alex <> ui fixes`"*, from the outside.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The two halves, each addressed two ways: the conversation you are in, and an
// `@name` from anywhere.
//
// `@` rather than a bare word, and that is what makes the grammar unambiguous
// rather than positional. `/name bob` in a conversation can only be "call this
// one bob"; `/name @alex bob` can only be "call alex bob". Without the marker
// the same two words would mean different things depending on which pane had
// the keys, which is the failure the room's own addressing already avoids.
func TestNameAndTaskCarryTheTargetAndTheValue(t *testing.T) {
	for _, tc := range []struct {
		name, draft, kind, session, text string
		room                             bool
	}{
		{name: "rename this conversation", draft: "/name bob", kind: rpc.FrameRename, session: "s1", text: "bob"},
		{name: "rename another agent", draft: "/name @sydney bob", kind: rpc.FrameRename, session: "s2", text: "bob"},
		{name: "rename from the room", draft: "/name @sydney bob", kind: rpc.FrameRename, session: "s2", text: "bob", room: true},
		{name: "label this conversation", draft: "/task ui fixes", kind: rpc.FrameLabel, session: "s1", text: "ui fixes"},
		{name: "label another agent", draft: "/task @sydney ui fixes", kind: rpc.FrameLabel, session: "s2", text: "ui fixes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)
			if tc.room {
				a = a.showRoom()
			}

			_, cmd := typeAndSubmit(a, tc.draft)
			go func() { _ = runCmdQuietly(cmd) }()
			f := awaitFrame(t, sent)

			if f.Kind != tc.kind {
				t.Fatalf("%q wrote a %q frame, want %q", tc.draft, f.Kind, tc.kind)
			}
			if f.SessionID != tc.session {
				t.Errorf("%q was addressed to %q, want %q", tc.draft, f.SessionID, tc.session)
			}
			if f.Text != tc.text {
				t.Errorf("%q asked for %q, want %q", tc.draft, f.Text, tc.text)
			}
		})
	}
}

// A label is prose and keeps its spaces. `alex <> ui fixes` is the founding
// message's own example, so a parser that took one word would fail on the
// string the feature was asked for in.
func TestATaskLabelKeepsEveryWordThatWasTyped(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)

	_, cmd := typeAndSubmit(a, "/task ship the roster fix")
	go func() { _ = runCmdQuietly(cmd) }()

	if f := awaitFrame(t, sent); f.Text != "ship the roster fix" {
		t.Errorf("the label reached the daemon as %q", f.Text)
	}
}

// Neither verb guesses a target, and neither guesses a value.
//
// The room is not one conversation, so `/name bob` there has no target - and
// the two available guesses are both unrecoverable in the way this project
// cares about: rename whichever agent the roster cursor is resting on, or
// rename the first one. Both change where the operator's next `@` goes.
func TestNameAndTaskRefuseRatherThanGuess(t *testing.T) {
	for _, tc := range []struct {
		name, draft, says string
		room              bool
	}{
		{name: "no target in the room", draft: "/name bob", says: noNameTarget, room: true},
		{name: "no target for a label", draft: "/task ui fixes", says: noTaskTarget, room: true},
		{name: "no new name", draft: "/name", says: nameUsage},
		{name: "no label", draft: "/task", says: taskUsage},
		{name: "a name for nobody", draft: "/name @nobody bob", says: noSuchAgent},
		{name: "two new names", draft: "/name bob carol", says: nameUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
			if tc.room {
				a = a.showRoom()
			}

			m, cmd := typeAndSubmit(a, tc.draft)
			if cmd != nil {
				t.Fatalf("%q was acted on anyway: %+v", tc.draft, sentFrames(t, m.(App), cmd))
			}
			if got := shown(m.(App)); !strings.Contains(got, tc.says) {
				t.Errorf("%q was refused without saying %q:\n%s", tc.draft, tc.says, got)
			}
		})
	}
}

// **The ruling, at the layer where a name is an address.** After a rename the
// new handle routes and the old one does not.
//
// The alternative was an alias, and the pool is what kills it: a name goes back
// to the pool when its session ends, so `@alex` kept alive as an alias for a
// session now called `bob` resolves to two live agents the moment a fresh spawn
// draws `alex`. What this asserts is the half a client can hold: the room reads
// the daemon's report and nothing else, so the old handle stops resolving in
// the same frame the new one starts.
func TestARenamedAgentAnswersToItsNewHandleAndNotItsOld(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("alex")

	if _, ok := a.fleet.ByName("alex"); !ok {
		t.Fatal("the fixture's agent does not answer to its own name, so this proves nothing about a rename")
	}

	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "bob", State: rpc.StateIdle},
	}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})

	if _, ok := a.fleet.ByName("bob"); !ok {
		t.Error("@bob reaches nothing after the rename: the handle the roster now shows has to be the one that routes")
	}
	if agent, ok := a.fleet.ByName("alex"); ok {
		t.Errorf("@alex still resolves, to %+v. A name is released and reissued when a session ends, so an "+
			"alias outlives what it named and starts resolving to two live agents - which is the one "+
			"failure `no two live sessions share a name` exists to prevent", agent)
	}
}

// The keypress says which agent, and **not what it will be called** - because
// this side does not know.
//
// `normalizeName` folds case behind the socket, so an ask that echoed the
// request said *"to @BOB"* and stored `bob`; `normalizeLabel` truncates at a
// column bound, so an ask that echoed a long label reported a value that was
// never stored - and it ended in `…`, the same rune as truncationMark, so an
// untruncated label read as truncated. A confirmation that reports the request
// as though it were the outcome is the lying-feature shape.
//
// What the operator gets instead is the stored value, from the surface the
// daemon feeds: the header, which App.renamed keeps in step with the report.
func TestTheAskNamesTheAgentAndTheReportNamesTheResult(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)

	m, cmd := typeAndSubmit(a, "/name BOB")
	go func() { _ = runCmdQuietly(cmd) }()
	a = m.(App)

	said := shown(a)
	if !strings.Contains(said, "renaming") || !strings.Contains(said, agentPrefix+"alex") {
		t.Errorf("the keypress did not say which agent is being renamed:\n%s", said)
	}
	if strings.Contains(said, "BOB") {
		t.Errorf("the keypress reported %q as the new name and the daemon folds case, so the sentence "+
			"names something no session will ever be called:\n%s", "BOB", said)
	}

	// The daemon's answer is what says how it turned out.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "bob", State: rpc.StateIdle},
	}}})
	if got := shown(a); !strings.Contains(got, agentPrefix+"bob") {
		t.Errorf("nothing shows the name the daemon actually stored:\n%s", got)
	}
}

// The daemon's refusal is shown as the daemon wrote it. Every one of them names
// *when* the operator can act - a parked session that `/resume` brings back, a
// name another agent holds, the manager's name that is not display at all - and
// a local "could not rename" would replace the only useful half.
func TestTheDaemonsRefusalOfARenameIsWhatTheOperatorReads(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)

	const why = "this session is parked, and what it is called is written in the park book by the park itself"
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameError, SessionID: "s1", Text: why})

	if !strings.Contains(shown(a), "parked") {
		t.Errorf("the daemon's refusal did not reach the operator:\n%s", shown(a))
	}
}

// **The ruling's own safety argument, at the one surface where a handle is read
// in order to be typed.**
//
// Everywhere else a stale handle produces a refusal, which is what the ruling
// trades on: `@old` resolves to nothing and the operator reads a sentence. The
// DM header is different in kind. It is not resolved by anything - it is *read
// by a person and then typed*, and a name goes back to the pool the moment its
// session gives it up. So a header that kept the name a conversation was opened
// under would be a handle pointing at whoever draws that name next, and the
// outcome is not a refusal but a delivery: `@alex rebase and force-push` to an
// agent spawned `auto` in a directory nobody chose, with no error anywhere.
//
// The scenario is this branch's own two verbs, in the order an operator uses
// them: rename the conversation you have open, then start something.
func TestAnOpenConversationsHeaderNeverNamesAnAgentThatIsNotIt(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	if !strings.Contains(shown(a), agentPrefix+"alex") {
		t.Fatal("the fixture's header does not name the agent it is open on, so nothing below is about a rename")
	}

	// The daemon confirms the rename, and later somebody spawns an agent that
	// draws the freed name.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "bob", State: rpc.StateIdle},
	}}})
	if got := shown(a); !strings.Contains(got, agentPrefix+"bob") {
		t.Errorf("the open conversation still heads itself with the name it was opened under:\n%s", got)
	}

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "bob", State: rpc.StateIdle},
		{ID: "s2", Name: "alex", State: rpc.StateIdle},
	}}})
	if dm := a.dms["s1"]; dm.Name == "alex" {
		t.Errorf("the conversation with s1 heads itself @alex, and @alex is now s2. A handle on this " +
			"surface is read in order to be typed, so the next message goes to a stranger with nothing " +
			"reporting it")
	}
	// And the handle it does show is the one that routes back to it.
	if agent, ok := a.fleet.ByName(a.dms["s1"].Name); !ok || agent.ID != "s1" {
		t.Errorf("the header names %q, which resolves to %+v rather than to s1", a.dms["s1"].Name, agent)
	}
}

// The bijection, over every conversation this client holds rather than over the
// one in front of it.
//
// `hideDM` keeps a DM's transcript so ⌃W is reversible, and `openDMWith` builds
// one only when the map has none - so a conversation closed before a rename and
// reopened after it would still carry the old handle if the refresh were keyed
// on opening. Stated as a property over the whole map because that is the shape
// the defect had: not "the visible header is wrong" but "some header is".
func TestEveryOpenConversationCarriesTheNameAndParentTheReportDoes(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("alex", "sydney")
	a = a.openDMWith("s1", "alex").openDMWith("s2", "sydney")
	a = a.hideDM(true) // closed, transcript kept - the case a re-open would miss

	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "bob", State: rpc.StateIdle},
		{ID: "s2", Name: "maya", State: rpc.StateIdle, ParentID: "s1"},
	}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})

	if len(a.dms) != 2 {
		t.Fatalf("this client holds %d conversations, want 2: the fixture cannot show a bijection over one", len(a.dms))
	}
	for _, s := range st.Sessions {
		dm, held := a.dms[s.ID]
		if !held {
			t.Fatalf("no conversation for %s", s.ID)
		}
		if dm.Name != s.Name {
			t.Errorf("the conversation with %s heads itself %q and the report says %q", s.ID, dm.Name, s.Name)
		}
		if want := a.parentName(s.ID); dm.ParentName != want {
			t.Errorf("the conversation with %s says it was forked from %q and the fleet says %q: a renamed "+
				"*parent* leaves the same stale handle on a child, one surface down", s.ID, dm.ParentName, want)
		}
	}
}

// `/rename` is claude's own word - the recorded corpus shows it advertised, so
// the router leaves it a message and it still reaches the agent - and Wake
// mirrors it onto its own handle in the same keystroke, so the roster and
// claude's title do not drift. One `/rename bob` in a conversation therefore
// both renames s1 in Wake and sends the draft on to claude.
//
// The two halves are one action but two writes, because they are as independent
// as /name is from a message: the rename is the daemon's to accept or refuse,
// and the send keeps its own refusals. This asserts both frames go out.
func TestRenameMirrorsOntoWakesHandleWhileStillReachingClaude(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)

	_, cmd := typeAndSubmit(a, "/rename bob")
	frames := batchFrames(t, a, cmd)

	var renamed, passed bool
	for _, f := range frames {
		if f.Kind == rpc.FrameRename && f.SessionID == "s1" && f.Text == "bob" {
			renamed = true
		}
		if f.Kind == rpc.FrameSend && f.SessionID == "s1" && strings.Contains(f.Text, "rename bob") {
			passed = true
		}
	}
	if !renamed {
		t.Errorf("/rename bob wrote no FrameRename for s1, so Wake's own handle did not move - the reported bug. frames=%+v", frames)
	}
	if !passed {
		t.Errorf("/rename bob did not still reach claude as a message, so claude's own /rename stopped working. frames=%+v", frames)
	}
}

// A bare `/rename` with no new name is left entirely alone: nothing to mirror,
// and the draft passes through to claude the way it always did.
func TestBareRenameIsJustAMessage(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)

	_, cmd := typeAndSubmit(a, "/rename")
	for _, f := range batchFrames(t, a, cmd) {
		if f.Kind == rpc.FrameRename {
			t.Errorf("bare /rename wrote a FrameRename with no name to give: %+v", f)
		}
	}
}

// The mirror follows claude's grammar, not `/name`'s, and every place they
// differ it declines **silently** - no FrameRename, and no `/name`-flavoured
// refusal leaking over a passthrough that worked.
//
// claude's `/rename` renames the session it is typed in: it has no `@who` (so a
// leading one is just a word claude reads, not a Wake target - the reported
// footgun was `/rename @sydney bob` in alex's DM renaming sydney in Wake while
// claude renamed alex), it is a one-word name (Wake cannot hold two), it is the
// exact advertised word (not `/RENAME`), and it is a conversation (the room is
// not one session). Each is left to the passthrough rather than mirrored wrong.
func TestRenameMirrorDeclinesSilentlyWhenItIsNotClaudesGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, draft string
		room        bool
	}{
		{name: "a leading @who is not a target", draft: "/rename @sydney bob"},
		{name: "a multi-word name Wake cannot hold", draft: "/rename bob smith"},
		{name: "the exact word, not a folded one", draft: "/RENAME bob"},
		{name: "the room is not one conversation", draft: "/rename bob", room: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)
			if tc.room {
				a = a.showRoom()
			}

			_, cmd := typeAndSubmit(a, tc.draft)
			if cmd != nil {
				for _, f := range batchFrames(t, a, cmd) {
					if f.Kind == rpc.FrameRename {
						t.Errorf("%q wrote a FrameRename %+v; the mirror follows only claude's own grammar", tc.draft, f)
					}
				}
			}
			if got := shown(a); strings.Contains(got, nameUsage) || strings.Contains(got, noNameTarget) {
				t.Errorf("%q leaked a /name refusal over a /rename that just passes through:\n%s", tc.draft, got)
			}
		})
	}
}

// batchFrames runs a command - each member of a tea.Batch in turn - and returns
// every frame it wrote to the recorder. sentFrames cannot: it runs the batch
// once and reads before the members it holds have run.
func batchFrames(t *testing.T, a App, cmd tea.Cmd) []rpc.Frame {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command: nothing was sent")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				c()
			}
		}
	}
	return recorderOf(t, a).taken(t)
}
