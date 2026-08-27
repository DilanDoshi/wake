package ui

// Attention: which two of these thirty need you right now.
//
// Pure. Agents in, ranked agents out - no processes, no I/O, no clock. At 20
// agents this ordering *is* the product, and it is the hardest thing in the
// app to be sure about, so it must be testable without spawning anything.
//
// It ranks rpc's own state constants rather than a second enum of its own.
// A parallel liveness vocabulary here would be a hand-written list standing in
// for something the code already declares - the failure decisions.md names -
// and TestEveryStateTheDaemonCanReportHasARank derives the set from
// rpc/lifecycle.go so a seventh state is a test failure rather than a row that
// quietly sorts nowhere.

import (
	"slices"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// attentionRank is the order spec §6 sets, with one correction the corpus
// forced: `silent` sorts *below* idle rather than near the top.
//
// Only `blocked` is evidence. A silent agent is a five-minute timer that has
// expired, and an idle one is provably wrong for an agent working on its own -
// agent.go's own header concedes that an agent which starts a turn under
// --brief "is owed nothing by Wake and reads as idle while it works". Sorting
// a timer above the states it cannot distinguish would put the least reliable
// judgement at the top of the list an operator scans first.
//
// Parked sits between silent and ended, and both sides of that are decisions.
// It is below silent because a silent agent might still be working and a parked
// one provably is not - the process is gone. It is above ended because a parked
// session can be brought back and an ended one cannot, so of the two rows that
// have no process behind them, the recoverable one is the one worth seeing
// first.
var attentionRank = map[string]int{
	rpc.StateBlocked:  0,
	rpc.StateWorking:  1,
	rpc.StateIdle:     2,
	rpc.StateSilent:   3,
	rpc.StateParked:   4,
	rpc.StateEnded:    5,
	rpc.StateOrphaned: 6,
}

// unranked is where a state this build does not know sorts: after everything
// it does. Never a panic and never the top - a version skew must not be able
// to push a blocked agent off the first screen.
//
// It is one past the last rank above rather than a number somebody picked, and
// TestAStateThisBuildDoesNotKnowSortsLastAndNeverFirst is what holds it there:
// adding a state without moving this would put the new one at or past where an
// unknown state sorts, which is the claim inverted.
const unranked = 7

// Rank orders agents by what they need from you, returning a new slice.
//
// Within `working`, stalest first: the agent that has been on one pytest call
// for twelve minutes floats above the one that started thirty seconds ago, so
// stuck agents surface themselves without anyone deciding what stuck means.
// Ties fall back to the order they were first seen in, which is what stops two
// equal rows swapping places between frames.
func Rank(agents []Agent) []Agent {
	out := slices.Clone(agents)
	slices.SortStableFunc(out, func(a, b Agent) int {
		if d := rankOf(a.State) - rankOf(b.State); d != 0 {
			return d
		}
		if a.State != rpc.StateWorking {
			return 0
		}
		switch {
		case a.QuietMS > b.QuietMS:
			return -1
		case a.QuietMS < b.QuietMS:
			return 1
		default:
			return 0
		}
	})
	return out
}

func rankOf(state string) int {
	if r, ok := attentionRank[state]; ok {
		return r
	}
	return unranked
}
