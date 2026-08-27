// Whether any of this daemon's own agents has lost its process while its
// session is still open.
//
// # Why this is not reap.go's question
//
// The reaper asks about a pid it did **not** spawn, read off a file a dead
// daemon wrote. It has no fleet, no parent relationship and no way to bound the
// answer, so it asks once per pid and has to defend against pid reuse with an
// argv match. That is the right shape for that caller and it is what agentGone
// was: one `ps -p <pid>` per session, per ask.
//
// The watchdog is a different caller asking a different question. These are
// **this process's own children**, it holds all of them at once, and it asks on
// a schedule rather than on an operator's verb. Inheriting the reaper's tool
// made the ask cost one process per agent per tick - 86,400 a day at 30 agents,
// which is the non-negotiable's *no process spawned on a timer* and its *a
// per-agent cost multiplies by 30*, both at once. One `ps` answers the whole
// fleet for the same price as one agent, which is the same ruling liveid_unix.go
// already made for its own question, and it is why agentGone was replaced rather
// than kept beside this.
//
// # What survives the change, unchanged
//
// The three-valued contract, which is the part that must not be traded for the
// cost. "Gone" is a fact the caller acts on by reporting a session silent, and
// "I could not check" must never be mistaken for it: a machine whose ps cannot
// answer would otherwise have its whole fleet declared dead, marked unreachable
// and then SIGKILLed by process group at shutdown instead of stopped gently.
// So a lookup that fails is an **error for the whole pass** and changes nothing,
// and an agent this cannot speak about is simply absent from the map. Only
// `gone[id] == true` means anything.
//
// A zombie counts as gone, for agentGone's reason: it has released every
// descriptor it held, and it is precisely what an agent becomes when core's
// pump parks in Scan and never reaches Wait - the one case this exists for.
//
// And the argv match is kept, though this caller is the one that could most
// nearly justify dropping it: an unreaped child's pid cannot be recycled, so a
// listed pid really is the agent. "Most nearly" is not "provably", the check is
// free once the listing is in hand, and reading a stranger's process as a live
// agent is the direction that hides a dead one.
//
// # Two of the checks here are redundant, and this says which
//
// A pid absent from the table yields the zero process, whose empty argv fails
// the argv match - so `case !listed` and the argv arm return the same answer for
// an unlisted pid, and replacing the first with a constant false is invisible.
// It is kept because relying on that is relying on a zero value two arms away:
// the mutation that actually matters is `case !listed: continue` - *"not listed
// means we do not know"*, a sentence somebody writes - and that one is caught.
// The same is true one file over, where the empty-listing refusal is subsumed by
// the missing-daemon one. Both are stated rather than deleted because each names
// a different failure a reader has to be able to find, and neither is claimed to
// be independently load-bearing.

package daemon

import (
	"context"
	"strings"
)

// watched is one session the liveness probe is asking about: the id Wake
// minted for it, and the process group core recorded at spawn.
//
// A pair rather than an *agent, so the question can be asked - and the answer
// tested - without a fleet, a daemon or a lock.
type watched struct {
	id  string
	pid int
}

// goneNow reports which of these agents' processes have ended even though
// their sessions have not.
//
// An error means the machine could not be asked, and every caller must treat it
// as "nothing changed" rather than as "nothing is there". An id missing from
// the map is the same answer for one agent.
//
// The id has to be one Wake minted, exactly as in verifyAgent. Every caller's
// id came through maySpawn, so this cannot fire today - but the argv test below
// is a strings.Contains against a live command line, and a short or ordinary id
// would match any process whose arguments happen to contain it.
func goneNow(ctx context.Context, fleet []watched) (map[string]bool, error) {
	if len(fleet) == 0 {
		// Nothing to ask about, so nothing is spawned. The caller checks this
		// too; here as well because "no agents" must never reach ps.
		return nil, nil
	}
	table, err := processTable(ctx)
	if err != nil {
		return nil, err
	}
	return goneIn(table, fleet), nil
}

// goneIn is that verdict as a pure function of a process table, which is what
// makes each arm of it separately falsifiable.
//
// Split from the ask rather than inlined, because the three ways an agent can be
// gone are not equally reachable through a real ps and two of them overlap. Every
// ps that reports a zombie rewrites its command - `(cmd)` on darwin, `[cmd]
// <defunct>` on Linux - so through a running fleet the argv arm answers first and
// the zombie arm decides nothing anyone can observe. Verified by mutation:
// `case p.zombie()` replaced with `case false` survived the whole package,
// including the end-to-end test whose entire subject is an agent that became a
// zombie. Over a table the arm can be handed the one input that isolates it.
func goneIn(table map[int]process, fleet []watched) map[string]bool {
	gone := make(map[string]bool, len(fleet))
	for _, w := range fleet {
		if w.pid <= 1 || !mintedByWake(w.id) {
			// Unknown, which is absence rather than false: nothing was
			// established about this session either way.
			continue
		}
		p, listed := table[w.pid]
		switch {
		case !listed:
			gone[w.id] = true
		case p.zombie():
			gone[w.id] = true
		case !strings.Contains(p.argv, w.id):
			// The pid is somebody else's now, so this agent ended some time ago.
			gone[w.id] = true
		}
	}
	return gone
}
