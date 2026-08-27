// Which session did they mean.
//
// One search over two spaces - the names an agent is talked about by and the
// session ids it is addressed by - and the refusal when a word reaches more
// than one of them. Split from attach.go, which owns getting a connection and
// running the conversation over it; this owns the question that has to be
// answered before either.

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// resolveSession is the fleet's account of one session, or why there is not
// one - and the whole report it was found in.
//
// Every failure here is a sentence a person can act on, because this is the
// only place that knows the difference between the ways a lookup fails: no
// daemon, a fleet whose daemon died, and no such session.
//
// What it does **not** decide is whether the session it found is one the
// caller can use. `wake attach` needs a live one; `wake fork` is perfectly
// happy with an ended one, because a fork resumes a transcript on disk rather
// than a process. Splitting that out is what stops fork from needing a second
// matcher - and the ambiguity rule matchSession implements is precisely the
// thing that must not exist twice.
//
// The report comes back because it was already fetched. It answers one
// question about one session and describes every one of them, which is exactly
// what the room opens with.
func resolveSession(socket, want string) (rpc.SessionStatus, rpc.Status, error) {
	st, err := daemon.Status(socket)
	if err != nil {
		return rpc.SessionStatus{}, rpc.Status{}, fmt.Errorf("ask what is running: %w", err)
	}
	if !st.Running {
		if len(st.Sessions) > 0 {
			return rpc.SessionStatus{}, rpc.Status{}, fmt.Errorf("no daemon is running, and a previous one left %s behind with nothing holding them: check `wake status`",
				agents(len(st.Sessions)))
		}
		return rpc.SessionStatus{}, rpc.Status{}, errors.New("no daemon is running, so there is nothing to attach to")
	}

	sess, err := matchSession(st, want)
	if err != nil {
		return rpc.SessionStatus{}, rpc.Status{}, err
	}
	return sess, st, nil
}

// liveSession is resolveSession plus the two states `wake attach` cannot open
// a conversation with.
func liveSession(socket, sessionID string) (rpc.SessionStatus, rpc.Status, error) {
	sess, st, err := resolveSession(socket, sessionID)
	if err != nil {
		return rpc.SessionStatus{}, rpc.Status{}, err
	}
	switch sess.State {
	case rpc.StateEnded:
		return rpc.SessionStatus{}, rpc.Status{}, fmt.Errorf("session %s has ended%s, so there is nothing to attach to", shortID(sess.ID), endedBecause(sess))
	case rpc.StateParked:
		// Refused for ended's reason - the process is gone, and a composer over
		// one swallows every keystroke - and answered differently, because a
		// park is meant to be taken back and an ending is not.
		//
		// **Both routes are named, and which comes first is the whole of it.**
		// Somebody typing `wake attach <parked>` wants *this* conversation back,
		// so `/resume` leads: it keeps the id, the name, the label and the
		// directory. `wake fork` is offered second as the other thing they might
		// have meant, because it makes a **second** session - the original stays
		// parked, another name is spent and another row is written.
		//
		// This comment said "waking is not built" for three tasks after waking
		// was built, and the sentence below it said so too. It was true when it
		// was written and `park_test.go`'s guard held it in place by forbidding
		// the word `resume` in the refusal, which made writing the correct
		// message a build failure. That is rung 7 of docs/notes/decisions.md and
		// this is the instance it was found on: a guard perfectly able to fail,
		// still enforcing a premise another task had falsified.
		//
		// There is still no `wake resume` **shell** verb, which is why the
		// sentence has to name the in-TUI command and say where it is typed -
		// and it is what the inverted guard now forbids instead.
		//
		// Fork is offered rather than merely mentioned because the daemon really
		// does allow it on a parked parent: a parked process has exited, and an
		// exited parent is what every recorded fork resumed.
		return rpc.SessionStatus{}, rpc.Status{}, fmt.Errorf(
			"session %s is parked, so its process has stopped and there is no conversation to attach to; "+
				"run `wake` and type `/resume %s` to bring this conversation back, "+
				"or `wake fork %s` to branch what it said into a new session",
			shortID(sess.ID), displayName(sess), displayName(sess))
	case rpc.StateOrphaned:
		return rpc.SessionStatus{}, rpc.Status{}, fmt.Errorf("session %s has no daemon holding it: check `wake status`", shortID(sess.ID))
	default:
		return sess, st, nil
	}
}

// forkParent is resolveSession, and it applies no state rule of its own.
//
// That is the finding rather than an omission, and it is why this function
// still exists: it is where somebody would put one.
//
// An **ended** session is forkable. claude persists every session to
// ~/.claude/projects/<slug>/<uuid>.jsonl and a fork resumes that file rather
// than a process; the recording spike forked exited parents throughout. That
// is the whole reason resolveSession was split out of liveSession, which
// refuses an ended session and is right to.
//
// Every other state resolves here and is judged **over there**. The daemon is
// the only process that can see whether a parent is mid-turn, mid-tool or
// blocked, and a copy of that rule on this side would be the parallel
// implementation this project forbids - stale the day forkRefusal changes, and
// stale in the direction that refuses forks the daemon would allow.
//
// An orphan is not an exception either, and this used to say it was. A session
// is reported StateOrphaned only by daemon.FleetOnDisk, which is what
// daemon.Status returns when **the dial fails** - so that report carries
// Running false, and resolveSession has already refused it above with a
// sentence about the fleet rather than about one session. No status a running
// daemon produces can contain the state: fleet() is the only writer of
// Running true and its rows come from agent.stateLocked, which cannot return
// it. The arm that was here could not fire, and the guard built over it had
// begun to pin it - see forkguard_test.go, which now derives the reachable
// states from that producer instead of from rpc's constant block.
func forkParent(socket, who string) (rpc.SessionStatus, error) {
	sess, _, err := resolveSession(socket, who)
	if err != nil {
		return rpc.SessionStatus{}, err
	}
	return sess, nil
}

// matchSession finds one session by name, by id, or by a unique prefix of
// either.
//
// A prefix because a session id is a UUID and `wake status` prints the first
// few characters of one - which was fine while nothing took an id as an
// argument, and would be a copy-paste of 36 characters now that something does.
// A name because `wake attach 4f78b3d7` is not an interface anybody wants: the
// fleet is talked about by name, so it is reached by name.
//
// # Why names and ids are one search rather than two
//
// Because a second matcher would need its own answer for ambiguity, and the
// ambiguity is *between* the two spaces rather than inside either. A pooled
// name can never be a hex string - names_test.go holds that - but a name a
// person chose can be, and `wake attach abc` against a session called abc and a
// session whose id starts abc has to refuse rather than pick. One search over
// both is what makes that a refusal instead of a race between two matchers.
//
// # The two passes, in order
//
// A whole name first, so that a session called `sam` is reachable while a
// `sammy` exists - a single pass would call that ambiguous and leave sam
// unreachable. Then prefixes of either space.
//
// The rule the order states is that exactness beats partiality: a word that is
// one session's whole name and only the front of another's id resolves to the
// name, because one of those readings is complete and the other is a
// coincidence. When neither reading is complete - a prefix of a name against a
// prefix of an id - nothing distinguishes them and it is refused.
//
// There is deliberately no third pass for a whole *id*. There was one, and
// mutating it away changed no answer anywhere: a session id is a 36-character
// UUID and a name is capped well below that, so the only thing that can equal a
// whole id is that id, and a whole id is a prefix of itself. A branch that
// cannot decide anything is a branch no test can hold, so it is gone rather
// than commented.
func matchSession(st rpc.Status, want string) (rpc.SessionStatus, error) {
	if want == "" {
		return rpc.SessionStatus{}, fmt.Errorf("which session?\n%s", runningSessions(st))
	}
	if tiers := candidates(st, namedExactly(want)); matched(tiers) > 0 {
		return pickOne(want, tiers, st)
	}
	return pickOne(want, candidates(st, prefixedBy(want)), st)
}

// matched is how many sessions one pass found across every tier.
func matched(tiers [][]rpc.SessionStatus) int {
	n := 0
	for _, tier := range tiers {
		n += len(tier)
	}
	return n
}

// namedExactly matches a session whose name is the whole of what was typed.
// Folded, because a name is something a person types and `wake attach Sydney`
// meaning a different agent from `wake attach sydney` would be a trap - the
// daemon lower-cases every name it assigns.
func namedExactly(want string) func(rpc.SessionStatus) bool {
	return func(s rpc.SessionStatus) bool { return s.Name != "" && strings.EqualFold(s.Name, want) }
}

// prefixedBy matches a session by the front of its id or the front of its name.
func prefixedBy(want string) func(rpc.SessionStatus) bool {
	lower := strings.ToLower(want)
	return func(s rpc.SessionStatus) bool {
		return strings.HasPrefix(s.ID, want) ||
			(s.Name != "" && strings.HasPrefix(strings.ToLower(s.Name), lower))
	}
}

// candidates splits what matched into tiers, best answer first: what can be
// attached to, what is parked, and what has merely ended.
//
// **Three tiers rather than two, because there are three kinds of answer and
// only the first two are things anybody can act on.** A live session is one you
// can open. A parked one is not - liveSession refuses it - but it can be forked
// today and woken later, and it is still holding its name, so a word that
// reaches one is a word that reached something real. A remembered ending is
// none of that: it is in the report only so a refusal can say *that one ended*,
// and its name has already gone back to the pool.
//
// The tiers exist for pickOne's argument rather than for tidiness, and the
// collision they answer is reachable. A name returns to the pool when a session
// ends while the ending stays in the report, so the same word can name a
// remembered ending and a later session that has since been parked. Flattened
// to two tiers those two land together and are refused as ambiguous - over a
// listing that shows neither, because runningSessions filters out both.
//
// Note which collision is **not** the reason, because the first version of this
// comment claimed it: a live session and a parked one cannot share a name at
// all. completePark never releases the name and nameRegistry.claim refuses one
// that is taken, which internal/daemon asserts directly. A fixture with both was
// a verdict over an input no producer can make - this repository's own rung 4,
// arriving inside the change that celebrated catching it.
func candidates(st rpc.Status, match func(rpc.SessionStatus) bool) [][]rpc.SessionStatus {
	var live, parked, ended []rpc.SessionStatus
	for _, s := range st.Sessions {
		switch {
		case !match(s):
		case s.State == rpc.StateParked:
			parked = append(parked, s)
		case s.State == rpc.StateEnded:
			ended = append(ended, s)
		default:
			live = append(live, s)
		}
	}
	return [][]rpc.SessionStatus{live, parked, ended}
}

// pickOne resolves one pass's matches, or refuses to.
//
// **The best tier that matched anything answers, and only that tier can be
// ambiguous.** This is not a tie-break for its own sake. A status report carries
// recent endings for up to recentEndings of them, so an agent that ended a
// minute ago is still in the list this searches - and something unambiguous to
// the person typing it would be refused as ambiguous because of a row they can
// no longer attach to anyway. That is true of a name as well as a prefix: a name
// goes back to the pool when its session ends, so the same word can be a live
// agent and a remembered one at once. Only when nothing better matches does an
// ending answer, and then only so the refusal can say "that one ended" rather
// than "no such session".
//
// The tiers are walked rather than spelled out, which is what made the third one
// free. Written as a switch per tier it is four cases becoming six, and a sixth
// case nobody writes is a tier that silently stops being reachable.
//
// Ambiguity is an error rather than a guess: the things being attached to are
// conversations, and picking one for somebody is not a recoverable mistake -
// they type into it.
func pickOne(want string, tiers [][]rpc.SessionStatus, st rpc.Status) (rpc.SessionStatus, error) {
	for _, tier := range tiers {
		switch {
		case len(tier) == 1:
			return tier[0], nil
		case len(tier) > 1:
			return rpc.SessionStatus{}, ambiguous(want, len(tier), st)
		}
	}
	return rpc.SessionStatus{}, fmt.Errorf("nothing here is named %q, and no session here starts with it\n%s", want, runningSessions(st))
}

// ambiguous is the refusal to pick one conversation out of several.
func ambiguous(want string, n int, st rpc.Status) error {
	return fmt.Errorf("%q names %d sessions; use more of the name or the id\n%s", want, n, runningSessions(st))
}

// runningSessions lists what could be attached to, so a wrong id is one command
// rather than two.
//
// It shows the labelled title rather than the bare name, because the list is
// what somebody reads to choose between two agents - and at that moment which
// branch each is on is the whole difference between them.
//
// Everything on it is something attach accepts, which is what makes a parked
// row wrong here rather than merely surplus: the header says "running now", a
// parked session is not running, and offering one is an offer the very next
// command refuses. `wake status` is the surface that lists every session
// whatever state it is in, and it prints the state beside each.
func runningSessions(st rpc.Status) string {
	var b strings.Builder
	b.WriteString("running now:")
	n := 0
	for _, s := range st.Sessions {
		if s.State == rpc.StateEnded || s.State == rpc.StateParked {
			continue
		}
		n++
		fmt.Fprintf(&b, "\n  %s  %s  %s", shortID(s.ID), sessionTitle(s), s.State)
	}
	if n == 0 {
		return "nothing is running"
	}
	return b.String()
}

// endedBecause is the reason half of an ending, when there is one. A clean exit
// carries no error and inventing one would be worse than saying nothing.
func endedBecause(s rpc.SessionStatus) string {
	if s.Error == "" {
		return ""
	}
	return " (" + s.Error + ")"
}
