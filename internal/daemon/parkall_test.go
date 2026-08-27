// `⌃Q`: parking the whole fleet on the way out, and the ordering that makes it
// safe for somebody to start a daemon the moment this one lets go.
//
// Split off park_test.go at the 800-line convention, by subject. That file is
// about one session parking and what survives it; this one is about the fleet -
// a different verb, a different frame kind, and a property about *when* the
// park book is finished rather than about what is in it.

package daemon

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Parking the whole fleet on the way out - `⌃Q` - is a different verb from
// stopping it, and the difference is entirely about what happens to the book.
//
// The park book is complete before any client learns the daemon is going.
//
// This is EnsureRunning's edge, from the inside. A daemon in shutdown keeps its
// listener bound, so a `wake` started during it dials into the backlog and
// waits for hello-or-EOF - and the EOF is the signal that starting a fresh
// daemon is safe. If the book were written after the clients were closed, the
// next daemon could read a partial one and offer back half a fleet, or none,
// with nothing anywhere reporting it.
//
// The attached client's connection ends in closeClients, which is the *first*
// thing shutdown does after the parking. So "the book is complete when my
// connection dies" is the strictly stronger claim, and it is the one asserted.
//
// **What this test alone does not prove is the ordering.** Moving the book
// write one statement later - after closeClients rather than before - leaves a
// window of microseconds in which this can still read a finished book, so this
// is a content assertion that happens to be taken at the edge.
// TestTheParkBookIsWrittenEarlyAndForgottenLate is what closes the ordering,
// statically, and the two are a pair.
func TestTheParkBookIsCompleteBeforeTheDaemonLetsGoOfItsClients(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.spawn(idBeta, "sydney")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.awaitState(idBeta, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	c.awaitClose() // the daemon closed this client: shutdown has reached closeClients

	book := map[string]bool{}
	for _, rec := range loadParkBook(parkBookPath(d.socket)) {
		book[rec.ID] = true
	}
	for _, id := range []string{idAlpha, idBeta} {
		if !book[id] {
			t.Errorf("session %s is not in the park book when the daemon closed its clients. The next "+
				"`wake` starts on the EOF that follows, so anything written after this point is "+
				"written after somebody has already read the book", id)
		}
	}
	d.waitForExit(t)
}

// The two orderings the park book depends on, asserted where they live rather
// than sampled where they show.
//
// This is rung 6: the test above observes a *finished* book at an edge, which
// is the end state and not the ordering. Move the write below
// `s.closeClients()` and that test races a window measured in microseconds - it
// would fail often and pass sometimes, which is worse than either. The property
// is "this statement runs before that one", the code declares it as statement
// order, and statement order is static.
//
// **The two arms pull in opposite directions**, which is why this is a table
// and not one comparison. An entry that must *exist* has to be written before
// anybody can read the book, so bookParked goes before the clients are closed
// and a duplicate written afterwards costs nothing. An entry that must *not*
// exist has to be removed after anything that could still write one - and
// completePark writes from a fan-out goroutine, so a clear placed early is
// overtaken by a park that was still finishing and `wake stop` leaves behind
// the one session the operator ended it to be rid of. The wait on the
// WaitGroup is what proves no such write is still coming.
//
// It reads shutdown's own body rather than a copy of it, and every anchor is a
// Fatal when it is missing: a renamed method or a broken parse otherwise yields
// "no violation", which reads as the strongest possible pass. **And an index is
// only a happens-before while the statement runs on this goroutine**, which the
// last check below is what makes true.
func TestTheParkBookIsWrittenEarlyAndForgottenLate(t *testing.T) {
	body := functionBody(t, "server.go", "shutdown")

	var anchors []string
	for _, tc := range []struct{ earlier, later, why string }{
		{
			earlier: "s.bookParked(", later: "s.closeClients()",
			why: "a client whose connection has just died waits for the EOF on the listener and starts a " +
				"fresh daemon, which reads this file - so a fleet booked afterwards is offered back as " +
				"whatever had been written by then, with nothing anywhere reporting the rest. " +
				"EnsureRunning's doc comment is what this ordering is for",
		},
		{
			earlier: "waitFor(&s.wg", later: "s.parked.clear()",
			why: "completePark adds an entry from a fan-out goroutine, after core's Wait returns, and that " +
				"wait is the only thing that proves none is still coming. Cleared before it, a park that " +
				"was still finishing lands in a book `wake stop` had already emptied - and the next " +
				"`wake` offers back the one session somebody ran stop to be rid of",
		},
	} {
		anchors = append(anchors, tc.earlier, tc.later)

		earlierAt, laterAt := -1, -1
		for i, stmt := range body.List {
			text := stmtText(t, stmt)
			if earlierAt < 0 && strings.Contains(text, tc.earlier) {
				earlierAt = i
			}
			if laterAt < 0 && strings.Contains(text, tc.later) {
				laterAt = i
			}
		}
		switch {
		case earlierAt < 0 || laterAt < 0:
			t.Fatalf("shutdown has no top-level statement containing %q (found at %d) or %q (found at %d), "+
				"so this pair orders nothing and the test is asserting nothing. Either the work moved out "+
				"of shutdown - in which case this test has to follow it - or it is gone:\n%s",
				tc.earlier, earlierAt, tc.later, laterAt, stmtText(t, body))
		case earlierAt > laterAt:
			t.Errorf("shutdown runs %q at statement %d and %q at statement %d, so they are the wrong way "+
				"round: %s", tc.earlier, earlierAt, tc.later, laterAt, tc.why)
		}
	}

	// **An index is not a happens-before, and one token turns the first into a
	// claim about nothing.** `go s.bookParked(agents)` keeps the statement
	// exactly where it is, so every comparison above stays green while the
	// property dies: the write is merely *scheduled* before the clients are
	// closed, the scheduler is free to run closeClients first, and a `wake`
	// woken by the EOF reads `parked.json` while the goroutine is mid-add. The
	// file is replaced whole through a rename, so what it reads is a fleet short
	// by however many had not been added yet - with nothing anywhere reporting
	// the rest. `defer` is the same edit with a different keyword and lands
	// after `roster.clear`.
	//
	// It is also the single most likely edit anybody makes to a shutdown path
	// that feels slow, which is why it is worth a check of its own rather than a
	// sentence in a comment.
	ast.Inspect(body, func(n ast.Node) bool {
		var keyword string
		switch n.(type) {
		case *ast.GoStmt:
			keyword = "go"
		case *ast.DeferStmt:
			keyword = "defer"
		default:
			return true
		}
		text := stmtText(t, n)
		for _, anchor := range anchors {
			if !strings.Contains(text, anchor) {
				continue
			}
			t.Errorf("shutdown runs %q under a `%s`, so its position in the statement list is no longer "+
				"when it happens: `%s`. Every ordering above is then a claim about where a line sits "+
				"rather than about what has finished, and the park book can still be being written when "+
				"a client reads it.", anchor, keyword, text)
		}
		return true
	})
}

// A session that had to be killed is not parked, and the reason is a missing
// recording rather than a policy.
//
// The transcript is on disk either way; what nobody has recorded is what
// --resume loads from one a SIGKILL cut mid-turn. Offering it back would be a
// guess dressed as a feature, and the operator would find out by resuming into
// a conversation that is missing its last turn - or is not, nobody knows.
//
// `deaf` is the fake for it: it says `ready` and then ignores its stdin for
// far longer than any grace, so closing stdin cannot end it and the kill at the
// end of the grace is the only way out. A fake that is merely *slow* would race
// its own clock; this one has no clock in it that the grace can win.
func TestASessionThatHadToBeKilledIsNotParked(t *testing.T) {
	shortQuitGrace(t, 200*time.Millisecond)
	fakeClaudeOnPath(t, "deaf")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "alex")
	// The barrier every fake with an opening turn owes a test here: the session
	// is provably started and its process provably running before the fleet is
	// asked to park, so the kill below lands on something.
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	c.awaitClose()
	d.waitForExit(t)

	for _, rec := range loadParkBook(parkBookPath(d.socket)) {
		if rec.ID == idAlpha {
			t.Errorf("session %s was killed at shutdown and written to the park book anyway. What a "+
				"--resume of a transcript a SIGKILL cut mid-turn loads is unrecorded, so offering it "+
				"back is a guess", idAlpha)
		}
	}
}

// A signalled daemon parks nothing, because a signal is not a verb.
//
// The third value of quitVerb, and the one no client can send. A SIGTERM, a
// laptop lid or the process group going reaches Serve as a cancelled context,
// and nobody in that story asked for a fleet to be parked - so a book that
// filled up on the way out would offer back twenty sessions the next `wake`
// would resume without anyone ever having decided to keep them.
//
// It is the mirror of TestADaemonThatWasSignalledRatherThanStoppedKeepsTheParkedFleet,
// which holds the other half: that one starts with a parked session and
// requires it to survive, this one starts with a live one and requires it not
// to be added. Neither implies the other, and the mutation that makes
// `a.beginPark()` unconditional is invisible to the first.
func TestADaemonThatWasSignalledParksNothing(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.close()

	// The context cancellation Serve is given, which is what signal.NotifyContext
	// delivers on SIGINT and SIGTERM in cmd/wake's serveDaemon.
	d.stop(t)

	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 0 {
		t.Errorf("a daemon that was signalled left %+v in the park book. Nobody asked for that fleet to "+
			"be parked, and the next `wake` reads this file to decide what to offer back - so a signal "+
			"would silently become the recoverable ending that ⌃Q is the verb for", recs)
	}
}

// The first quit verb wins, so a park is never turned into a stop.
//
// Two clients ending the daemon at once is a race nobody can resolve into a
// third meaning, and the two answers are not symmetric: a park that became a
// stop clears a book somebody meant to keep, and there is nothing left
// afterwards that says a fleet was lost. So whichever arrived first is what this
// daemon is doing.
//
// Driven directly rather than through two sockets, and that is what makes it an
// assertion rather than a coin toss: over a connection the second frame is only
// dispatched if the reader goroutine gets to it before shutdown closes the
// client, so a test that sent both would pass whether or not the guard exists
// on the runs where the second frame never arrived.
//
// The table is every ordered pair of the two verbs a client can produce.
// quitNone is not among them - nothing passes it, it is what quitReason answers
// before anyone has - and the zero case below is what asserts that.
func TestTheFirstQuitVerbWinsSoAParkIsNeverTurnedIntoAStop(t *testing.T) {
	if got := newServer(tempSocket(t)).quitReason(); got != quitNone {
		t.Errorf("a daemon nobody has ended reports verb %d, want quitNone (%d): a signal is not a verb, "+
			"and a daemon that thinks it was asked for something acts on it in shutdown", got, quitNone)
	}

	for _, tc := range []struct {
		what   string
		first  quitVerb
		second quitVerb
	}{
		{"a park then a stop", quitPark, quitStop},
		{"a stop then a park", quitStop, quitPark},
		{"two parks", quitPark, quitPark},
		{"two stops", quitStop, quitStop},
	} {
		t.Run(tc.what, func(t *testing.T) {
			s := newServer(tempSocket(t))
			s.beginQuit(tc.first)
			s.beginQuit(tc.second)
			if got := s.quitReason(); got != tc.first {
				t.Errorf("the daemon is quitting for verb %d, want %d - the first one it was given. The "+
					"unsafe end of getting this wrong is a park that became a stop: the park book is "+
					"cleared, and nothing anywhere reports that a fleet somebody meant to keep is gone",
					got, tc.first)
			}
		})
	}
}

// Parking the fleet keeps what was already parked, and keeps *when* it parked.
//
// The book carries every parked session through a ⌃Q whether or not this
// shutdown is what parked it, because a restored row is in s.agents like any
// other. What is not free is the timestamp: `Parked` is the only clock a
// restored row has, and noteQuietSince is what stops a session parked yesterday
// coming back reported `quiet 0.0s`. Re-stamping it on the way out would put
// that wrong answer one ⌃Q away, on the surface an operator scans thirty rows
// of to decide what to bring back first.
//
// The record is written by hand rather than parked here, which is the only way
// to have a session that parked long enough ago for the difference to be
// visible without a test that sleeps.
func TestParkingTheFleetKeepsWhenAnAlreadyParkedSessionParked(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	parkedAt := time.Now().Add(-36 * time.Hour).UTC().Round(time.Second)
	writeParkBook(t, socket, []parkedRecord{{ID: idAlpha, Name: "alex", Label: "dev", Dir: "/tmp/repo", Parked: parkedAt}})

	d := startDaemonOn(t, socket)
	c := attach(t, socket)
	if got := stateOf(c.status(), idAlpha); got != rpc.StateParked {
		t.Fatalf("the daemon restored session %s as %q rather than parked, so nothing below is about a "+
			"session that was already parked when the fleet was parked", idAlpha, got)
	}

	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	c.awaitClose()
	d.waitForExit(t)

	var rec parkedRecord
	for _, r := range loadParkBook(parkBookPath(socket)) {
		if r.ID == idAlpha {
			rec = r
		}
	}
	switch {
	case rec.ID == "":
		t.Fatalf("⌃Q left session %s out of the park book although it was already in it. A fleet parked "+
			"on the way out must not be smaller than the one that was parked before it", idAlpha)
	case !rec.Parked.Equal(parkedAt):
		t.Errorf("⌃Q re-stamped a session that parked at %v as parked at %v. That timestamp is the only "+
			"clock a restored row has - the next daemon reports it as quiet since then - so a session "+
			"nobody touched comes back looking like one that was working until a moment ago",
			parkedAt, rec.Parked)
	}
}

// The park book records exactly the sessions that parked, over every state
// shutdown can hand bookParked.
//
// **This is where the booking is actually tested, and the end-to-end test above
// is not.** Both of these narrowings ran green against the whole package with
// the ⌃Q test passing:
//
//	return a.ended                      // "it ended, so book it"
//	return a.ended && a.parked          // the settled flag alone
//
// The reason is completePark. It writes the same record from the fan-out
// goroutine, and in a two-session test on an idle machine it gets there first -
// so a book that is complete proves a park completed and says nothing about
// which writer completed it. Nothing over a live daemon can separate them,
// because the thing being separated is which of two goroutines won.
//
// So this drives bookParked with agents that have no fan-out goroutine at all.
// completePark cannot run, the book is empty unless this wrote it, and the
// states are the ones shutdown produces rather than the eight a struct of three
// booleans permits: each row is built by calling the transitions themselves -
// beginPark, finish, markParked, kill - in an order shutdown can reach.
//
// One call for the whole fleet, and the assertion is over the resulting set. A
// narrowing that excludes some subset of the agents is invisible to a test that
// books one at a time and asks whether that one landed.
func TestTheParkBookRecordsExactlyTheSessionsThatParked(t *testing.T) {
	rows := []struct {
		what string
		// reach is how shutdown gets an agent into this state, called in
		// order. Nothing here writes a field: a state no sequence of these
		// produces is not a state this function has to have an answer for.
		reach func(*agent)
		want  bool
		why   string
	}{
		{
			what:  "parked, and retire finished before shutdown looked",
			reach: func(a *agent) { a.beginPark(); a.finish(nil); a.markParked() },
			want:  true,
			why:   "the settled park: markParked ran in retire, after core's Wait returned",
		},
		{
			what:  "the process has gone and retire has not reached completePark yet",
			reach: func(a *agent) { a.beginPark(); a.finish(nil) },
			want:  true,
			why: "the ordinary path rather than an edge case - shutdown's wait returns on `ended`, which " +
				"finish sets before markParked, so this is the state most of a fleet is in when the book " +
				"is written. A check that reads the settled flag alone loses whichever sessions retire " +
				"had not caught up with, silently, and more of them the busier the machine",
		},
		{
			what:  "ended on its own in the gap before the kill landed",
			reach: func(a *agent) { a.beginPark(); a.finish(nil); a.kill() },
			want:  true,
			why: "the grace samples finished() on a 20ms tick, so a session that ends inside that gap is " +
				"killed after it is already dead. The signal cut nothing and the transcript is the one " +
				"the agent finished writing, so the park stands",
		},
		{
			what:  "killed at the end of the grace, then retired",
			reach: func(a *agent) { a.beginPark(); a.kill(); a.finish(nil) },
			want:  false,
			why: "what a --resume of a transcript a SIGKILL cut mid-turn loads is unrecorded, and this " +
				"project refuses unrecorded behaviour rather than designing around it",
		},
		{
			what:  "killed at the end of the grace and not yet retired",
			reach: func(a *agent) { a.beginPark(); a.kill() },
			want:  false,
			why:   "the same session a moment earlier; nothing about it has become recorded",
		},
		{
			what:  "asked to park and still running",
			reach: func(a *agent) { a.beginPark() },
			want:  false,
			why: "beginPark labels a stop that is in flight. A session that has not ended has a process " +
				"holding its transcript, and a book entry is an offer to resume that id",
		},
		{
			what:  "ended without anyone asking for a park",
			reach: func(a *agent) { a.finish(nil) },
			want:  false,
			why: "an ordinary ending. Booking it would offer back a session somebody stopped, which is " +
				"the whole distinction between the two quit verbs arriving one session at a time",
		},
		{
			what:  "running, and nobody asked it for anything",
			reach: func(a *agent) {},
			want:  false,
			why:   "reachable only under a verb that is not quitPark, and the answer is the same",
		},
	}

	s := newServer(tempSocket(t))
	agents := make([]*agent, len(rows))
	for i, row := range rows {
		id := fmt.Sprintf("%08x-0000-4000-8000-%012x", i+1, i+1)
		agents[i] = newAgent(id, fmt.Sprintf("agent-%d", i), "dev", "/tmp/repo", "",
			core.NewSession(core.Config{SessionID: id}), func() {})
		row.reach(agents[i])
	}

	s.bookParked(agents)

	booked := map[string]bool{}
	for _, rec := range loadParkBook(parkBookPath(s.socket)) {
		booked[rec.ID] = true
	}
	for i, row := range rows {
		if got := booked[agents[i].id]; got != row.want {
			t.Errorf("a session that %s was %s. It should %s: %s",
				row.what,
				map[bool]string{true: "written to the park book", false: "left out of the park book"}[got],
				map[bool]string{true: "be written down", false: "not be offered back"}[row.want],
				row.why)
		}
	}
}

// The booking decision reads the park flags and nothing else, and this is the
// rung the table above cannot reach.
//
// A table closes a *domain*; it cannot close a *value space*. Both of these are
// sentences somebody writes and both walk past every behavioural test here,
// because no fixture happens to hold the value they key on:
//
//	case !a.bookable() || a.dir == "":        // "a wake needs a directory anyway"
//	case !a.bookable() || a.id[0] == 'b':     // a subset of the fleet, silently
//
// The first is not a strawman - unpark really does refuse a record with no
// directory - and it is wrong here for a different reason: refusing to *write*
// one loses the id, which is the one thing that reaches the transcript, while
// refusing to *wake* it keeps the row and says so. So the closing move is to
// deny the decision the field.
//
// **The unit is every decision that can skip an agent, not one function and not
// one kind of node.** An earlier version of this guarded `bookParked`'s *case
// expressions* and nothing else, and `if a.id[0] == 'b' { continue }` one line
// lower - inside the `default:` body, where the `add` actually happens - was
// green against the whole package: not a case expression, keyed on a value no
// table row holds, and invisible end to end because completePark writes the
// record anyway. That is the same two-producer blindness this file's other
// guard exists for, arriving through the gap in it. So:
//
//   - every field `bookable` reads is one of the three park flags, and every
//     mention of the receiver is such a read - which closes "move it into a
//     helper";
//   - **the returned predicate's shape** is closed too, not only its inputs: a
//     grammar of park-flag leaves under `&&`, `||` and `!`. Otherwise
//     `… && os.Getenv("WAKE_NO_BOOK") == ""` reads no field and passes, which is
//     the third of the three shapes that beat the equivalent check in
//     internal/core/argvguard_test.go;
//   - every expression `bookParked` uses **as a truth value** - case
//     expressions, `if` conditions, loop conditions - is one of three, and
//   - every range it walks is the whole fleet, so a narrowing cannot hide in
//     `agents[1:]` where there is no condition to inspect.
//
// The floors are what stop a rename or a broken parse reading as the strongest
// possible pass - zero mentions found is "reads nothing at all", which would
// otherwise be a green run.
func TestTheBookingDecisionReadsNothingButTheParkFlags(t *testing.T) {
	// The flags a booking decision may read, and the whole of what parking a
	// session is: it ended, and the ending was a park.
	allowedFields := map[string]bool{"ended": true, "parking": true, "parked": true}
	// The lock every one of those reads is taken under. It is not an input to
	// the decision and it is not in the floor below, which is why it is named
	// here rather than folded into the set above.
	const theLock = "mu"

	bookable := functionBody(t, "park.go", "bookable")
	seen := map[string]bool{}
	ast.Inspect(bookable, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok {
			if x, isIdent := sel.X.(*ast.Ident); isIdent && x.Name == "a" {
				seen[sel.Sel.Name] = true
				if !allowedFields[sel.Sel.Name] && sel.Sel.Name != theLock {
					t.Errorf("bookable reads a.%s. What may be written to the park book is decided by "+
						"whether the session ended and whether that ending was a park, and by nothing "+
						"else - anything further is a session whose id is lost rather than one that is "+
						"refused a wake with a sentence", sel.Sel.Name)
				}
				return false
			}
		}
		if id, isIdent := n.(*ast.Ident); isIdent && id.Name == "a" {
			t.Errorf("bookable mentions the receiver outside a field selector (%s), which is how a "+
				"predicate moves one call down and out of sight of this check", stmtText(t, n))
		}
		return true
	})
	for field := range allowedFields {
		if !seen[field] {
			t.Errorf("bookable never reads a.%s, so either the predicate has been narrowed or this scan "+
				"is broken - and a scan that finds nothing reports the strongest possible pass", field)
		}
	}

	// And the shape of what it returns, because reading only allowed fields is
	// not the same claim as asking only allowed questions.
	returns := 0
	ast.Inspect(bookable, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		returns++
		if len(ret.Results) != 1 || !isParkFlagPredicate(ret.Results[0], allowedFields) {
			t.Errorf("bookable returns `%s`, which is not a combination of the park flags under &&, || "+
				"and !. A predicate that asks any other kind of question - a comparison, a length, an "+
				"env read, a clock - decides which sessions reach the book on something that is not "+
				"whether they parked", stmtText(t, ret))
		}
		return true
	})
	if returns == 0 {
		t.Error("bookable returns nothing, so the shape check read nothing: the parse is broken or the " +
			"predicate has moved")
	}

	// The other place a predicate can live. Written as printed source rather
	// than as a shape, because the set is three: a call to the predicate above,
	// the lookup that leaves an entry already in the book alone, and the error
	// check on the write itself.
	allowedTests := map[string]bool{"!a.bookable()": true, "held[a.id]": true, "err != nil": true}
	// The whole fleet, and each range subject named: a `range agents[1:]` is a
	// narrowing with no condition in it for the check above to see.
	allowedRanges := map[string]bool{"agents": true, "s.parked.records()": true}

	tests, ranges := 0, 0
	ast.Inspect(functionBody(t, "park.go", "bookParked"), func(n ast.Node) bool {
		var asked []ast.Expr
		switch v := n.(type) {
		case *ast.CaseClause:
			asked = v.List
		case *ast.IfStmt:
			asked = []ast.Expr{v.Cond}
		case *ast.ForStmt:
			if v.Cond != nil {
				asked = []ast.Expr{v.Cond}
			}
		case *ast.RangeStmt:
			ranges++
			if text := stmtText(t, v.X); !allowedRanges[text] {
				t.Errorf("bookParked walks `%s` rather than the whole fleet (%v). A range over a subset "+
					"skips sessions with no condition anywhere for this check to read", text, allowedRanges)
			}
		}
		for _, expr := range asked {
			tests++
			if text := stmtText(t, expr); !allowedTests[text] {
				t.Errorf("bookParked decides on `%s`, which is not one of the three questions it is "+
					"allowed to ask (%v). A condition here - in a case *or* in a body - excludes some "+
					"subset of the fleet from the book, and a session left out of it is an id nobody can "+
					"hand --resume: the transcript is on disk and nothing names it", text, allowedTests)
			}
		}
		return true
	})
	if tests == 0 || ranges == 0 {
		t.Errorf("bookParked has %d expressions used as truth values and %d ranges, and this check needs "+
			"both to be reading something: either the booking loop moved or the parse is broken", tests, ranges)
	}
}

// isParkFlagPredicate reports whether an expression asks only "which of this
// agent's park flags are set", combined with the three boolean operators.
//
// It is the grammar half of the guard above. Constraining which fields may be
// *read* leaves a predicate that reads none of them - an env var, a clock, a
// package-level flag - entirely unconstrained, and that is the shape
// internal/core/argvguard_test.go was written after it beat the equivalent
// check there.
func isParkFlagPredicate(e ast.Expr, allowed map[string]bool) bool {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return isParkFlagPredicate(v.X, allowed)
	case *ast.UnaryExpr:
		return v.Op == token.NOT && isParkFlagPredicate(v.X, allowed)
	case *ast.BinaryExpr:
		// && and || only. An == or a < is a question about a *value*, and the
		// only values here are the flags themselves.
		return (v.Op == token.LAND || v.Op == token.LOR) &&
			isParkFlagPredicate(v.X, allowed) && isParkFlagPredicate(v.Y, allowed)
	case *ast.SelectorExpr:
		x, ok := v.X.(*ast.Ident)
		return ok && x.Name == "a" && allowed[v.Sel.Name]
	default:
		return false
	}
}

// Asking an already-parked session to park again leaves it parked, which is
// markParked's stated invariant surviving the one caller that would break it.
//
// markParked's doc comment says *"asked to park" and "parked" are never both
// true, so nothing has to decide which it is looking at.* ⌃Q is what makes that
// a live claim rather than a description: `shutdown` labels **every** agent it
// took, and under a park verb that set includes sessions parked earlier in this
// daemon's life and rows restored from the park book - agents whose retire has
// already run and will never run again to clear the label.
//
// Nothing reads `parking` on such an agent today and the daemon is exiting, so
// the cost is not a failure this test can name. It is an invariant written in a
// doc comment and falsified by a caller in the same change, which in this
// project is the thing somebody finds three tasks later by trusting the comment.
func TestAskingAnAlreadyParkedSessionToParkAgainLeavesItParked(t *testing.T) {
	a := newAgent(idAlpha, "alex", "", "", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	a.beginPark()
	a.finish(nil)
	a.markParked()
	a.markWakeable(recordFor(a), true)

	a.beginPark()

	if !a.isParked() {
		t.Error("a second park request took a parked session out of the parked state")
	}
	if a.parkRequested() {
		t.Error("a parked session is also reported as having been asked to park. markParked's whole " +
			"contract is that those two are never both true, so anything reading one of them - retire " +
			"is the reader that matters - now has to decide which it is looking at")
	}
}

// A kill withdraws a park only from a session that has not already ended, and
// both halves are load-bearing in opposite directions.
//
// Withdrawing from a *running* session is the rule the whole feature is unsafe
// without: what a --resume of a transcript a SIGKILL cut mid-turn loads is
// unrecorded, so a park request that has not completed by the time somebody
// reaches for kill is refused rather than guessed at.
//
// Not withdrawing from one that has already ended is what makes the rule
// correct rather than merely early. `ended` is set in retire, after core's Wait
// returned, so it is Wake's own proof that the process is gone - the signal cut
// nothing and the transcript is the one the agent finished writing. It is
// reachable rather than theoretical: shutdown's grace samples `finished()` on a
// 20ms tick and kills whatever had not ended by the last look, so a fleet parked
// by ⌃Q loses a session per agent that ends inside that gap - silently, and more
// of them the busier the machine.
//
// Driven at the agent rather than through a socket, because the ordering under
// test is a 20ms window in somebody else's scheduler. An unstarted core.Session
// gives kill everything it needs: Stop finds no stdin and returns nil, and the
// cancel is a no-op with no process behind it.
func TestAKillWithdrawsAParkOnlyFromASessionThatHasNotAlreadyEnded(t *testing.T) {
	for _, tc := range []struct {
		what     string
		ended    bool
		wantPark bool
		why      string
	}{
		{
			what: "the process is still running", ended: false, wantPark: false,
			why: "a SIGKILL that lands mid-turn leaves a transcript nothing has ever recorded a --resume of, " +
				"so the park has to be withdrawn rather than honoured",
		},
		{
			what: "the process has already gone", ended: true, wantPark: true,
			why: "the session had already ended, so the signal cut nothing - withdrawing there throws away a " +
				"park that completed, and shutdown reaches it every time an agent ends inside the 20ms " +
				"between the grace's last look and the kill",
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			a := newAgent(idAlpha, "alex", "", "", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
			a.beginPark()
			if tc.ended {
				a.finish(nil)
			}

			a.kill()

			if got := a.parkRequested(); got != tc.wantPark {
				t.Errorf("after a kill on a session where ended=%v, the park request is %v, want %v: %s",
					tc.ended, got, tc.wantPark, tc.why)
			}
		})
	}
}

// functionBody parses one file of this package and returns the named function's
// body, so a test can assert about the shape of the code rather than about a
// sample of its behaviour.
//
// It matches on the name alone, which is unambiguous here: this package declares
// no two functions or methods sharing one.
func functionBody(t *testing.T, file, name string) *ast.BlockStmt {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name && fn.Body != nil {
			return fn.Body
		}
	}
	t.Fatalf("%s declares no function %s with a body, so whatever this test asserts about it is "+
		"asserted about nothing", file, name)
	return nil
}

// stmtText renders one parsed statement back to source, which is what the
// assertions above match on and what their failures print.
func stmtText(t *testing.T, node ast.Node) string {
	t.Helper()

	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), node); err != nil {
		t.Fatalf("print statement: %v", err)
	}
	return b.String()
}

// Everything this package spawns goes through server.start, so shutdown's Wait
// is a complete account rather than a hope - server.start's own words, and
// CLAUDE.md's "a goroutine leak is a bug" arriving by construction. dispatch
// is where the rule was broken once: the two history reads leave the
// connection's goroutine so a 740ms transcript scan cannot park a client's
// kill frame, and a bare `go` there was a goroutine shutdown never waited for.
func TestDispatchStartsNoUntrackedGoroutine(t *testing.T) {
	body := functionBody(t, "server.go", "dispatch")
	ast.Inspect(body, func(n ast.Node) bool {
		if g, ok := n.(*ast.GoStmt); ok {
			t.Errorf("dispatch starts a goroutine with `go %s`: everything this package spawns goes "+
				"through s.start, so shutdown's Wait accounts for it", stmtText(t, g.Call))
		}
		return true
	})
}
