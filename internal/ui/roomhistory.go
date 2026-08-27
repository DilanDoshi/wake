package ui

// The room coming back with what was said.
//
// A conversation has restored itself from claude's transcript since
// history.go; the room never has. `⌃Q`, then `wake`, then `/resume all` left a
// working fleet above an empty group chat, and so did reopening a room over a
// fleet that had been running since before the window existed.
//
// # Why it is re-derived rather than kept
//
// "Wake owns almost no state" is a non-negotiable, and the four things it does
// own are each something nothing else records. What was said in the room is
// recorded, once per participant, in the files claude already writes. The cost
// of that choice is this file: the transcripts are per session, so the room's
// order has to be reconstructed from timestamps and its filter re-applied.
//
// # The filter is fold, not a copy of fold
//
// The room is a filter rather than a log - thinking, tool calls and diffs were
// never in it - so a restored prefix built any other way would be a *different*
// room above the live one. Every event goes through ui.fold with a scratch
// Agent per session, which is the same pure function App.observe uses.

import (
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// roomHistoryFailed names the write that could not happen, so the notice row
// says what was being asked for rather than only what the socket said about it.
const roomHistoryFailed = "asking what the room already said"

// roomAsk is the room's ledger: which sessions it has asked about, when each
// ask went out, and which frames are queued to go.
//
// One App field rather than three, and copied on write for the reason App.dms
// is - Bubble Tea hands models around by value, so a shared map makes a
// discarded App's decisions stick to the live one.
//
// **It is its own ledger and not history.go's**, which is the whole reason
// rpc.FrameRoomHistory exists: askHistory is once per session per client, and a
// room sharing it would spend the ask a conversation opened later needs.
type roomAsk struct {
	// at is what each session was asked with. Per session rather than one clock
	// and one count for the room: a wake an hour in must not be measured
	// against the moment the seed went out.
	at map[string]askedAt

	// pending is what takeHistoryAsks has yet to write.
	pending []string
}

// askedAt is the state one ask is folded against.
type askedAt struct {
	// cutoff is when the ask went out. A transcript event stamped at or after
	// it may also have arrived on the socket.
	cutoff time.Time

	// lines is how many room lines this session had when the ask went out, and
	// it is the guard the cutoff cannot be: the cutoff is stamped *before* the
	// frame is written, and the frames a reattach read past on its way in are
	// replayed into the model *after* it exists - so an event timestamped
	// before the cutoff can still reach the room after it, and both copies get
	// drawn. If this session has said anything in the meantime its whole batch
	// is dropped.
	//
	// It is always zero, because askRoomHistory refuses a session that already
	// has lines on screen. Kept as a field rather than folded into a bool so the
	// comparison at the other end stays a comparison: what it is really asking
	// is "has the count moved", and a literal 0 there would be a coincidence
	// somebody could break by relaxing that refusal.
	//
	// Per session rather than for the whole room, which is the one thing that
	// makes it usable here: history.go's guard watches one pane, and a room over
	// thirty streaming agents would drop every round on the first one to speak.
	//
	// The trade is history.go's own, unchanged: losing the history is the safe
	// side, because a conversation that appears to have happened twice is worse
	// than one that appears not to have happened at all.
	lines int
}

// askRoomHistory queues one ask per session the room has not asked about.
//
// Called from exactly two places, which between them are every way a session
// arrives into a room that is missing what it said: NewRoomApp's seed, and
// wakeArrived. **Not a spawn and not a fork** - a fresh agent has no transcript
// and a fork's is its parent's, which the room already draws under the parent,
// so folding it would put an hour of the same prose on screen twice under two
// names.
func (a App) askRoomHistory(ids ...string) App {
	next := roomAsk{at: make(map[string]askedAt, len(a.roomAsk.at)+len(ids))}
	for k, v := range a.roomAsk.at {
		next.at[k] = v
	}
	next.pending = append([]string(nil), a.roomAsk.pending...)

	now := clock()
	for _, id := range ids {
		if _, asked := next.at[id]; id == "" || asked {
			continue
		}
		// A session the room already holds lines for is refused outright, the
		// way a fork is. Its transcript covers the same turns that are on
		// screen, and nothing de-duplicates a restored line against a live one -
		// so this is a park and a wake inside one window drawing everything that
		// session said before the park a second time. The count is zero at the
		// seed, which is when a session that predates this window is asked
		// about; anything with lines already drawn arrived after it.
		if a.room.linesFor(id) > 0 {
			continue
		}
		next.at[id] = askedAt{cutoff: now, lines: 0}
		next.pending = append(next.pending, id)
	}
	a.roomAsk = next
	return a
}

// takeRoomHistoryAsks drains the queue into one command, beside the
// conversation's. Queued rather than written where the decision is made, for
// takeHistoryAsks' reason: the two callers are a constructor and a status fold,
// and neither is in a position to return a tea.Cmd.
func (a App) takeRoomHistoryAsks() (App, tea.Cmd) {
	if len(a.roomAsk.pending) == 0 {
		return a, nil
	}
	frames := make([]rpc.Frame, 0, len(a.roomAsk.pending))
	for _, id := range a.roomAsk.pending {
		frames = append(frames, rpc.Frame{Kind: rpc.FrameRoomHistory, SessionID: id})
	}
	next := a.roomAsk
	next.pending = nil
	a.roomAsk = next
	return a, a.write(roomHistoryFailed, frames...)
}

// roomHistoryArrived folds one session's transcript into the room.
//
// Answered once. A reply for a session nothing asked about is dropped, and so
// is a second answer to the same ask: a daemon that answered twice, or a
// second client's reply arriving after a reattach, would otherwise draw the
// same conversation again underneath the first copy.
//
// Folded as it lands rather than held until every session has answered. The
// merge is what makes that safe - Room.Before puts each batch where its
// timestamps say it goes - and waiting on a fleet of thirty would mean one
// unanswerable session kept the whole room empty.
func (a App) roomHistoryArrived(f rpc.Frame) App {
	ask, asked := a.roomAsk.at[f.SessionID]
	if !asked || len(f.Events) == 0 {
		return a
	}
	a = a.roomHistoryAnswered(f.SessionID)
	// Dropped whole if this session has said anything since the ask: its live
	// copy is already on screen and the transcript holds the same turn. See
	// askedAt.lines.
	if a.room.linesFor(f.SessionID) != ask.lines {
		return a
	}
	return a.withRoom(a.room.Before(roomHistoryLines(f.Events, ask.cutoff, a.agentFor)))
}

// roomHistoryAnswered takes the ask off the ledger while leaving the session on
// it, so a second reply is dropped and a second *ask* is still refused.
func (a App) roomHistoryAnswered(id string) App {
	// pending is copied rather than shared, matching the two functions either
	// side of this one. Nothing appends into it in place today, so this is the
	// convention rather than a fix - and the convention is what the next
	// mutating path gets to lean on.
	next := roomAsk{
		at:      make(map[string]askedAt, len(a.roomAsk.at)),
		pending: append([]string(nil), a.roomAsk.pending...),
	}
	for k, v := range a.roomAsk.at {
		next.at[k] = v
	}
	// The zero value answers both questions at once: `asked` stays true, so no
	// second ask goes out, and the zero cutoff is never After any event, so a
	// second reply folds nothing even if the line count happens to match.
	next.at[id] = askedAt{}
	a.roomAsk = next
	return a
}

// agentFor is how a restored line is attributed: the live fleet's answer, or a
// bare id for a session the fleet has not named. Never the record rebuilt from
// the transcript, which would be a claim about what an agent is doing now made
// from an hour-old file.
func (a App) agentFor(id string) Agent {
	if agent, ok := a.fleet.Agent(id); ok {
		return agent
	}
	return Agent{ID: id}
}

// liveSessions is every session in a report the room should ask about: the ones
// that are running and are not forks. Parked rows are disjoint from these by
// construction - see rpc.Status.Parked - and an ended one has nothing to come
// back to.
func liveSessions(st *rpc.Status) []string {
	if st == nil {
		return nil
	}
	out := make([]string, 0, len(st.Sessions))
	for _, s := range st.Sessions {
		if s.State != rpc.StateEnded && !isFork(s) {
			out = append(out, s.ID)
		}
	}
	return out
}

// isFork is a session whose transcript is somebody else's conversation with its
// own tail on the end.
//
// A fork is `--resume <parent> --fork-session`, so its file opens with every
// line the parent had at the moment it was taken. Restoring it puts an hour of
// the parent's prose on screen a second time under a new name - and the parent
// is usually right there in the same report.
//
// It is checked in both places a room ask is decided, which is the correction an
// adversarial review made: the ruling was written down and only the *later*
// status push enforced it, because askRoomHistory is never reached from an
// ordinary report. A fork sitting in the seed, or one coming back from parked,
// went straight through. What it costs is that a fork's own conversation after
// the fork point does not come back either; deferred.md records why that is the
// cheaper half.
func isFork(s rpc.SessionStatus) bool { return s.ParentID != "" }

const (
	// roomRawEvents bounds Room.raw, and it is a memory backstop rather than a
	// display bound. Restored and live display events share chat.go's one cap.
	//
	// raw is what the turn rule reads, and trimming it from the front **evicts
	// the broadcast that opens a turn**. A cut through a same-text anchor run can
	// also re-anchor its retained suffix and turn a previously private copy
	// public, so trimRoomRaw advances through the whole run before collapsing.
	// This backstop stays independent of what the pane retains.
	//
	// It may retain fewer than this many events to keep pruning privacy-monotonic,
	// but never more.
	roomRawEvents = 1_600

	// broadcastWindow is how far apart two transcripts may record the same
	// words and still be one thing somebody said.
	//
	// A broadcast is one write fanned out to N agents in a single pass, so the
	// records land within milliseconds of each other; five seconds is generous
	// against a machine under load rather than a judgement about typing speed.
	broadcastWindow = 5 * time.Second
)

// roomHistoryLines turns raw transcript events into the lines the room draws.
//
// cutoff is the moment the ask went out. Anything at or after it may also have
// arrived on the socket, and claude writes a turn to its transcript *and*
// streams it - so folding one would draw the same turn twice, once above the
// other. An event with no time at all is dropped for the same reason read the
// other way: it cannot be placed among the rest, and a restored line in the
// wrong place is a conversation nobody had.
//
// agentOf attributes a line. It is passed rather than read from a Fleet held
// here because attribution is the *live* fleet's answer - an agent's label
// changes when it changes branch, and the room stores the pairing rather than
// re-deriving it later for exactly that reason.
// It does **not** collapse broadcasts and does not cap. Both are Room.Before's,
// and that is a correction rather than a layering preference: the daemon answers
// **one transcript per frame**, so a rule that has to see two of them cannot run
// where a reply is folded. It ran here, where `senders` could never hold more
// than one session, and every restored user turn was silently dropped - green
// against a unit test that handed this function three sessions in one slice, a
// shape nothing on the wire produces.
func roomHistoryLines(events []core.Event, cutoff time.Time, agentOf func(string) Agent) []roomLine {
	kept := make([]core.Event, 0, len(events))
	for _, ev := range events {
		if ev.At.IsZero() || !ev.At.Before(cutoff) {
			continue
		}
		kept = append(kept, ev)
	}
	// By time, then by session, so two records stamped the same millisecond
	// come back in the same order on every run rather than in map order.
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].At.Equal(kept[j].At) {
			return kept[i].SessionID < kept[j].SessionID
		}
		return kept[i].At.Before(kept[j].At)
	})

	// One scratch Agent per session, because fold is stateful per agent and
	// each session's events reach it in that session's own order. The value it
	// carries is thrown away: the live fleet already holds the real one, and a
	// record rebuilt from an hour-old transcript would be a claim about what an
	// agent is doing *now*.
	scratch := map[string]Agent{}
	lines := make([]roomLine, 0, len(kept))
	for _, ev := range kept {
		a, known := scratch[ev.SessionID]
		if !known {
			a = Agent{ID: ev.SessionID}
		}
		next, out := fold(a, ev, ev.SessionID)
		scratch[ev.SessionID] = next
		for _, e := range out {
			lines = append(lines, roomLine{ev: e, by: agentOf(ev.SessionID)})
		}
	}

	return lines
}

// trimRoomRaw applies the raw memory backstop without changing how the retained
// suffix is partitioned into same-text anchor runs. The nominal tail boundary
// may split several interleaved runs transitively, so the cut advances to a
// fixed point. Refused and ambiguous runs count too: pruning must not make a
// previously unprovable turn provable under a new anchor.
func trimRoomRaw(lines []roomLine) []roomLine {
	cut := max(len(lines)-roomRawEvents, 0)
	if cut == 0 {
		return lines
	}
	runs := broadcastRuns(lines)
	for {
		next := cut
		for _, run := range runs {
			if run[0] < cut && run[len(run)-1] >= cut {
				next = max(next, run[len(run)-1]+1)
			}
		}
		if next == cut {
			break
		}
		cut = next
	}
	kept := make([]roomLine, len(lines)-cut)
	copy(kept, lines[cut:])
	return kept
}

// collapseBroadcasts decides what happens to the turns *you* typed.
//
// A user turn on disk is indistinguishable from a private one: sendRoom strips
// a leading @name before writing, and FromRoom is presentation-only and on no
// wire - so the same bytes are `@all fix the tests` sent to the room and `fix
// the tests` typed into a conversation pane. This project has a shipped ruling
// that a DM is private, so drawing every one of them in the room is out.
//
// What *is* decidable is multiplicity. One broadcast to N agents is the same
// text, at the same moment, in N files. So a turn found in two or more
// transcripts within broadcastWindow comes back once, attributed to nobody -
// which is how the room draws a turn you typed - and a turn found in one is
// dropped.
//
// It errs toward silence, which is the safe direction of the two: what it costs
// is that `@noah do this` does not come back, and what the other direction
// costs is a private turn on a shared surface.
//
// # The turn is the unit, not the line
//
// Deciding this line by line hid the question and showed the answer. A turn
// typed into a conversation pane was dropped here, and the agent's reply to it -
// the same conversation, in the agent's words - went into the group chat anyway,
// at transcript scale. Live, App.observe keeps a whole DM-sent turn out of the
// room through Fleet.inDM; nothing on disk records which surface a turn was
// typed on, so the restore has to carry provenance itself.
//
// So an agent's prose is restored **only while the user turn it belongs to is
// one two transcripts prove was a broadcast**, and a session's turn is closed by
// the next user turn in its own transcript. Prose with no initiator in the
// window at all is dropped, which is most of a 400-event tail - keeping it would
// reopen the leak exactly where it is widest. deferred.md records what that
// costs.
//
// # It runs over every transcript the room has restored, not over one reply
//
// The daemon answers one session per frame, so a copy of a broadcast arrives in
// its own frame, minutes of wall clock apart from its siblings on a slow fleet.
// Room.raw is what it is given, which is why that field exists.
//
// The lines are already in time order, so grouping by text costs one pass and
// the clusters inside a group are contiguous.
//
// lines is Room.raw's newly merged private copy. The function annotates every
// member of a currently proved cluster with its logical identity; annotations
// on refused or no-longer-public runs are cleared.
func collapseBroadcasts(lines []roomLine, nextID *uint64) []roomLine {
	firsts, public, clusters := broadcastIndex(lines)

	// Reuse an identity only when exactly one old logical cluster contributes
	// physical members to exactly one current cluster. This preserves a proved
	// broadcast through reply reordering and raw-front eviction without joining
	// clusters whose membership actually merged or split.
	candidates := make(map[int]uint64, len(clusters))
	claims := make(map[uint64]int, len(clusters))
	retainedMembers := make(map[uint64]int, len(clusters))
	for _, line := range lines {
		if line.broadcastID != 0 {
			retainedMembers[line.broadcastID]++
		}
	}
	starts := make([]int, 0, len(clusters))
	for start, members := range clusters {
		starts = append(starts, start)
		inside := map[uint64]int{}
		for _, i := range members {
			if id := lines[i].broadcastID; id != 0 {
				inside[id]++
			}
		}
		if len(inside) == 1 {
			for id, count := range inside {
				if count == retainedMembers[id] {
					candidates[start] = id
					claims[id]++
				}
			}
		}
	}
	for i := range lines {
		lines[i].broadcastID = 0
	}
	sort.Ints(starts)
	identities := make(map[int]uint64, len(clusters))
	for _, start := range starts {
		identity := candidates[start]
		if identity == 0 || claims[identity] != 1 {
			(*nextID)++
			identity = *nextID
		}
		identities[start] = identity
		for _, i := range clusters[start] {
			lines[i].broadcastID = identity
		}
	}

	// Per session, because a turn is a property of one transcript. False until
	// a broadcast opens one: a session whose window starts mid-conversation has
	// nothing saying its prose was ever public.
	open := make(map[string]bool, len(lines))
	out := make([]roomLine, 0, len(lines))
	for i, l := range lines {
		if l.ev.Kind != core.KindUserText {
			if open[l.ev.SessionID] {
				out = append(out, l)
			}
			continue
		}
		open[l.ev.SessionID] = public[i]
		if firsts[i] {
			// The chronological representative can change when an earlier
			// transcript reply arrives late or the raw backstop evicts one member.
			// The proved cluster carries a separate logical rendered-block identity.
			l.id = identities[i]
			// The operator's own turn has no speaker, the same way a live echo
			// into the room does not. Under one agent's name it would read as
			// that agent quoting you.
			l.by = Agent{}
			out = append(out, l)
		}
	}
	return out
}

// broadcastIndex reads every user line and answers two questions about it:
// whether it is the copy to *draw* (firsts), and whether it was part of a
// fan-out at all (public), which is what opens its session's turn.
// clusters exposes only the membership of runs those same rules proved public;
// it does not broaden the decision.
//
// Two answers rather than one because they are different sets: one broadcast to
// thirty agents is thirty public turns and one line on screen.
//
// A run is measured from its own first copy rather than from each candidate, so
// a fan-out slower than the window becomes two broadcasts rather than a chain of
// overlapping ones. A run that only ever names one session yields nothing, which
// is what drops a turn typed privately - and what drops the same words typed
// twice into one conversation.
//
// **A session appearing twice in one run proves nothing and the whole run is
// refused.** One broadcast writes once per transcript, so a repeat means two
// separate sends and nothing says which of them was the public one. Counting
// distinct senders alone was a leak rather than a miss: typing `status` into a
// conversation and then broadcasting `status` seconds later put both copies and
// s2's in one run, made it public on two distinct senders, and promoted the
// *earliest* - the private one - to the line the room draws, which opened its
// turn and restored the reply to it. Refusing costs a real broadcast sent twice
// inside five seconds, which is the safe direction.
func broadcastIndex(lines []roomLine) (firsts, public map[int]bool, clusters map[int][]int) {
	firsts, public, clusters = map[int]bool{}, map[int]bool{}, map[int][]int{}
	for _, at := range broadcastRuns(lines) {
		start := at[0]
		senders := map[string]struct{}{lines[start].ev.SessionID: {}}
		repeat := false
		for _, k := range at[1:] {
			id := lines[k].ev.SessionID
			if _, twice := senders[id]; twice {
				repeat = true
			}
			senders[id] = struct{}{}
		}
		if len(senders) > 1 && !repeat {
			firsts[start] = true
			clusters[start] = append([]int(nil), at...)
			for _, k := range at {
				public[k] = true
			}
		}
	}
	return firsts, public, clusters
}

// broadcastRuns partitions every eligible same-text user line from its own
// first copy. It deliberately does not decide whether a run is public: raw
// pruning needs refused and repeated-session runs as well as accepted ones.
func broadcastRuns(lines []roomLine) [][]int {
	byText := map[string][]int{}
	for i, l := range lines {
		// Every decoded image carries the one placeholder text, so two private
		// image sends to two agents inside the window would read as one text from
		// two senders and be promoted to a broadcast - restoring a private image
		// to the room and, for an image-only turn, opening it so the agent's
		// following private prose leaks in. Text multiplicity can never be sound
		// proof for an image, so it is excluded here rather than trusted: an image
		// is dropped from the room the way any unprovable turn is.
		if l.ev.Kind == core.KindUserText && l.ev.Text != core.ImagePlaceholder {
			byText[l.ev.Text] = append(byText[l.ev.Text], i)
		}
	}
	var runs [][]int
	for _, at := range byText {
		for i := 0; i < len(at); {
			start := at[i]
			j := i + 1
			for ; j < len(at) && lines[at[j]].ev.At.Sub(lines[start].ev.At) <= broadcastWindow; j++ {
			}
			runs = append(runs, append([]int(nil), at[i:j]...))
			i = j
		}
	}
	return runs
}
