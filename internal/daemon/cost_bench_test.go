package daemon

// What the daemon costs at the fleet size Wake is for, and what the two things
// Phase 3 added cost on top of it: a 31st session that is the manager, and a
// park book on the startup path.
//
// The daemon is the half of Wake that has no user interface, so nothing about
// it is visible while it is being expensive. Every number here is on a path
// that runs without anybody asking: the fleet report is rebuilt on every state
// change and on every liveness tick that saw one, the park book is rewritten on
// every park, and it is read back before the accept loop.
//
// The agents below have **no process**. parkedAgent is how the daemon itself
// builds a row with nothing behind it, and it is what makes these hermetic -
// there is no fake claude, no socket and no scheduler in the numbers. What that
// costs is stated where it matters: snapshot's work is building an
// rpc.SessionStatus out of fields under one lock, which is the same work for a
// running agent and a parked one, and the arms hold the state constant anyway.
//
// Run: go test ./internal/daemon -run XXX -bench 'Fleet|ParkBook|Restore' -count 5

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// benchFleetSize is CLAUDE.md's opening line, at the top of its range.
const benchFleetSize = 30

var benchStatusSink rpc.Status

// BenchmarkFleetReport is s.fleet(), which is what every status push and every
// status reply is built from - so it is paid once per state change across the
// whole fleet, and once per liveness tick that saw one.
//
// # What the pairing isolates
//
// One row, and which row it is. agents=30 and agents=31 differ by an ordinary
// session; agents=30+manager differs by the manager's, which is the session
// `wake manager` adds and which nothing here treats specially. If those two
// diverge, the daemon has started paying for the manager as a special case,
// which is exactly what internal/daemon/manager.go's "keyed on the name"
// design is supposed to cost nothing at report time.
//
// # What it does not isolate
//
// Not the broadcast. fleet() builds one Status; broadcast then enqueues that
// one frame to every attached client under s.mu, and the encode happens per
// client on its own goroutine. Not the manager's `claude` process either -
// that is a 31st process on the machine and is not in this address space.
func BenchmarkFleetReport(b *testing.B) {
	for _, c := range []struct {
		name    string
		agents  int
		manager bool
	}{
		{"agents=30", benchFleetSize, false},
		{"agents=31", benchFleetSize + 1, false},
		{"agents=30+manager", benchFleetSize, true},
	} {
		b.Run(c.name, func(b *testing.B) {
			s := benchServer(b, c.agents, c.manager)
			want := c.agents
			if c.manager {
				want++
			}
			b.ReportAllocs()
			for b.Loop() {
				benchStatusSink = s.fleet()
			}
			if n := len(benchStatusSink.Sessions); n != want {
				b.Fatalf("the report carries %d sessions, want %d: this is not pricing the fleet it names", n, want)
			}
		})
	}
}

// BenchmarkParkBookWrite is ⌃Q's cost on the way out: N sessions parked one at
// a time, each one rewriting the whole book through a temp file and a rename.
//
// # What the pairing isolates
//
// The shape of the growth, which is the finding rather than either number.
// Every arm parks its whole fleet from an empty book, so agents=30 does 30
// writes of a book that grows to 30 records while agents=1 does one write of a
// book of one. If the second is not far more than thirty times the first, the
// write has stopped being per-park - and if it is much more, the quadratic is
// real and is on the path `shutdown` runs *before* it closes its clients,
// which is the edge `EnsureRunning` waits on.
//
// # What it does not isolate
//
// Not the disk. writeFileAtomically renames into place, and how long that
// takes is the filesystem's answer, not Wake's - which is why the arms are
// compared against each other rather than against a target.
func BenchmarkParkBookWrite(b *testing.B) {
	for _, n := range []int{1, benchFleetSize} {
		b.Run(fmt.Sprintf("agents=%d", n), func(b *testing.B) {
			recs := benchRecords(n)
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				p := newParkBook(parkBookPath(benchSocket(b)))
				b.StartTimer()

				for _, rec := range recs {
					if err := p.add(rec); err != nil {
						b.Fatalf("park book add: %v", err)
					}
				}
				if got := len(p.records()); got != n {
					b.Fatalf("%d records written, want %d", got, n)
				}
			}
		})
	}
}

// BenchmarkParkBookAtStartup is the other half, and it is on the startup path:
// newServer reads the park book off disk before the accept loop runs.
//
// That ordering is why it is worth a number. `wake` decides whether a fleet
// exists by dialling and waiting for FrameHello or EOF, and the hello does not
// arrive until Serve is accepting - so whatever this costs is time a `wake`
// spends looking at nothing.
//
// # What the pairing isolates
//
// Book size, and nothing else. Nothing is restored into the fleet any more and
// no process is started, so what is left is the read - which is why a daemon
// succeeding a ⌃Q now comes up holding zero agents whatever the book says. The
// assertion is on s.agents being *empty*: this benchmark is the place a restore
// growing back would show up as a number rather than as a surprise.
func BenchmarkParkBookAtStartup(b *testing.B) {
	for _, n := range []int{1, benchFleetSize} {
		b.Run(fmt.Sprintf("parked=%d", n), func(b *testing.B) {
			socket := benchSocket(b)
			seed := newParkBook(parkBookPath(socket))
			for _, rec := range benchRecords(n) {
				if err := seed.add(rec); err != nil {
					b.Fatalf("seed park book: %v", err)
				}
			}

			b.ReportAllocs()
			for b.Loop() {
				// newServer is what Serve calls, and it seeds the book off
				// disk on the way past - so the read is inside the
				// measurement exactly as it is inside a start.
				s := newServer(socket)
				if got := len(s.parked.records()); got != n {
					b.Fatalf("%d records read, want %d: this is not pricing the read it names", got, n)
				}
				if got := len(s.agents); got != 0 {
					b.Fatalf("a starting daemon holds %d agents, want 0: the park book is addressable, not restored", got)
				}
			}
		})
	}
}

// BenchmarkClientEnqueue is the fan-out's per-frame cost. It runs on the
// goroutine draining an agent's stdout, so anything it costs is paid once per
// frame per attached client - and since --include-partial-messages that is once
// per output token, ~1,300/s across a busy fleet.
//
// The queue is full in both arms, deliberately: that is the state
// partialCeiling decides anything in, and it is the one a lagging client is
// actually in. A drained queue would price the channel handoff and the
// scheduler rather than the discriminator.
//
// Measured on an M5 Max: **8.9-9.2ns for a record and 4.4-4.5ns for a
// preview**, no allocation either way - the ceiling returns before the select
// the record still runs. So the frame rate this fan-out was given by
// --include-partial-messages did not get more expensive to refuse; it got
// cheaper.
//
// Run: go test ./internal/daemon -run XXX -bench ClientEnqueue -count 5
func BenchmarkClientEnqueue(b *testing.B) {
	for _, c := range []struct {
		name  string
		frame rpc.Frame
	}{
		{"record", rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{Kind: core.KindAssistantText, Text: "done"}}},
		{"preview", rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{Kind: core.KindPartialText, Text: "tok "}}},
	} {
		b.Run(c.name, func(b *testing.B) {
			cl := &client{out: make(chan rpc.Frame, clientQueue)}
			for range clientQueue {
				cl.out <- c.frame
			}
			b.ReportAllocs()
			for b.Loop() {
				cl.enqueue(c.frame)
			}
		})
	}
}

// benchServer is a daemon holding n rows and no processes.
func benchServer(b *testing.B, agents int, manager bool) *server {
	b.Helper()

	s := newServer(benchSocket(b))
	for i := range agents {
		rec := benchRecord(i)
		if !s.register(parkedAgentRow(rec, benchName(i))) {
			b.Fatalf("agent %d was refused a row", i)
		}
	}
	if manager {
		if !s.register(parkedAgentRow(benchRecord(agents), core.ManagerName)) {
			b.Fatal("the manager was refused a row")
		}
	}
	return s
}

// benchRecords is a design-sized park book: real UUIDs, because a resume
// refuses an id Wake could not have minted and a fixture of `s0`..`s29` would
// name nothing that could ever come back.
func benchRecords(n int) []parkedRecord {
	out := make([]parkedRecord, 0, n)
	for i := range n {
		out = append(out, benchRecord(i))
	}
	return out
}

// benchName is a display name for agent i. Not `a00`: normalizeName refuses a
// name made only of hex digits, because `wake status` prints eight characters
// of a session id and such a name would shadow one - so a hex fixture sends a
// resume down its *fallback* branch, claiming a pool name instead, and the
// benchmark then prices a path production does not take. The guard fired on the
// first run of this file, in its own log.
func benchName(i int) string { return fmt.Sprintf("w%02d", i) }

func benchRecord(i int) parkedRecord {
	return parkedRecord{
		ID:     uuid.NewString(),
		Name:   benchName(i),
		Label:  "main",
		Dir:    "/Users/someone/code/api-v2",
		Parked: time.Now(),
	}
}

// benchSocket is a socket path under a directory of this benchmark's own, so
// the park book beside it can never be a real fleet's. Nothing here binds it -
// newServer only records the path - so tempSocket's sun_path discipline does
// not apply and b.TempDir is enough.
func benchSocket(b *testing.B) string {
	b.Helper()
	return filepath.Join(b.TempDir(), "s")
}
