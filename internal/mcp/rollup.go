package mcp

// roll_up: broad awareness, on demand, paid once.
//
// # Why this exists rather than the manager reading the room
//
// Because the arithmetic does not work. At 30 agents and a mean room message of
// 372 characters (~93 tokens), 25 turns per agent per day is 750 messages, 70k
// of context, and 1.4M tokens for twenty questions - and the manager re-reads
// its whole context on every message. The 200k window is exhausted at 71 turns
// per agent, and it degrades *exactly* when it would be most useful, because
// compaction takes the oldest history and the collision worth noticing happened
// hours ago.
//
// And the case that motivated it does not need message history at all. "Peter
// and john working on the same thing" is answerable from what they are *doing*,
// not from what they said: two rows both reading Edit(auth/token.go) is a string
// comparison, and core.ToolCall.Display already carries it, decoded, on the
// wire.
//
// What is genuinely given up is noticing something *semantic* - two agents
// solving one problem in different words with no shared file to give it away.
// Real, rarer than the file case, weakest value per token, and the first thing
// compaction destroys. Revisit with a specific missed case in hand, which is a
// far better basis than a guess.
//
// # Why every byte goes through one function
//
// A digest that can grow without limit is the context it was built to replace,
// arriving in one tool result - so there is a bound, and the bound is only worth
// as much as the number of places that can bypass it. `add` is the only writer;
// TestEveryWriteIntoTheDigestGoesThroughTheBoundedOne fails the build on a
// second one. The version this was written from appended its truncation notice
// with a bare Fprintf after the loop stopped measuring, and overran its own
// bound by the length of one agent line - which its own bound test did not see,
// because the overshoot was smaller than the fixture's slack.

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The two numbers that bound a digest, and the fleet size they are chosen
// against.
//
// # Why the bound is arithmetic rather than a round number
//
// rollUpMaxBytes is a backstop, not a working limit. If the fleet this product
// is *for* could be truncated, the digest would be answering a different
// question from the one it was built for on an ordinary day - so the size is
// derived from the worst arrangement of designFleet agents rather than picked:
// every agent in its own workspace, every line at its maximum. That is
// (1 + 3·designFleet) lines - a headline, then a blank, a workspace header and
// a row each - which is what TestADesignSizedFleetCannotBeTruncatedHoweverItIs
// Arranged asserts over these three constants, with no fixture in it.
//
// digestLineMax bounds one line so that arithmetic exists at all. Without it a
// single Bash command decides how much of the fleet a manager gets to see.
const (
	rollUpMaxBytes = 16 * 1024
	digestLineMax  = agentLineMax + len(rowIndent)

	// designFleet is the 15-30 sessions Wake is built for, at the top of that
	// range. Named here because it is load-bearing arithmetic and not a
	// comment: spec §1 and CLAUDE.md's opening line are where the number comes
	// from.
	designFleet = 30
)

// rowIndent puts an agent under its workspace.
const rowIndent = "  "

// rollUpOrder is the order a digest reads in: what needs a human first.
//
// A digest is read once, so anything that needs acting on has to be above the
// fold or it is not in the digest at all. It has to name every state
// liveSessions offers, which TestEveryStateTheDigestCanBeHandedHasAPlaceInIts
// Order derives from that verdict table rather than restating it: a state
// offered here and unranked falls to the bottom of its workspace, which is
// where a blocked agent must never be.
var rollUpOrder = []string{rpc.StateBlocked, rpc.StateSilent, rpc.StateWorking, rpc.StateIdle}

// rollUpTruncated is what a digest says about the rows it kept back. A model
// reading a short list otherwise believes it is the fleet.
const rollUpTruncated = "\n… %d more agents not shown"

// noAgents is what an empty fleet reads as, on both surfaces that can report
// one. A model reading nothing cannot tell "no agents" from "the tool is
// broken", and only the second is worth retrying - and two spellings of that
// sentence would be two answers to one question.
const noAgents = "No agents are running."

// RollUp is the fleet as a digest: what needs a human, what is in flight, and
// what is quiet - grouped by workspace, ordered so the first line is the one
// worth acting on.
func RollUp(st rpc.Status) string {
	live := liveSessions(st)
	if len(live) == 0 {
		return noAgents
	}

	d := newDigest(len(live))
	// Through add like everything else, so it is inside the bound - and first,
	// because it exists to change how the lines after it are read.
	d.add(agentTextNote)
	d.add(headline(live))
	w, shown := columns(live), 0
	for _, group := range byWorkspace(live) {
		d.add("")
		d.add(fmt.Sprintf("%s (%s)", workspaceLabel(group[0].Dir), agentCount(len(group))))
		for _, s := range inRollUpOrder(group) {
			if d.add(rowIndent + agentLine(s, w)) {
				shown++
			}
		}
	}
	return d.String(len(live) - shown)
}

// headline is the one line a manager reads if it reads nothing else: how many
// agents there are and how many are in each state that has any.
//
// Above the groups because grouping is what can bury the single row worth
// acting on, and because a truncated digest still has to be able to say that
// something is blocked even when the blocked row itself was cut.
func headline(live []rpc.SessionStatus) string {
	counts := map[string]int{}
	for _, s := range live {
		counts[s.State]++
	}
	parts := make([]string, 0, len(rollUpOrder))
	for _, state := range rollUpOrder {
		if counts[state] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
		}
	}
	return fmt.Sprintf("%s: %s.", agentCount(len(live)), strings.Join(parts, ", "))
}

// agentCount is a count with the right noun on it.
func agentCount(n int) string {
	if n == 1 {
		return "1 agent"
	}
	return fmt.Sprintf("%d agents", n)
}

// byWorkspace groups the fleet by the directory each session runs in, most
// urgent workspace first.
//
// **By urgency and not alphabetically**, because grouping is exactly what can
// bury the row the digest exists to surface: an agent blocked in `zed` sits
// under every quiet agent in `api` if the groups are sorted by name. Ordering by
// the most urgent member is still deterministic for a fixed report - which is
// the whole reason there is a sort here at all, Go randomising map iteration -
// and the tie-break is the directory, so two workspaces in the same shape read
// the same way every time.
func byWorkspace(live []rpc.SessionStatus) [][]rpc.SessionStatus {
	order := make([]string, 0, len(live))
	groups := map[string][]rpc.SessionStatus{}
	for _, s := range live {
		if _, seen := groups[s.Dir]; !seen {
			order = append(order, s.Dir)
		}
		groups[s.Dir] = append(groups[s.Dir], s)
	}
	slices.SortStableFunc(order, func(a, b string) int {
		if d := urgency(groups[a]) - urgency(groups[b]); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})

	out := make([][]rpc.SessionStatus, 0, len(order))
	for _, dir := range order {
		out = append(out, groups[dir])
	}
	return out
}

// urgency is the rank of the most urgent session in a group. Lower is more
// urgent, which is rollUpOrder's own direction.
func urgency(group []rpc.SessionStatus) int {
	best := len(rollUpOrder)
	for _, s := range group {
		best = min(best, rank(s.State))
	}
	return best
}

// inRollUpOrder sorts one workspace's agents by what they need, stalest work
// first within a state - the same rule the roster applies, for the same reason:
// an agent on one call for twelve minutes is the one worth naming.
//
// It copies rather than sorting in place. The slice it is handed is a group out
// of the caller's own map and the caller reads it again for the workspace label;
// more to the point, this package's own rule is that a function over a report
// returns a new value rather than reordering somebody else's.
func inRollUpOrder(in []rpc.SessionStatus) []rpc.SessionStatus {
	out := slices.Clone(in)
	slices.SortStableFunc(out, func(a, b rpc.SessionStatus) int {
		if d := rank(a.State) - rank(b.State); d != 0 {
			return d
		}
		// Stalest first, so the agent that has said nothing for twelve minutes
		// is above the one that spoke a second ago.
		return cmp.Compare(b.QuietMS, a.QuietMS)
	})
	return out
}

// rank is where a state sits in the reading order, with anything unranked last.
//
// The fall-through is unreachable by construction - liveSessions offers only
// what rollUpOrder names, and a test derives that from the verdict table rather
// than trusting this sentence - and it is here because a sort comparator has to
// answer for whatever it is handed.
func rank(state string) int {
	if i := slices.Index(rollUpOrder, state); i >= 0 {
		return i
	}
	return len(rollUpOrder)
}

// workspaceLabel is the directory as a person names it. The basename, because a
// full path is what a model has to read past rather than read.
func workspaceLabel(dir string) string {
	if dir == "" {
		return "(unknown directory)"
	}
	return filepath.Base(dir)
}

// digest is the bounded text a roll_up returns.
//
// The reservation is exact rather than a margin: the truncation notice's length
// is known once the number of agents is, so the room kept back for it is the
// most it can ever need. A margin would be a guess, and the guess that fails is
// the fleet of a thousand where the count grows a digit.
type digest struct {
	b    strings.Builder
	room int
	full bool
}

func newDigest(agents int) *digest {
	return &digest{room: rollUpMaxBytes - len(fmt.Sprintf(rollUpTruncated, agents))}
}

// add bounds one line, appends it if what is left will hold it, and reports
// whether it did.
//
// It bounds *every* line rather than trusting each caller to have bounded its
// own, which is what makes the whole-digest arithmetic true of the headline and
// the workspace headers as well as of the rows: a directory basename has no
// length anybody enforces, and neither has a git branch.
//
// Once a line has not fitted, nothing after it does either. The digest is
// ordered so that what is cut is the least urgent thing in it, and letting a
// later short line jump the queue would make the cut arbitrary rather than the
// bottom of the list - a manager would then be reading a sample of the fleet
// with a truncation notice implying a tail.
func (d *digest) add(line string) bool {
	line = oneLine(line, digestLineMax)
	if d.full || d.b.Len()+len(line)+1 > d.room {
		d.full = true
		return false
	}
	d.b.WriteString(line)
	d.b.WriteString("\n")
	return true
}

// String is the digest, with the notice about however many agents were cut.
//
// The notice is concatenated rather than appended through add, which is the one
// deliberate exception and the reason newDigest reserves room for it: it is
// written after the bound has stopped admitting lines, so putting it through add
// would measure it against a budget that has already been spent.
//
// It counts *agents* and not lines, which is why the caller supplies it: a
// workspace header and the blank line above it are lines nobody wants counted as
// agents.
func (d *digest) String(hidden int) string {
	out := strings.TrimRight(d.b.String(), "\n")
	if hidden <= 0 {
		return out
	}
	return out + fmt.Sprintf(rollUpTruncated, hidden)
}
