package mcp

// What the manager's broadest question costs, at the fleet size Wake is for.
//
// roll_up walks the whole fleet on every call, and it is the one tool a
// manager reaches for repeatedly - "what is everybody doing" is the question
// the session exists to answer. So this is the cost of the surface Phase 3
// added, priced against a View that costs ~250µs and an Observe that costs
// tens of nanoseconds.
//
// # There is no clock in this package, so there is no idle cost to measure
//
// TestNothingInThisPackageKeepsTime derives that from the source: nothing here
// caches a snapshot on a ticker, polls for a change or sleeps between retries.
// An answer is produced when a model asks and at no other time - so a `wake
// mcp` process beside thirty agents costs exactly this, times how often the
// manager asks, and zero in between. That is the whole of what "cheap to leave
// open" means for a query surface, and it is why the numbers below are per
// call rather than per second.
//
// Run: go test ./internal/mcp -run XXX -bench 'RollUp|ListAgents' -count 5

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// benchSink keeps the compiler from eliding a digest nobody reads.
var benchSink string

// BenchmarkRollUp is the digest at the design fleet size, and then the same
// fleet with the manager on it.
//
// # What the pairing isolates
//
// Exactly one row. Both arms hold designFleet agents with identical ids,
// names, directories, states and tool arguments; the second adds the manager's
// own session to the report, which is what every fleet report actually carries
// once `wake manager` has been run.
//
// The interesting half is that liveSessions **excludes** the manager, so the
// second arm walks 31 rows and emits 30. The difference between the two arms
// is therefore the cost of filtering a row out - not the cost of a 31st agent
// - and if the two ever diverge by more than that, something has started
// treating the manager as an agent.
//
// # What it does not isolate
//
// Not the manager's `claude` process, which is a 31st process on the machine
// and is not in this address space at all. Not the socket round trip either:
// RollUp is pure, and `wake mcp` dials the daemon for the rpc.Status this is
// handed. The daemon's side of that is internal/daemon's own measurement.
//
// spread=1 and spread=30 are the two arrangements of the same fleet, because
// byWorkspace groups and one group of thirty is a different walk from thirty
// groups of one - and the second is the arrangement rollUpMaxBytes is derived
// against.
func BenchmarkRollUp(b *testing.B) {
	for _, spread := range []int{1, designFleet} {
		for _, withManager := range []bool{false, true} {
			b.Run(fmt.Sprintf("workspaces=%d/manager=%t", spread, withManager), func(b *testing.B) {
				st := benchStatus(spread, withManager)
				b.ReportAllocs()
				for b.Loop() {
					benchSink = RollUp(st)
				}
				assertWholeFleet(b, benchSink)
			})
		}
	}
}

// BenchmarkListAgents is the other reading tool, for scale: the same walk with
// no grouping and no ordering. It is the tool a manager calls most, because
// every acting tool needs an id from it first.
func BenchmarkListAgents(b *testing.B) {
	st := benchStatus(designFleet, true)
	b.ReportAllocs()
	for b.Loop() {
		benchSink = framed(agentLines(liveSessions(st)))
	}
	assertWholeFleet(b, benchSink)
}

// benchStatus is a design-sized fleet spread over n workspaces, optionally
// with the manager's own row on it - which is what a report from a daemon
// serving a managed fleet actually looks like.
func benchStatus(workspaces int, withManager bool) rpc.Status {
	sessions := manyAgents(designFleet, func(i int) string {
		return fmt.Sprintf("/Users/someone/code/repo-%d", i%workspaces)
	})
	if withManager {
		sessions = append(sessions, rpc.SessionStatus{
			ID:    "1e5c1b8a-0000-4000-8000-ffffffffffff",
			Name:  core.ManagerName,
			Dir:   "/Users/someone/code/repo-0",
			State: rpc.StateIdle,
		})
	}
	return rpc.Status{Running: true, Sessions: sessions}
}

// assertWholeFleet fails a benchmark whose output stopped being a whole fleet.
//
// A digest that truncated, or a walk that started skipping rows, would report
// as an improvement: less work, faster number, no failure. The count is over
// the id prefix every fixture shares, so a workspace header is not mistaken
// for an agent.
func assertWholeFleet(b *testing.B, out string) {
	b.Helper()
	if n := strings.Count(out, agentIDPrefix); n != designFleet {
		b.Fatalf("%d of %d agents in the output: this benchmark stopped pricing a whole fleet", n, designFleet)
	}
}
