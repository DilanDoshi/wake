package ui

// A fork's transcript opens with a copy of its parent's records under the same
// uuids and times (testdata/transcript/fork-child.jsonl). The restore reads the
// uuid: the same record in two transcripts is one thing a fork copied, never a
// broadcast, and it is drawn once - under the parent when the report names it.

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// record is an event restored from a transcript record with this uuid.
func record(ev core.Event, uuid string) core.Event {
	ev.MessageID = uuid
	return ev
}

// lineage is the attribution with fork is a fork of parent, as the report says.
func lineage(fork, parent string) func(string) Agent {
	return func(id string) Agent {
		a := named(id)
		if id == fork {
			a.ParentID = parent
		}
		return a
	}
}

func restoredAs(agentOf func(string) Agent, batches ...[]core.Event) Room {
	r := NewRoom().SetSize(80, 24)
	for _, b := range batches {
		r = r.Before(roomHistoryLines(b, base.Add(100*time.Hour), agentOf))
	}
	return r
}

// parentTurn is a public turn s1 took before being forked: a broadcast s2 also
// got, and s1's reply to it.
func parentTurn(id string) []core.Event {
	return []core.Event{
		record(opener(id, base), "u-open-"+id),
		record(heard(id, "the parent's answer", base.Add(time.Second)), "u-answer"),
	}
}

// The fork's own turns come back, and the parent's copied answer is drawn once,
// under the parent, whichever reply arrived first.
func TestAForksOwnTurnsComeBackAndItsCopyOfTheParentIsDrawnOnce(t *testing.T) {
	fork := []core.Event{
		// The copy: the parent's records, same uuids, same times, fork's session.
		record(opener("f1", base), "u-open-s1"),
		record(heard("f1", "the parent's answer", base.Add(time.Second)), "u-answer"),
		// The fork's own public turn, which s2 got too.
		record(typed("f1", "@all fork on", base.Add(time.Minute)), "u-fork-on-f1"),
		record(heard("f1", "the fork's own answer", base.Add(time.Minute+time.Second)), "u-fork-answer"),
	}
	second := []core.Event{
		record(opener("s2", base.Add(40*time.Millisecond)), "u-open-s2"),
		record(typed("s2", "@all fork on", base.Add(time.Minute+40*time.Millisecond)), "u-fork-on-s2"),
	}
	for name, order := range map[string][][]core.Event{
		"parent first": {parentTurn("s1"), fork, second},
		"fork first":   {fork, second, parentTurn("s1")},
	} {
		r := restoredAs(lineage("f1", "s1"), order...)
		got := texts(r)
		if n := strings.Count(strings.Join(got, "|"), "the parent's answer"); n != 1 {
			t.Errorf("%s: the parent's answer is drawn %d times, want once: %v", name, n, got)
		}
		if !strings.Contains(strings.Join(got, "|"), "the fork's own answer") {
			t.Errorf("%s: the fork's own turn did not come back: %v", name, got)
		}
		for _, l := range r.said.slice(0, r.said.len()) {
			if l.ev.Text == "the parent's answer" && l.by.ID != "s1" {
				t.Errorf("%s: the copied answer is attributed to %q, want the parent", name, l.by.ID)
			}
		}
	}
}

// The leak this closes: a fork woken from parked has no ParentID (the park book
// holds none), so it is asked about like any session, and its copy of the
// parent's *private* DM turn matches the parent's by text and time. Multiplicity
// read that as a broadcast and put a private turn - and the reply to it - in the
// room. The shared uuid says it is one record.
//
// Mutation check: have forkCopies return nothing and the private turn restores.
func TestAWokenForksCopyOfAPrivateTurnIsNotABroadcast(t *testing.T) {
	private := func(id string) []core.Event {
		return []core.Event{
			record(typed(id, "the thing I told sydney alone", base), "u-private"),
			record(heard(id, "understood, privately", base.Add(time.Second)), "u-private-reply"),
		}
	}
	r := restored(private("s1"), private("f1")) // named: nobody has a ParentID
	for _, text := range texts(r) {
		if strings.Contains(text, "told sydney alone") || strings.Contains(text, "privately") {
			t.Fatalf("a woken fork's copy made a private turn public: %v", texts(r))
		}
	}
}

// A real broadcast is still one: the targets' records are different uuids (a
// send mints one per target), so the dedupe leaves multiplicity to decide.
func TestABroadcastWithAUUIDPerTargetIsStillOneRoomLine(t *testing.T) {
	r := restored(
		[]core.Event{record(typed("s1", "@all stop", base), "u-a")},
		[]core.Event{record(typed("s2", "@all stop", base.Add(80*time.Millisecond)), "u-b")},
	)
	if got := texts(r); len(got) != 1 || got[0] != "@all stop" {
		t.Errorf("a broadcast with a uuid per target came back as %v, want it once", got)
	}
}

// A copy dropped for its keeper still carries that turn's standing into its own
// session: a woken fork kept as the broadcast's copy (it arrived first) must not
// leave the parent's own later prose, in the same public turn, unopened.
func TestADroppedCopyStillOpensItsOwnSessionsTurn(t *testing.T) {
	fork := []core.Event{
		record(opener("f1", base), "u-open-s1"),
		record(heard("f1", "the parent's answer", base.Add(time.Second)), "u-answer"),
	}
	parent := []core.Event{
		record(opener("s1", base), "u-open-s1"),
		record(heard("s1", "the parent's answer", base.Add(time.Second)), "u-answer"),
		// Said by the parent after the fork, still inside that public turn.
		record(heard("s1", "and one more thing, after the fork", base.Add(time.Minute)), "u-after"),
	}
	second := []core.Event{record(opener("s2", base.Add(40*time.Millisecond)), "u-open-s2")}
	r := restored(fork, second, parent) // named: no ParentID, so the first to arrive is kept
	if got := strings.Join(texts(r), "|"); !strings.Contains(got, "after the fork") {
		t.Errorf("the parent's own prose in a public turn was dropped because its copy of the opener was: %v", texts(r))
	}
}

// A fork whose parent is not running is not asked about: only the fork would
// hold the records it inherited, so they would be drawn under the fork's name -
// a direct `@parent ...` readdressed to the fork. Asked again once the parent is
// live, with its copy drawn once under the parent.
func TestTheRoomAsksAboutAForkOnlyWhileItsParentIsLive(t *testing.T) {
	st := &rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: "f1", Name: "juno", State: rpc.StateIdle, ParentID: "s1"},
		{ID: "f2", Name: "nora", State: rpc.StateIdle, ParentID: "gone"},
	}}
	if got := strings.Join(liveSessions(st), ","); got != "s1,f1" {
		t.Errorf("the room would ask about %q, want the parent and the fork whose parent is live", got)
	}
}
