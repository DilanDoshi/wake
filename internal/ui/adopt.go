package ui

// `/adopt` - opening a group chat among sessions Wake never started.
//
// # The founding sentence, and the half of it that was still a shell command
//
// *"If you have a bunch of sessions open in terminals scattered or in cmux, be
// able to open a group chat among them - select which ones you would like to
// add - and then query them via the group chat."*
//
// `wake import` built the adopting. It is a shell verb, so the operator who is
// already in the room has to leave it, run a command, and come back - and it
// takes **one** session per invocation, so there is no moment at which anybody
// chooses a *set*. Both halves of that sentence are about a room, so both
// belong to a command typed in one.
//
// # The word is `adopt` because `import` is claude's, and that is measured
//
// The obvious spelling is `/import`, matching the shell verb. It is refused:
// `slash_commands` on claude's `init` frame advertises **`import`**, in 45 of
// the 45 recorded files that carry the key, so taking it would replace a
// working command with a refusal and the operator's only symptom would be that
// the command stopped doing what it used to. That is the fence
// TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising exists for, and
// it caught this word before anything rested on it - the second time the corpus
// has picked the vocabulary (`/name` and `/task` are the first, because
// `rename` is in it too).
//
// `adopt` is in none of the 133. It is also the word this build already used
// for the action - internal/daemon/import.go's header is *"Adopting a session
// Wake never started"* - so the shell verb and the room's verb differ in
// spelling and agree in meaning. TestImportIsNotWakesWordBecauseTheCorpusShowsClaudeAdvertisingIt
// pins the finding in both directions, because half of it rots on its own.
//
// # This package cannot read this machine, so it is handed a way to ask
//
// Discovery lives in internal/daemon, which internal/ui may not import, and it
// reads claude's **on-disk transcript** - a second Claude format contained to
// one file across the whole tree. A copy here would be both the parallel
// implementation this project forbids and a third place that knows that format.
//
// So Sessions is a seam, injected by cmd/wake, which is Dialer's arrangement
// and Dialer's argument read one door over: *"this package may not dial"*
// becomes *"this package may not walk ~/.claude/projects"*. cmd/wake answers it
// with the two functions `wake import` already uses, so there is one picker and
// one resolver in the build rather than two.
//
// **And it is asked off the draw goroutine.** Discovery walks every transcript
// under ~/.claude/projects - 428 files on the recording machine - and Bubble
// Tea has one Update goroutine and it renders. The router hands back a tea.Cmd
// and folds the answer when it arrives, which is the rule inbox.go already
// carries for the socket.
//
// # A row is still not an offer
//
// The listing says which conversations exist on disk and nothing about whether
// any of them may be imported: a transcript is not evidence about a process,
// and 2026-08-12 findings §5 counted live `claude` processes whose entire argv
// is the word `claude`. So this side decides nothing. It resolves what was
// typed to a session id - *which one did you mean* - and asks; the daemon
// re-decides across five refusals every time, and the operator reads its
// sentence rather than a local guess at it. In particular a session the picker
// could prove **no directory** for is still asked for, because the refusal it
// earns names what to do about it.
//
// # Where the picker is drawn, and why it is never a DM
//
// The room, whichever pane has the keys. A bang goes to the conversation it was
// typed into because a bang *is* addressed to that conversation - `!git status`
// runs in that agent's own directory. `/adopt` is addressed to Wake, which is
// the same division new.go draws for a directory, and a machine-wide listing
// pasted into a DM would make Wake author a turn inside somebody's conversation
// with claude. The sessions it names arrive in the room when they are adopted;
// the listing belongs where they will.
//
// It is fenced through bangBlock for bangBlock's reason, which is load-bearing
// rather than cosmetic: a transcript renders an echoed user turn as markdown
// and markdown joins consecutive lines into a paragraph, so an unfenced columnar
// listing arrives as one run-on line.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// refusedAdoptWord is the word this command is deliberately *not* called, held
// as a constant so the test that reads the corpus asserts about the same string
// this file's header argues about.
const refusedAdoptWord = "import"

const (
	// cannotSeeMachine is a model with no Sessions seam. Every unit test in
	// this package builds one, and so would any future caller that has not
	// wired one up - so it is a real shape and it says so, rather than
	// producing the silence of a command that looks like it worked.
	cannotSeeMachine = "wake cannot read the claude sessions on this machine from here, so there is nothing to adopt from"

	// adoptStillReading is the second /adopt while the first has not answered.
	// Said for cannotSeeMachine's reason: the read is bounded by nothing, so a
	// stalled mount holds this guard for the life of the window, and a command
	// that silently does nothing is indistinguishable from one that worked.
	adoptStillReading = "wake is still reading this machine's sessions, and nothing bounds that read; /adopt again if it answers"

	// pickerDrawn is said on the notice row when the listing lands, because
	// below 120 columns the room is not drawn at all - so an operator in a
	// conversation would otherwise see nothing happen.
	pickerDrawn = "the claude sessions on this machine are listed in the room"

	// adoptAsked and adoptAskedN are said when the frames go out rather than
	// when they are confirmed: the daemon refuses an import for real reasons -
	// already in this fleet, no provable directory, a process holding the id -
	// and the operator should know the command was read either way. The
	// arrival is startArrived's, which is where a fork and a `/new` also say
	// what they got.
	adoptAsked  = "adopting session %s…"
	adoptAskedN = "adopting %d sessions…"

	// adoptCopies is the half of `cmd/wake`'s importedNotice that the room's
	// own arrival sentence does not say, and it is said **on the ask**.
	//
	// An adopted session arrives through startArrived, whose sentence is
	// SnapshotNotice - and it takes the unnamed arm, because an import's parent
	// is a session id no fleet holds. So the operator is told it is a snapshot
	// and is *not* told the thing that is specific to a stranger's session:
	// Wake cannot tell whether the original is still open in a terminal.
	// 2026-08-12 findings §5 counted live `claude` processes whose entire argv
	// is the word `claude`, which core.SessionArgvMarkers cannot match, and
	// that is precisely the shape this command exists for.
	//
	// It is on the ask rather than on the arrival for the card arm's reason:
	// the moment an operator can still decide otherwise is before the frame is
	// written. Afterwards there is a `claude` running.
	adoptCopies = "an adopted session is a copy, and wake cannot tell whether the original is still open somewhere - if it is, the two carry on separately"

	// adoptFailed names the write that could not happen, so the notice row
	// says which command was typed rather than only what the socket said.
	adoptFailed = "adopting a session"

	// shortIDLen is how much of a session id a listing prints and an operator
	// copies. Spelled here because the sentences below quote an id back.
	shortIDLen = 8
)

// Sessions is how this client asks about the claude sessions on this machine
// that Wake did not start.
//
// Two questions and no third: **what is there**, and **which one did they
// mean**. Neither is *may this be imported* - that is the daemon's, asked over
// the socket, and a client that answered it would be the second copy of a
// decision whose whole value is being made once.
//
// It is an interface rather than two function fields because the two answers
// come from one walk of one directory and one implementation owns both.
type Sessions interface {
	// Listing is the picker as the operator reads it: what is on this machine,
	// newest first, bounded for a pane.
	Listing() (string, error)

	// Resolve turns what was typed - a short id off the listing, or a whole
	// one - into full session ids, in the order they were typed. It answers
	// with an error naming the first thing it could not resolve.
	Resolve(typed []string) ([]string, error)
}

// WithSessions returns an App that can see the claude sessions on this machine
// which Wake did not start. Without one, `/adopt` says it cannot look.
//
// It lives here rather than beside WithDialer because the type it installs and
// the argument for the type are both in this file's header - and the field it
// writes is the only part of this feature that App itself has to hold.
func (a App) WithSessions(s Sessions) App { a.sessions = s; return a }

// adoptedMsg is what the machine answered, folded by App.Update.
//
// One message for both questions, because they are one command with an
// argument. `listing` is set when nothing was named and `ids` when something
// was; `err` supersedes both.
type adoptedMsg struct {
	listing string
	ids     []string
	err     error
}

// adopt takes a draft of `/adopt [<id> …]` and asks this machine about it.
//
// It writes no frame and mints no id: everything here happens before the answer
// exists. What it returns is the command that goes and looks.
func (a App) adopt(arg string) (App, tea.Cmd) {
	if a.sessions == nil {
		notice.Report("%s", cannotSeeMachine)
		return a, nil
	}
	if a.adoptOut {
		// The draft is kept, unlike the started case below: nothing bounds the
		// read this is waiting on, so the words may be all that survives it.
		notice.Report("%s", adoptStillReading)
		return a, nil
	}
	a.adoptOut = true
	return a.clearDraft(), askMachine(a.sessions, strings.Fields(arg))
}

// askMachine is the walk of ~/.claude/projects, on its own goroutine.
func askMachine(s Sessions, typed []string) tea.Cmd {
	return func() tea.Msg {
		if len(typed) == 0 {
			listing, err := s.Listing()
			return adoptedMsg{listing: listing, err: err}
		}
		ids, err := s.Resolve(typed)
		return adoptedMsg{ids: ids, err: err}
	}
}

// adoptArrived folds what the machine said: a refusal, a listing, or a set to
// adopt.
//
// **The whole set or none of it.** A name that does not resolve refuses every
// name beside it, and the reason is what a retry costs: the daemon refuses a
// source that is already in this fleet, so adopting two of three and refusing
// the third would leave a command the operator cannot simply correct and
// retype. Refusing the set keeps the draft's own words the unit of the
// decision, which is what *"select which ones"* asks for.
func (a App) adoptArrived(m adoptedMsg) (App, tea.Cmd) {
	a.adoptOut = false
	switch {
	case m.err != nil:
		notice.Report("%v", m.err)
		return a, nil
	case len(m.ids) == 0:
		notice.Report("%s", pickerDrawn)
		return a.showPicker(m.listing), nil
	default:
		return a.adoptAll(m.ids)
	}
}

// showPicker puts the listing in the room, as a preformatted block.
func (a App) showPicker(listing string) App {
	a = a.withRoom(a.room.Append(core.Event{
		Kind:   core.KindUserText,
		Echoed: true,
		Text:   bangBlock(listing),
	}, Agent{}))
	return a
}

// adoptAll asks the daemon for one import per session the operator chose.
//
// **Every id this waits on is one it minted here**, and that is `wake fork`'s
// rule for `wake fork`'s reason: awaitSpawn and this wait both match a refusal
// on the *new* session's id, the daemon addresses every import refusal to it,
// and neither wait has a deadline by design. A client waiting on the source's
// id is therefore not refused - it waits forever, with nothing on screen, which
// looks exactly like a daemon that is thinking.
//
// One write for the whole set, which is App.write's rule: bubbletea runs every
// tea.Cmd on its own goroutine and rpc's write lock is process-wide, so ten
// sessions built as ten commands would be ten goroutines queueing on one lock
// for one keystroke.
func (a App) adoptAll(sources []string) (App, tea.Cmd) {
	frames := make([]rpc.Frame, 0, len(sources))
	for _, source := range sources {
		minted := uuid.NewString()
		frames = append(frames, rpc.Frame{
			Kind:      rpc.FrameImport,
			SessionID: minted,
			ParentID:  source,
		})
		a = a.awaitingStart(minted)
	}
	notice.Report("%s %s", adoptingLine(sources), adoptCopies)
	return a, a.write(adoptFailed, frames...)
}

// adoptingLine names one session rather than counting it, whichever arm asked -
// bringBack's rule, for bringBack's reason: "1 sessions" is a sentence nobody
// writes, and at one the id is the more useful half anyway.
func adoptingLine(sources []string) string {
	if len(sources) == 1 {
		return fmt.Sprintf(adoptAsked, shortSource(sources[0]))
	}
	return fmt.Sprintf(adoptAskedN, len(sources))
}

// shortSource is the eight characters the picker prints and the operator
// copied, so the sentence quotes back what they typed rather than a UUID they
// have not seen.
func shortSource(id string) string {
	if len(id) < shortIDLen {
		return id
	}
	return id[:shortIDLen]
}
