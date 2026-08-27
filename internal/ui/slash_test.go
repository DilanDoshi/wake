package ui

// The slash layer, from the outside: what Wake takes, and - first - what it
// must not.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Claude Code's own slash commands still reach the agent.
//
// This is the @-overload one door over, and it is the half a router gets wrong
// first: a message that begins with `/` and a command addressed to Wake are
// different things, and the recorded fact is that most of them are the former.
// `/model`, `/clear`, `/compact` and `/context` all survive stream-json mode
// (2026-08-08 stream-json findings), so intercepting one would take a working
// feature away and replace it with "unknown command".
//
// The list is hand-written because Claude's command set is not something this
// build can derive - so it carries a count, which is docs/notes/decisions.md's
// rule for a list that genuinely has to be one.
var claudeCommandsThatMustPassThrough = []string{"/model opus", "/clear", "/compact", "/context", "/help"}

const claudeCommandCount = 5

func TestClaudesOwnSlashCommandsStillReachTheAgent(t *testing.T) {
	if len(claudeCommandsThatMustPassThrough) != claudeCommandCount {
		t.Fatalf("the passthrough list holds %d entries and the count says %d: update both deliberately",
			len(claudeCommandsThatMustPassThrough), claudeCommandCount)
	}
	for _, text := range claudeCommandsThatMustPassThrough {
		t.Run(text, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
			mustBeALiveConversation(t, a)

			if _, _, handled := a.slash(text); handled {
				t.Fatalf("Wake intercepted %q. It is claude's own command and it works in stream-json "+
					"mode, so taking it here replaces a working feature with a refusal", text)
			}
			// And end to end, because "the router declined" is not the same as
			// "the frame went out": submit is where the two meet.
			m, cmd := typeAndSubmit(a, text)
			_ = m
			go func() { _ = runCmdQuietly(cmd) }()
			f := awaitFrame(t, sent)
			if f.Kind != rpc.FrameSend || f.Text != text {
				t.Errorf("submitting %q wrote %+v, want a FrameSend carrying it unchanged", text, f)
			}
		})
	}
}

// mustBeALiveConversation is the premise of every passthrough assertion, read
// back before the behaviour rather than assumed.
//
// "Nothing intercepted it" and "there was nowhere for it to go" produce the
// same silence. A fixture whose focus drifted to the room would route the draft
// through core.Resolve, where an unaddressed message is refused - so the test
// would fail for a reason that has nothing to do with the router, and a fixture
// whose agent had ended would be refused by submit one line further on.
func mustBeALiveConversation(t *testing.T, a App) {
	t.Helper()
	if a.focus == "" {
		t.Fatalf("the fixture is not a focused conversation (focus=%v open=%q): a draft from here is "+
			"routed rather than sent, so nothing it does says anything about the slash router", a.focus, a.focus)
	}
	agent, ok := a.fleet.Agent(a.focus)
	if !ok || agent.State == rpc.StateEnded {
		t.Fatalf("the fixture's agent %q is %+v: submit refuses a draft to an ended session before it "+
			"reaches a socket, so an absent frame would prove nothing", a.focus, agent)
	}
}

// A command Wake does not own and the passthrough list does not name still
// reaches the agent - which is every `.claude/commands/*.md` an operator ever
// wrote.
//
// # Why this is not a fourth list
//
// Its domain is the **complement of the two hand-written lists**, and that is
// the whole of what it adds. Both other tests can be satisfied by a router that
// enumerates: `claudeCommandsThatMustPassThrough` holds five of claude's ~30
// built-ins, so a `claudesOwn` map beside `commands` keeps every one of them
// green while refusing everything else. Two such mutants were built and both
// survived the entire package, static guards included:
//
//	in send.go, one statement past the router:
//	  if t := strings.TrimSpace(text); len(t) > 0 && t[0] == '/' && !claudeAlsoHas(t) {
//	      notice.Report("unknown command"); return a, nil
//	  }
//	in slash, laundering the key through one more assignment:
//	  key := strings.ToLower(word)
//	  for c := range commands { if strings.HasPrefix(c, key) { key = c } }
//
// The first evades the file-scope guard with a rune literal; the second evaded
// the key grammar because a local could hold anything - that half is closed
// now, transitively, and a third mutant (`key := alias(strings.ToLower(word))`)
// is what closed it. **Neither can be answered by widening a list**, because the
// set they break is the one nothing can enumerate *at the moment the question is
// asked* - an operator's own commands directory. (Claude announces it on the
// `init` frame, per session and after the first one; the airlock drops it, and
// slash.go's header says why a per-session list cannot decide a per-keystroke
// question.) So the assertion is over words that are in neither list: no
// allowlist a mutant writes can contain them, because nothing knows what they
// are when it has to decide.
//
// Driven through submit rather than through slash, because both mutants live on
// the two sides of that call and only the keystroke sees both.
func TestAnOperatorsOwnCommandStillReachesTheAgent(t *testing.T) {
	for _, text := range []string{
		"/deploy staging", // a command with an argument
		"/sync-notes",     // a hyphenated one, which is what /add-<name> looks like
		"/resumes",        // a longer word beginning with one of Wake's
		"/r",              // a shorter one, which is what an abbreviation rule would swallow
		// The two the `/add-<agent-name>` ruling is about. `/new-oscar` is one
		// of eight `new-…` commands in this repository's own recorded corpus,
		// so it is exactly what a prefix rule on Wake's `/new` would swallow -
		// that entry is evidence rather than illustration. `/add-dir` is the
		// spelling the founding verb collides with; it is **not** in the corpus
		// and is listed here as an ordinary word Wake does not own, which is
		// all this test needs it to be. Both reach the agent because the router
		// keys on the whole first word. See slash.go for the ruling.
		"/add-dir ~/project",
		"/new-oscar",
		// **claude's whole settings surface**, which reaches it through this
		// rule and through no feature of Wake's. `/config key=value` is a real
		// setter with 36 keys - model, permissionMode, outputStyle, theme,
		// autoCompact - and it is handled by the CLI at zero cost with no model
		// turn (2026-08-14 config-surface findings). Wake owns none of those
		// words, so the passthrough is the entire mechanism: if this test ever
		// fails, configuring Claude Code from a conversation has stopped
		// working and nothing else would say so.
		"/config model=opus",
		"/config permissionMode=plan autoCompact=false",
		"/usage",
	} {
		t.Run(text, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
			mustBeALiveConversation(t, a)

			_, cmd := typeAndSubmit(a, text)
			go func() { _ = runCmdQuietly(cmd) }()
			f := awaitFrame(t, sent)
			if f.Kind != rpc.FrameSend || f.Text != text {
				t.Errorf("submitting %q wrote %+v. Wake's set does not hold this word, so the only "+
					"thing that can have refused it is Wake claiming a command it cannot know about - "+
					"and the operator's symptom is that their own command stopped working with nothing "+
					"to read", text, f)
			}
		})
	}
}

// A draft that merely contains a command's word is a message.
//
// The prefix is half of "is this ours" and the lookup is the other half, and
// this is the half a table can speak to: dropping the prefix test makes every
// sentence beginning with the word `resume` a command, which is a keystroke
// that stops a message and brings back a fleet.
func TestAMessageThatMerelyMentionsACommandIsStillAMessage(t *testing.T) {
	for _, text := range []string{
		"resume alex",         // no prefix at all
		"please /resume alex", // a prefix that is not at the front
		"/resumes",            // a longer word that starts with one of Wake's
		"//resume",            // the prefix twice, which is not the command
	} {
		t.Run(text, func(t *testing.T) {
			fresh(t)
			a := newRoomApp(t).withSize(160, 30).withRoster(
				rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
			)
			if _, _, handled := a.slash(text); handled {
				t.Errorf("Wake took %q as one of its own commands", text)
			}
		})
	}
}

// --- /resume -------------------------------------------------------------

// /resume writes a wake for the session it names, and for nothing else.
func TestResumeBringsBackTheSessionItNames(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateIdle},
	)

	next, cmd, handled := a.slash("/resume alex")
	if !handled {
		t.Fatal("/resume was not taken by the router")
	}
	frames := sentFrames(t, next, cmd)
	if len(frames) != 1 {
		t.Fatalf("/resume alex wrote %d frames, want exactly one: %+v", len(frames), frames)
	}
	if frames[0].Kind != rpc.FrameWake || frames[0].SessionID != "s1" {
		t.Errorf("/resume alex wrote %+v, want a FrameWake for s1", frames[0])
	}
}

// /resume all is one command writing N frames, which is App.write's rule and
// not an implementation detail: bubbletea runs every command on its own
// goroutine and rpc's write lock is process-wide, so twenty commands for one
// keystroke is twenty goroutines on one lock.
func TestResumeAllIsOneCommandWritingOneFramePerParkedSession(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s3", Name: "john", State: rpc.StateIdle},
	)

	next, cmd, handled := a.slash("/resume all")
	if !handled {
		t.Fatal("/resume all was not taken by the router")
	}
	frames := sentFrames(t, next, cmd)
	woke := map[string]bool{}
	for _, f := range frames {
		if f.Kind != rpc.FrameWake {
			t.Errorf("/resume all wrote a %q frame: %+v", f.Kind, f)
		}
		woke[f.SessionID] = true
	}
	if len(frames) != 2 || !woke["s1"] || !woke["s2"] {
		t.Errorf("/resume all woke %v, want exactly s1 and s2: john is running and a wake for a live "+
			"session is a frame the daemon refuses", woke)
	}
	if goroutines := commandCount(cmd); goroutines != 1 {
		t.Errorf("/resume all used %d commands. bubbletea runs every tea.Cmd on its own goroutine and "+
			"rpc's write lock is process-wide, so twenty parked sessions would be twenty goroutines "+
			"queueing on one lock for one keystroke", goroutines)
	}
}

// A name that is not parked is refused with the list, not with silence.
//
// The load-bearing assertion is that no frame was written. Asserting only on
// the sentence leaves the test green when parkedAgents stops filtering, which
// wakes a session that never stopped.
func TestResumeRefusesASessionThatIsNotParkedAndSaysWhatIs(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateIdle},
	)

	next, cmd, handled := a.slash("/resume sydney")
	if !handled {
		t.Fatal("/resume was not taken by the router")
	}
	if cmd != nil {
		t.Fatalf("a wake went out for a session that is running: %+v", sentFrames(t, next, cmd))
	}
	if got := shown(next); !strings.Contains(got, "@sydney is not parked") {
		t.Errorf("the refusal does not name what was asked for:\n%s", got)
	}
	if got := shown(next); !strings.Contains(got, "parked: @alex") {
		t.Errorf("the refusal does not say what *is* parked, so a wrong name costs a second command:\n%s", got)
	}
}

// A bare /resume in the room refuses rather than picking one.
func TestABareResumeInTheRoomAsksWhichOne(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateParked},
	)

	next, cmd, handled := a.slash("/resume")
	if !handled {
		t.Fatal("/resume was not taken by the router")
	}
	if cmd != nil {
		t.Fatalf("the room picked one for the operator: %+v", sentFrames(t, next, cmd))
	}
	got := shown(next)
	if !strings.Contains(got, noResumeTarget) {
		t.Errorf("a bare /resume in the room does not say how to address it:\n%s", got)
	}
	if !strings.Contains(got, "@alex") || !strings.Contains(got, "@sydney") {
		t.Errorf("the refusal does not name what could be brought back:\n%s", got)
	}
}

// A bare /resume in a parked conversation is unambiguous, because the pane
// names its recipient in its own header - and it brings back that one alone.
func TestABareResumeInAParkedConversationBringsBackThatOne(t *testing.T) {
	a := newRoomApp(t).WithOpenDM("s1", "alex").withSize(160, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateParked},
	)

	next, cmd, handled := a.slash("/resume")
	if !handled {
		t.Fatal("/resume was not taken by the router")
	}
	frames := sentFrames(t, next, cmd)
	if len(frames) != 1 || frames[0].Kind != rpc.FrameWake || frames[0].SessionID != "s1" {
		t.Errorf("a bare /resume in @alex's conversation wrote %+v, want one FrameWake for s1 alone: "+
			"the other parked session belongs to /resume all", frames)
	}
}

// A bare /resume in a conversation whose agent is running is not that agent.
//
// The DM is unambiguous about *who*, which is why the bare form is allowed
// there at all - it says nothing about whether they are parked. Waking a live
// session is a frame the daemon refuses, and the operator's symptom is a
// refusal about the session they were looking at rather than the one they meant.
func TestABareResumeInALiveConversationDoesNotWakeIt(t *testing.T) {
	a := newRoomApp(t).WithOpenDM("s1", "alex").withSize(160, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateParked},
	)

	next, cmd, handled := a.slash("/resume")
	if !handled {
		t.Fatal("/resume was not taken by the router")
	}
	if cmd != nil {
		t.Fatalf("a wake went out for the running session the operator happened to be reading: %+v",
			sentFrames(t, next, cmd))
	}
	if got := shown(next); !strings.Contains(got, "parked: @sydney") {
		t.Errorf("the refusal does not name the one that could be brought back:\n%s", got)
	}
}

// A name is resolved exactly, the way Fleet.ByName is.
//
// A prefix match belongs to `wake attach`, where a person is typing at a shell
// and can see what came back. Here it picks a conversation for somebody, and
// they type into it.
func TestResumeResolvesANameExactlyRatherThanByPrefix(t *testing.T) {
	for _, who := range []string{"ale", "alexander", "@ale"} {
		t.Run(who, func(t *testing.T) {
			fresh(t)
			a := newRoomApp(t).withSize(160, 30).withRoster(
				rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
			)
			next, cmd, handled := a.slash("/resume " + who)
			if !handled {
				t.Fatal("/resume was not taken by the router")
			}
			if cmd != nil {
				t.Fatalf("%q resolved to a parked session anyway: %+v", who, sentFrames(t, next, cmd))
			}
		})
	}
}

// /resume with nothing parked says so - and names the two keys that park one,
// which it may do now that both of them do.
//
// This is the hint line's rule one surface over, and it has been read in both
// directions here. For one task the sentence named no key at all, because ⌃C
// detached and advertising it as the park key would have cost whoever followed
// it the window they were reading. Now that ⌃C parks and ⌃Q parks the fleet,
// naming them is the *other* half of the same rule: a way to park that nothing
// tells you about is a feature nobody uses.
//
// **The assertion derives what a key does from the legend rather than restating
// it.** legendEntries is the one place that says what this build's keys do and
// it is held to App.key's own switch by
// TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed, so a sentence
// checked against it cannot advertise a key that has stopped working - which is
// exactly how this sentence went wrong the first time somebody wrote it.
func TestResumeWithNothingParkedNamesTheKeysThatPark(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).withAgents("alex")

	next, cmd, handled := a.slash("/resume alex")
	if !handled {
		t.Fatal("/resume was not taken by the router")
	}
	if cmd != nil {
		t.Fatalf("a wake went out with nothing parked: %+v", sentFrames(t, next, cmd))
	}
	if got := shown(next); !strings.Contains(got, noParkedSessions) {
		t.Errorf("/resume against a running fleet does not say nothing is parked:\n%s", got)
	}
	for _, glyph := range []string{"⌃C", "⌃Q"} {
		if !strings.Contains(noParkedSessions, glyph) {
			t.Errorf("the sentence does not name %s. Somebody reading \"nothing is parked\" is somebody "+
				"who wants to know how one gets parked, and a way to park that nothing tells you about "+
				"is a feature nobody uses", glyph)
			continue
		}
		what, bound := legendLabel(glyph)
		if !bound {
			t.Errorf("the sentence names %s and the legend does not, so nothing here says that key still "+
				"exists. Bind the key first, then say so - which is the rule the hint line carries, and "+
				"it was this sentence that broke it last time", glyph)
			continue
		}
		if !strings.Contains(what, "park") {
			t.Errorf("the sentence offers %s as a way to park and the legend says it does %q. One of the "+
				"two is lying, and the expensive direction is this one: somebody presses it expecting a "+
				"park and gets whatever the legend actually describes", glyph, what)
		}
	}
}

// legendLabel is what the legend says a glyph does, which is the only place in
// this build that says so and is itself held to App.key's switch.
func legendLabel(glyph string) (string, bool) {
	for _, e := range legendEntries {
		if e.glyph == glyph {
			return e.what, true
		}
	}
	return "", false
}

// The daemon's refusal reaches the operator as the daemon wrote it.
//
// Every refusal `unpark` can produce names *when* the operator can act rather
// than only that they cannot - something else is running under the id, this
// daemon does not know where the session ran, the daemon is shutting down - and
// that "when" is the only useful half of the sentence. This client decides none
// of it, on purpose (resume's header says why), so the one thing it owes those
// sentences is not to replace them with a local "could not resume".
//
// The text here stands in for the daemon's rather than copying it: what is
// under test is that the channel preserves a sentence, and a second spelling of
// the daemon's wording in this package would be the copy that goes stale.
func TestADaemonsWakeRefusalReachesTheOperatorUnchanged(t *testing.T) {
	const refusal = "something is already running under that id: wake it once that process has ended"

	a := newRoomApp(t).withSize(200, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
	)
	next, _, handled := a.slash("/resume alex")
	if !handled {
		t.Fatal("/resume was not taken by the router")
	}
	next = next.applyFrame(rpc.Frame{Kind: rpc.FrameError, SessionID: "s1", Text: refusal})

	if got := shown(next); !strings.Contains(got, refusal) {
		t.Errorf("the daemon's refusal did not reach the operator whole. It is the half that says when "+
			"the wake could work, and nothing on this side of the socket can reconstruct it:\n%s", got)
	}
}

// ↵ asks the router, and asks it before the send.
//
// The passthrough test drives submit too and cannot see this: with the router
// deleted from submit entirely, every one of claude's commands still reaches
// the agent and all five subtests stay green. What that deletion costs is the
// other half - `/resume` typed into a parked conversation becomes a message to
// a process that is not running - so the wiring needs a command going the other
// way to be visible at all.
func TestSubmittingAWakeCommandDoesNotSendItToTheAgent(t *testing.T) {
	a := newRoomApp(t).WithOpenDM("s1", "alex").withSize(160, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
	)

	m, cmd := typeAndSubmit(a, "/resume")
	frames := sentFrames(t, m.(App), cmd)
	if len(frames) != 1 || frames[0].Kind != rpc.FrameWake || frames[0].SessionID != "s1" {
		t.Fatalf("↵ on /resume wrote %+v, want one FrameWake for s1: a FrameSend here is the command "+
			"typed at an agent whose process is not running", frames)
	}
	if draft := m.(App).composer().Value(); draft != "" {
		t.Errorf("the draft survived the command it ran: %q", draft)
	}
}

// --- helpers -------------------------------------------------------------

// typeAndSubmit types a draft into the focused composer and presses ↵, the way
// Bubble Tea delivers both.
//
// It is bangkey_test.go's shape - the interception living in submit is exactly
// what "typing /model sends it to the model" would look like again if someone
// removed it, and only the key can see that - built out of withDraft rather
// than beside it, because a second way of typing is a second thing to keep
// working.
func typeAndSubmit(a App, text string) (tea.Model, tea.Cmd) {
	return a.withDraft(text).Update(tea.KeyMsg{Type: tea.KeyEnter})
}

// withRoster puts an exact fleet report in, states and all.
//
// withAgents makes everybody idle, and every assertion in this file is about a
// state - so a fixture built with that one would be asserting about a fleet
// with nothing parked in it.
func (a App) withRoster(sessions ...rpc.SessionStatus) App {
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running:  true,
		Sessions: sessions,
	}})
}

// withParkBook is a fleet holding **nothing**, beside a park book that names
// these - which is every fleet after a ⌃Q, and the shape the roster is empty in.
func (a App) withParkBook(parked ...rpc.SessionStatus) App {
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Parked:  parked,
	}})
}

// --- /resume out of an empty room ----------------------------------------

// A session in the park book and in no roster is still resumable by name.
//
// This is the whole of what makes the empty room safe. Nothing is in the fleet
// after a ⌃Q - no row, no name claimed, no cursor - so the *only* thing that can
// reach one of those sessions is /resume resolving a name against Status.Parked.
// A fold that dropped that list would leave a fleet nobody can address and
// nothing on screen saying so, which is indistinguishable from having lost it.
func TestResumeReachesASessionThatIsOnlyInTheParkBook(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).withParkBook(
		rpc.SessionStatus{ID: "s1", Name: "kwame", State: rpc.StateParked},
	)

	next, cmd, handled := a.slash("/resume kwame")
	if !handled {
		t.Fatal("/resume was not taken by the router")
	}
	frames := sentFrames(t, next, cmd)
	if len(frames) != 1 || frames[0].Kind != rpc.FrameWake || frames[0].SessionID != "s1" {
		t.Fatalf("/resume kwame wrote %+v, want one FrameWake for s1: the park book is the only index "+
			"a name can resolve against once the roster is empty", frames)
	}
}

// The park book survives an ordinary event arriving, which is what Fleet.copy
// has to carry it for.
//
// Observe copies the fleet for every event that moves an agent, and a copy that
// rebuilt only the agent map would drop the book on the first frame after the
// status report - so /resume would work for a moment and then stop, with nothing
// on screen having changed. This mutant survived the rest of this file: the
// other two tests fold a report and ask immediately, so neither of them ever
// reaches a second copy.
func TestTheParkBookSurvivesAnEventArriving(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30).withParkBook(
		rpc.SessionStatus{ID: "s1", Name: "kwame", State: rpc.StateParked},
	)

	// An event from somebody else entirely, which is the ordinary case: a room
	// left open beside a working fleet takes thousands of these.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s9", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "working on it",
	}})

	if got := len(a.fleet.Parked()); got != 1 {
		t.Fatalf("the park book holds %d records after one unrelated event, want 1: /resume resolves "+
			"names against this list and nothing else, so losing it on the first frame leaves a fleet "+
			"nobody can address and nothing saying so", got)
	}
	next, cmd, _ := a.slash("/resume kwame")
	frames := sentFrames(t, next, cmd)
	if len(frames) != 1 || frames[0].SessionID != "s1" {
		t.Errorf("/resume kwame wrote %+v after an event arrived, want one wake for s1", frames)
	}
}

// And a bare /resume lists them, which is the only way to discover a name.
//
// The offer line that used to name them on the first frame was removed with the
// restore, so nothing else on any surface says what is parked: not the roster,
// not the awareness strip, not `wake status`. If this list goes, a fleet parked
// by ⌃Q is reachable only by somebody who wrote the names down.
func TestABareResumeListsAParkBookNothingElseNames(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	a := newRoomApp(t).withSize(160, 30).withParkBook(
		rpc.SessionStatus{ID: "s1", Name: "kwame", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "jonas", State: rpc.StateParked},
	)

	if _, _, handled := a.slash("/resume"); !handled {
		t.Fatal("/resume was not taken by the router")
	}
	n, ok := notice.Latest()
	if !ok {
		t.Fatal("a bare /resume in a room with a full park book said nothing at all")
	}
	for _, want := range []string{"kwame", "jonas"} {
		if !strings.Contains(n.Text, want) {
			t.Errorf("a bare /resume said %q, which does not name %q: with the roster empty this is "+
				"the only surface that can tell somebody what there is to bring back", n.Text, want)
		}
	}
}

// A woken conversation says what it is, once, when it comes back.
//
// `wake attach` has said this since Phase 1 — *"What it said before now is not
// here - claude keeps the transcript, Wake does not"* — because a pane that
// opens empty over a session with an hour of history behind it reads as a
// session that lost it. `/resume` produced exactly that surprise and said
// nothing: "bringing @alex back…", and then an empty pane.
//
// It is the same sentence for the same reason, so it is one sentence in one
// place rather than two that can drift. `cmd/wake` had it inline; `TranscriptNotice`
// is now the only copy, and `wake attach` reads it from here.
//
// # Why the report and not the keypress
//
// parkArrived's reason, and ⌃F's before it: the daemon refuses a wake for real
// reasons — something already holds the id, the record carries no directory —
// and a sentence about a conversation that has come back is a lie until it has.
func TestAWokenConversationSaysTheTranscriptIsNotHere(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).
		withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked})

	m, _ := typeAndSubmit(a, resumeVerb+" alex")
	a = m.(App)

	if got := latestNotice(t); strings.Contains(got, "transcript") {
		t.Errorf("the keypress says %q, which describes a conversation that has not come back yet. "+
			"A wake the daemon refuses makes that a sentence the next frame contradicts", got)
	}

	// The value is discarded on purpose: what this asserts is the sentence the
	// fold *reported*, not the model it returned.
	_ = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true,
		Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}}})

	got := latestNotice(t)
	if !strings.Contains(got, "transcript") {
		t.Errorf("a woken session came back and the notice is %q. The pane opens empty over a "+
			"conversation with an hour behind it, and nothing says the history is claude's rather "+
			"than gone", got)
	}
	if !strings.Contains(got, "alex") {
		t.Errorf("the notice is %q and does not name the session it is about: at 30 agents a sentence "+
			"about an unnamed conversation is about none of them", got)
	}
}

// And the two surfaces say it with one sentence, so neither can drift.
func TestAttachAndResumeShareTheTranscriptSentence(t *testing.T) {
	if TranscriptNotice("alex") == "" {
		t.Fatal("TranscriptNotice is empty, so the assertion below holds over nothing")
	}
	if !strings.Contains(TranscriptNotice("alex"), "alex") {
		t.Errorf("TranscriptNotice(%q) = %q and does not name the session", "alex", TranscriptNotice("alex"))
	}
}
