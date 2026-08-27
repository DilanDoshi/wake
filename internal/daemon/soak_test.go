//go:build soak

// The daemon under the same load core's soak puts sessions under: whole
// lifecycles, thousands of them, through a real socket.
//
// It is a separate lane rather than an extension of core's because it
// measures something core's cannot reach. core churns sessions in-process; a
// leak that costs one goroutine per *connection*, one roster write per
// session, or one client queue per attach is invisible there and accumulates
// here. And the failure this daemon exists to prevent - one slow client
// freezing a fleet - only has meaning when there are clients.
//
// Behind the `soak` tag so it never runs in the ordinary suite, and written
// to find things rather than to pass: everything it measures is logged
// whether or not it fails.

package daemon

import (
	"flag"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

var (
	// Short by default so the soak is runnable in a review. The advertised
	// hour is `make soak SOAK_DURATION=1h`; nothing about the test changes
	// between the two, only how many lifecycles it gets through.
	soakDuration = flag.Duration("soak.duration", 20*time.Second, "how long TestSoakDaemon keeps sessions churning")
	soakClients  = flag.Int("soak.sessions", 20, "how many clients churn sessions concurrently")
)

// soakStep bounds one lifecycle step. Long enough to survive a loaded
// machine, short enough that a wedge fails the soak rather than running out
// the clock on it.
const soakStep = 30 * time.Second

// soakPoll is how often a worker asks whether its session has appeared or
// gone. Polling rather than waiting on a pushed frame is deliberate: a
// lagging client loses frames by design, so a soak that waited on one
// particular frame would be measuring its own luck.
const soakPoll = 20 * time.Millisecond

func TestSoakDaemon(t *testing.T) {
	fakeClaudeOnPath(t, "")

	baseline := settledGoroutines()
	childrenBefore := childCount(t)
	d := startDaemon(t)

	var (
		lifecycles atomic.Int64
		events     atomic.Int64
		gaps       atomic.Int64
		errors     atomic.Int64
	)

	// One client that only reads, for the whole run. It is the one that
	// would show frames being lost to something other than its own lag.
	//
	// It is also the room: every frame it takes off the socket is folded
	// through a real Fleet and drawn into a real Room, which is what an
	// attached `wake` does with the same stream. See roomFold.
	reader := newSoakClient(t, d.socket)
	room := newRoomFold()
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for f := range reader.frames {
			room.take(f)
			switch f.Kind {
			case rpc.FrameEvent:
				events.Add(1)
				if f.SessionID == "" || f.Event == nil {
					errors.Add(1)
				}
			case rpc.FrameError:
				if strings.Contains(f.Text, gapNotice) {
					gaps.Add(1)
				}
			}
		}
	}()

	deadline := time.Now().Add(*soakDuration)
	var wg sync.WaitGroup
	for range *soakClients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := newSoakClient(t, d.socket)
			defer c.close()
			for time.Now().Before(deadline) {
				if !c.oneLifecycle(uuid.NewString()) {
					errors.Add(1)
					return
				}
				lifecycles.Add(1)
			}
		}()
	}
	wg.Wait()

	// Every session must be gone before the daemon is asked to leave, or the
	// shutdown path is what is being measured rather than the churn.
	if got := live(reader.status()); len(got) != 0 {
		t.Errorf("%d sessions still held after every lifecycle finished: %+v", len(got), got)
	}

	// Then a bounded park phase, because park is a new ending with a retire
	// path of its own and childCount is the only detector in this repository
	// that can say a parked process is *actually* gone rather than reported so.
	//
	// **Bounded, and outside the churn, deliberately.** A parked session stays
	// in s.agents by design - `holds` has to keep refusing a respawn under its
	// id - so parking inside the churn loop accumulates rows for the whole run,
	// and every waitForSession poll serialises all of them. That makes the lane
	// quadratic in its own duration and measures the report size rather than a
	// leak, which is deferred M5 (nothing evicts a parked entry) biting the one
	// place it would be noticed. A fixed handful gets the detector without the
	// bound: what matters afterwards is that no process outlives the daemon,
	// and the assertion for that is already below.
	parked := parkSome(t, d.socket, *soakClients)
	if len(parked) != *soakClients {
		t.Errorf("%d of %d sessions reached %q: a park that did not take is a session the childCount "+
			"check below would clear for the wrong reason", len(parked), *soakClients, rpc.StateParked)
	}

	// Then wake half of them, and leave the other half parked. Both halves are
	// load-bearing and neither is arbitrary:
	//
	//   - The woken half is a *live fleet at shutdown*, which the churn phase
	//     deliberately drains before this point - so it is the only thing here
	//     that makes the daemon stop running processes on its way out rather
	//     than merely close an empty roster.
	//   - The parked half keeps the park book non-empty when the quit verb
	//     arrives, which is what makes the "stop clears the book" assertion at
	//     the end about anything. Waking all of them would empty the book
	//     first and leave that check passing over nothing.
	half := len(parked) / 2
	if woke := wakeSome(t, d.socket, parked[:half]); woke != half {
		t.Errorf("%d of %d parked sessions came back: a wake that did not take leaves no process for the "+
			"childCount check to find, so it would clear for the wrong reason", woke, half)
	}

	// And the race the sequential phase above cannot reach.
	//
	// Waking each id once, from one connection, exercises the wake *path* and
	// no part of the hazard it is guarded against. Measured: gutting
	// replaceParked's pointer-identity check - the thing that makes two wakes
	// of one id safe - left the lane green with the sequential phase alone.
	//
	// Two connections asking for the same id at once is the shape that matters,
	// because each client connection is dispatched on its own goroutine. If the
	// loser is not refused it has already started a second `claude` under an id
	// something else holds, which branches the transcript with last-writer-wins
	// and no error on any wire. Nothing detects that afterwards - the only
	// evidence it ever happened is a process, which is what childCount below
	// counts.
	raceWake(t, d.socket, parked[half:])

	reader.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)
	reader.close()
	<-readerDone

	t.Logf("daemon soak: %v, %d clients", *soakDuration, *soakClients)
	t.Logf("  lifecycles %d (%.0f/s)", lifecycles.Load(), float64(lifecycles.Load())/soakDuration.Seconds())
	t.Logf("  events     %d, %d gap notices, %d errors", events.Load(), gaps.Load(), errors.Load())
	t.Logf("  room       %d frames folded, %d events, %d lines drawn, %d agents", room.frames, room.events, room.drawn, room.agents())
	t.Logf("  goroutines baseline %d, now %d", baseline, runtime.NumGoroutine())
	t.Logf("  children   before %d, after %d", childrenBefore, childCount(t))

	if lifecycles.Load() == 0 {
		t.Fatal("no lifecycle completed, so this soak measured nothing")
	}
	if n := errors.Load(); n != 0 {
		t.Errorf("%d lifecycles or frames failed", n)
	}
	room.check(t)

	// The two that would accumulate on hour three.
	//
	// The baseline is settledGoroutines', taken at the top: the *minimum* over
	// a settle window, so a goroutine still unwinding from an earlier test
	// drags it toward the truth instead of raising it to a height a leak of the
	// same size could then sit at unseen. waitForGoroutines is the wait; that
	// is what it is waiting for, and the room client adds nothing to either
	// side of it - the fold runs on the reader goroutine that was already
	// there.
	waitForGoroutines(t, baseline)
	if after := childCount(t); after > childrenBefore {
		t.Errorf("%d processes still running after the daemon exited, up from %d: an agent survived the daemon that owned it", after, childrenBefore)
	}
	if recs := loadRoster(rosterPath(d.socket)); len(recs) != 0 {
		t.Errorf("roster after shutdown = %+v, want nothing left to hunt", recs)
	}
	// The second file this daemon leaves, and the lane is the only place it is
	// measured at fleet scale: the park phase above parks *soakClients sessions
	// and every one of them writes an entry. The quit is the deliberate ending,
	// so the book goes with the roster - and an entry per park that survived it
	// would be a book growing by a fleet on every run.
	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 0 {
		t.Errorf("park book after `wake stop` = %+v, want nothing offered back: stop is the ending there "+
			"is no way back from", recs)
	}
}

// soakClient is a client that keeps no history. testClient retains every
// frame it sees for its failure messages, which over an hour is the leak
// rather than the finding.
type soakClient struct {
	t      *testing.T
	conn   net.Conn
	frames <-chan rpc.Frame
	errs   <-chan error
}

func newSoakClient(t *testing.T, socket string) *soakClient {
	t.Helper()

	conn := mustDial(t, socket)
	frames, errs := rpc.ReadFrames(conn)
	c := &soakClient{t: t, conn: conn, frames: frames, errs: errs}
	c.send(rpc.Frame{Kind: rpc.FrameStatus}) // proves the connection works
	return c
}

func (c *soakClient) send(f rpc.Frame) {
	if err := rpc.WriteFrame(c.conn, f); err != nil {
		c.t.Errorf("write %s: %v", f.Kind, err)
	}
}

func (c *soakClient) close() {
	_ = c.conn.Close()
	for range c.frames {
	}
	<-c.errs
}

// oneLifecycle spawns a session, talks to it, stops it, and waits for it to
// leave the roster.
func (c *soakClient) oneLifecycle(id string) bool {
	// No name asked for, so the daemon draws one from the pool. Asking for a
	// literal name is what this used to do, and with more than one client it
	// meant every lifecycle after the first collided on it: nameRegistry.claim
	// refuses a *requested* name that is held, the spawn was refused, and the
	// session "never appeared" for the full soakStep. At the advertised 20
	// clients that is most of the run - measured on the committed tree at three
	// clients over two seconds, 2 of 9 lifecycles failed exactly this way.
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id})
	if !c.waitForSession(id, true) {
		c.t.Errorf("session %s never appeared", id)
		return false
	}
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: "ping"})
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: id})
	if !c.waitForSession(id, false) {
		c.t.Errorf("session %s never left the roster after being stopped", id)
		return false
	}
	return true
}

// parkSome spawns n sessions, parks each, and returns how many reached the
// parked state - so the caller can tell "nothing was parked" from "everything
// was parked and left no process".
func parkSome(t *testing.T, socket string, n int) []string {
	t.Helper()

	// Its own connection, never the reader's. The reader has a goroutine
	// draining its frames for the whole run, so a status request written down
	// that connection races its own answer - `reader.status()` returns an empty
	// Status about half the time and every poll built on it silently waits out
	// its deadline.
	c := newSoakClient(t, socket)
	defer c.close()

	ids := make([]string, 0, n)
	for range n {
		id := uuid.NewString()
		// Unnamed, for oneLifecycle's reason: a requested name that is already
		// held is refused, and every one of these is alive at once.
		c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id})
		if !c.waitForSession(id, true) {
			t.Errorf("session %s never appeared to be parked", id)
			continue
		}
		c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: id})
		ids = append(ids, id)
	}
	parked := make([]string, 0, len(ids))
	for _, id := range ids {
		if c.waitForState(id, rpc.StateParked) {
			parked = append(parked, id)
		} else {
			t.Errorf("session %s never reached %q", id, rpc.StateParked)
		}
	}
	return parked
}

// wakeSome brings parked sessions back and reports how many answered.
//
// This is the half of park/wake the lane could not see. The churn loop spawns
// and stops; parkSome parks. Nothing sent FrameWake, so `resumeSafe`,
// `replaceParked`'s pointer-identity swap, `withdraw`'s wake arm and
// `admitRefusal`'s wake sentence had no coverage under load at all - and the
// row-before-process ordering they protect is the fix for the one hazard that
// branches a transcript with no error on any wire.
//
// Its own connection, for parkSome's reason: the reader has a goroutine
// draining frames for the whole run, so a status request written down that
// connection races its own answer.
func wakeSome(t *testing.T, socket string, ids []string) int {
	t.Helper()

	c := newSoakClient(t, socket)
	defer c.close()

	for _, id := range ids {
		c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: id})
	}
	woke := 0
	for _, id := range ids {
		// Idle rather than "not parked": a wake that started nothing would
		// leave the row in the fleet, and only a state the agent reaches by
		// running says a process came back.
		if c.waitForState(id, rpc.StateIdle) {
			woke++
		} else {
			t.Errorf("session %s was asked to wake and never reached %q", id, rpc.StateIdle)
		}
	}
	return woke
}

// waitForState polls until a session reports one state.
//
// waitForSession cannot express a park: it asks whether the id is still held,
// and a parked one is - that is the whole design. The state is the only thing
// that moves.
func (c *soakClient) waitForState(id, state string) bool {
	deadline := time.Now().Add(soakStep)
	for time.Now().Before(deadline) {
		for _, s := range c.status().Sessions {
			if s.ID == id && s.State == state {
				return true
			}
		}
		time.Sleep(soakPoll)
	}
	return false
}

// waitForSession polls until a session is present or absent.
func (c *soakClient) waitForSession(id string, want bool) bool {
	deadline := time.Now().Add(soakStep)
	for time.Now().Before(deadline) {
		if holds(c.status(), id) == want {
			return true
		}
		time.Sleep(soakPoll)
	}
	return false
}

func (c *soakClient) status() rpc.Status {
	c.send(rpc.Frame{Kind: rpc.FrameStatus})
	deadline := time.After(soakStep)
	for {
		select {
		case f, open := <-c.frames:
			if !open {
				return rpc.Status{}
			}
			if f.Kind == rpc.FrameStatusReply && f.Status != nil {
				return *f.Status
			}
		case <-deadline:
			return rpc.Status{}
		}
	}
}

// holds reports whether a session is still running. Ended sessions stay in a
// status report for a while so a client that missed the announcement can still
// learn why one stopped - so "is it still there" has to ignore those, or a
// lifecycle never appears to finish.
func holds(st rpc.Status, id string) bool {
	for _, s := range st.Sessions {
		if s.ID == id && s.State != rpc.StateEnded {
			return true
		}
	}
	return false
}

// childCount is how many processes this one is still the parent of, which is
// the OS's own answer to "did anything survive that should not have".
func childCount(t *testing.T) int {
	t.Helper()

	out, err := exec.Command("ps", "-o", "ppid=", "-ax").Output()
	if err != nil {
		t.Logf("ps: %v", err)
		return 0
	}
	me := strconv.Itoa(os.Getpid())
	n := 0
	for _, line := range strings.Fields(string(out)) {
		if line == me {
			n++
		}
	}
	return n
}

// raceWake asks two connections to wake the same session at once, for every id
// given, and requires each to come back exactly once.
//
// It asserts nothing about *which* connection wins - that is a race and both
// answers are correct. What it asserts is that only one process results, and
// the assertion for that is childCount at the end of the lane: a loser that
// started a `claude` anyway leaves it running under an id the winner also
// holds, and neither the daemon nor claude's wire has anything to say about it.
func raceWake(t *testing.T, socket string, ids []string) {
	t.Helper()
	if len(ids) == 0 {
		t.Fatal("raceWake was given no sessions, so it raced nothing: the phase above parks the fleet " +
			"this splits, and an empty half means that phase changed shape")
	}

	a := newSoakClient(t, socket)
	defer a.close()
	b := newSoakClient(t, socket)
	defer b.close()

	for _, id := range ids {
		var wg sync.WaitGroup
		wg.Add(2)
		for _, c := range []*soakClient{a, b} {
			go func() {
				defer wg.Done()
				c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: id})
			}()
		}
		wg.Wait()

		if !a.waitForState(id, rpc.StateIdle) {
			t.Errorf("session %s was woken by two connections at once and never reached %q: one of them "+
				"had to win, and a refusal for both leaves a parked session nobody can bring back",
				id, rpc.StateIdle)
		}
	}
}

// ⌃Q at fleet scale: park everything on the way out, and come back to it.
//
// A separate test rather than a phase of the lane above, because the two quit
// verbs disagree about exactly one thing and it is the thing worth asserting.
// `wake stop` is the deliberate ending and clears the book; `⌃Q` fills it. The
// lane above ends with FrameQuit and asserts the book is *empty*, so folding
// this in would invert its closing assertion and leave neither claim tested.
//
// No churn phase here. The subject is the shutdown path, and what it needs is a
// *live fleet at the moment the verb arrives* - which is what an operator has
// when they press ⌃Q, and what the lane above deliberately drains before its
// own quit. Task 5's review found the one-frame version of this does not work
// for that reason: by the time the verb was sent the fleet was already parked,
// so `beginPark` refused every session and nothing was parked on the way out at
// all.
//
// # What this discriminates, and what it does not
//
// It kills the mutation that matters: dispatching FrameParkAll to `quitStop` -
// ⌃Q behaving like `wake stop` - empties the book and fails here with the
// count. That is the one thing the two verbs disagree about, and the reason
// FrameParkAll is its own kind rather than a flag on FrameQuit.
//
// It does **not** discriminate which writer filled the book. Gutting
// `bookParked` entirely leaves this green, and a narrowing of it does too,
// because `completePark` writes the same records from each session's fan-out
// goroutine - the two-writers problem CLAUDE.md already records against the
// end-to-end ⌃Q test. Both mutants were run here and both survived.
// `TestTheParkBookRecordsExactlyTheSessionsThatParked` is what covers that: it
// drives `bookParked` with agents that have no fan-out goroutine at all, so
// there is only one writer left to attribute the book to.
//
// So this is the fleet-scale, live-fleet-at-shutdown case with childCount
// behind it, and deliberately not a second attempt at that attribution.
func TestSoakParkAllLeavesAFleetToComeBackTo(t *testing.T) {
	fakeClaudeOnPath(t, "")

	childrenBefore := childCount(t)
	d := startDaemon(t)

	c := newSoakClient(t, d.socket)
	ids := make([]string, 0, *soakClients)
	for range *soakClients {
		id := uuid.NewString()
		c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id})
		if !c.waitForSession(id, true) {
			t.Errorf("session %s never started, so it is not part of the fleet this parks", id)
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		t.Fatal("no session started, so this measured nothing")
	}

	// Every one of them alive right now. That is the precondition the whole
	// test is about, and asserting it here means a failure below cannot be
	// "there was nothing to park".
	if got := live(c.status()); len(got) != len(ids) {
		t.Fatalf("%d of %d sessions are live when the fleet is parked, so this is not the shutdown an "+
			"operator produces with ⌃Q", len(got), len(ids))
	}

	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	d.waitForExit(t)
	c.close()

	// The book is the whole point: ⌃Q's promise is that the next `wake` can
	// offer this fleet back, and the book is the only thing that carries it
	// across a daemon that no longer exists.
	recs := loadParkBook(parkBookPath(d.socket))
	if len(recs) != len(ids) {
		t.Errorf("the park book holds %d of %d sessions after ⌃Q. Every one missing is a conversation "+
			"the next `wake` cannot offer back, and nothing anywhere reports it was lost", len(recs), len(ids))
	}
	booked := make(map[string]bool, len(recs))
	for _, r := range recs {
		booked[r.ID] = true
		if r.Dir == "" {
			t.Errorf("session %s was booked with no directory: a wake has to run where the session ran, "+
				"so a record without one is restored and then refused", r.ID)
		}
	}
	for _, id := range ids {
		if !booked[id] {
			t.Errorf("session %s is not in the park book after ⌃Q", id)
		}
	}

	// And parked means the process is gone. A book full of entries whose
	// processes are still running would be the worst of both: the next daemon
	// offers them back and `resumeSafe` refuses every one.
	waitForGoroutines(t, settledGoroutines())
	if after := childCount(t); after > childrenBefore {
		t.Errorf("%d processes still running after ⌃Q, up from %d: parking is ending the process, and a "+
			"parked session whose process survives is one no wake can ever bring back", after, childrenBefore)
	}
}

// roomFold is a `wake` room attached to the soak's socket: the same Fleet and
// the same Room the TUI folds its stream through, driven by frames off a real
// daemon over a real socket.
//
// # Why the room is on this lane
//
// Because internal/ui has no socket. Every test over Fleet.Observe and
// Room.Append there hands them events a test wrote; this hands them whatever
// twenty clients churning whole lifecycles actually produce, for as long as the
// soak runs. The failure it exists to catch is the one a table test structurally
// cannot see: a fold that grows with the *stream* rather than with what it
// draws, which at 15-30 agents is the difference between a room that can be left
// open and one that cannot.
//
// # What it asserts, and what it cannot
//
// It asserts the shape of the growth - the room holds what it drew and the
// stream is far larger than that - and it asserts the fold ran at all. It does
// **not** assert the filter's policy: which kinds reach the room is
// internal/ui/fleet_test.go's table, driven per kind, and a soak that tried to
// restate it would be a second copy of that policy written against whatever
// the fake agent happens to emit.
//
// The room is drawn at a real width so Append does the block render it does in
// production. Nothing calls View: Bubble Tea's draw is per *message* and this
// lane has no event loop, so a View here would be a number with no denominator.
type roomFold struct {
	fleet ui.Fleet
	room  ui.Room

	// frames is everything that arrived, events is the subset carrying an
	// agent's own output, and drawn is what the fold handed the room. The
	// bound below is drawn against **events**, not against frames: every
	// drawn line comes from an event, so a room that drew all of them would
	// still be under the frame count and a check written that way round could
	// not fail for the mutation it exists to catch.
	frames int
	events int
	drawn  int
}

// roomFoldWidth and roomFoldHeight are one pane of a 200-column terminal with
// both sidebars open - the arrangement §8 describes.
const roomFoldWidth, roomFoldHeight = 81, 40

func newRoomFold() *roomFold {
	return &roomFold{fleet: ui.NewFleet(), room: ui.NewRoom().SetSize(roomFoldWidth, roomFoldHeight)}
}

// take folds one frame exactly as ui.App.apply does: a report updates the
// fleet, an event is observed, and whatever the fold returns is drawn.
func (r *roomFold) take(f rpc.Frame) {
	r.frames++
	switch f.Kind {
	case rpc.FrameStatusReply, rpc.FrameStatusPush:
		r.fleet = r.fleet.WithStatus(f.Status)
	case rpc.FrameEvent:
		if f.Event == nil {
			return
		}
		r.events++
		out := []core.Event(nil)
		r.fleet, out = r.fleet.Observe(*f.Event, f.SessionID)
		who, _ := r.fleet.Agent(f.SessionID)
		for _, ev := range out {
			r.room = r.room.Append(ev, who)
			r.drawn++
		}
	}
}

func (r *roomFold) agents() int { return len(r.fleet.Agents()) }

// check is the bound this lane exists to hold.
func (r *roomFold) check(t *testing.T) {
	t.Helper()

	if r.frames == 0 {
		t.Fatal("the room client folded no frames at all, so every assertion below is vacuous")
	}
	if r.drawn == 0 {
		t.Error("the room drew nothing from a stream that carried whole lifecycles: the fold is not reaching the room, " +
			"so its bound is being satisfied by the fold being dead rather than by the fold being cheap")
	}
	if r.drawn >= r.events {
		t.Errorf("the room drew %d lines from %d events: it is growing with the stream rather than with what it draws, "+
			"which is the cost a fleet of 15-30 agents multiplies", r.drawn, r.events)
	}
	if r.agents() == 0 {
		t.Error("the room's fleet is empty after a whole soak: an agent seen only through an event is still an agent, " +
			"so a fleet that stayed empty means Observe never ran")
	}
}
