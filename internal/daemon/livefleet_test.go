package daemon

// The verdict per agent, over a process table this test writes.
//
// Every case here is reachable through a real fleet and only two of them are
// *distinguishable* through one, which is why this is a table rather than more
// integration tests. A zombie has its argv rewritten by every ps that reports
// one - `(cmd)` on darwin, `[cmd] <defunct>` on Linux - so through a running
// daemon the argv arm answers first and deleting the zombie arm changes nothing
// anyone can observe. Verified by mutation: `case p.zombie()` replaced with
// `case false` survived the whole package, including the end-to-end test whose
// entire subject is an agent that became a zombie.
//
// The last two rows are the unknown ones, and they are absence from the map
// rather than false. Only `gone[id] == true` is a fact a caller acts on, so a
// session this cannot speak about has to be silent about rather than cleared -
// noteUnreachable is a flag nothing clears, and the next thing a wrong true
// does is report a healthy agent silent and then SIGKILL its process group at
// shutdown.

import "testing"

func TestWhatCountsAsAnAgentThatHasLostItsProcess(t *testing.T) {
	const (
		live    = 4001
		reaped  = 4002
		keeping = 4003
		reused  = 4004
		absent  = 4005
	)
	// One table for every row, so a verdict is read out of the same machine
	// state the others are - which is what a real pass sees.
	//
	// Two zombies, and the second is the only reason the zombie arm is a guard
	// rather than a comment. Both ps implementations rewrite a zombie's command,
	// so `reaped` is the shape a real listing has - and it is caught by the argv
	// arm whether or not the zombie arm exists, which is how deleting that arm
	// survived every test in this package including the end-to-end one. So
	// `keeping` is the input that isolates it: a zombie whose command line was
	// left intact. Nothing recorded produces that, and that is the point - the
	// arm exists so the verdict does not depend on a ps mangling a string.
	table := map[int]process{
		live:    {state: "S", argv: "claude --session-id " + idAlpha + " --name alex"},
		reaped:  {state: "Z", argv: "(claude)"},
		keeping: {state: "Z", argv: "claude --session-id " + idAlpha + " --name alex"},
		reused:  {state: "S", argv: "/bin/zsh -l"},
	}

	for _, tc := range []struct {
		name string
		w    watched
		gone bool
		why  string
	}{{
		name: "running and still carrying its id",
		w:    watched{id: idAlpha, pid: live},
		gone: false,
		why:  "the process is there and its command line still names the session, which is the whole of being alive",
	}, {
		name: "a zombie whose command ps rewrote",
		w:    watched{id: idAlpha, pid: reaped},
		gone: true,
		why: "a zombie has released every descriptor it held, and it is exactly what an agent becomes when " +
			"core's pump parks in Scan and never reaches Wait - the case the probe was written for",
	}, {
		name: "a zombie whose command line survived",
		w:    watched{id: idAlpha, pid: keeping},
		gone: true,
		why: "the state is what makes it gone, not the argv: this is the only row the zombie arm decides " +
			"on its own, and without it a dead agent whose command line was left intact reads as healthy forever",
	}, {
		name: "the pid is somebody else's now",
		w:    watched{id: idAlpha, pid: reused},
		gone: true,
		why:  "the pid is running a shell, so the agent ended some time ago and the pid was recycled",
	}, {
		name: "not on the machine at all",
		w:    watched{id: idAlpha, pid: absent},
		gone: true,
		why:  "absence from a listing of the whole machine is what gone means here",
	}, {
		name: "a pid Wake never recorded",
		w:    watched{id: idAlpha, pid: 0},
		gone: false,
		why:  "there is no process group to ask about, so nothing was established - and unknown is not gone",
	}, {
		name: "an id Wake could not have minted",
		w:    watched{id: "s1", pid: reused},
		gone: false,
		why: "the argv match is a substring test, so a short id would match any process whose arguments " +
			"happen to contain it - and here it does not, which would read as a recycled pid",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := goneIn(table, []watched{tc.w})
			if got[tc.w.id] != tc.gone {
				t.Errorf("goneIn(%+v) = %t, want %t: %s", tc.w, got[tc.w.id], tc.gone, tc.why)
			}
		})
	}
}

// A verdict is only ever about the agent it was asked about.
//
// The map is keyed by session id and the fleet is asked as a batch, so an arm
// that wrote the wrong key would mark a healthy agent silent because a
// different one died - which at 15-30 sessions is the failure that looks like
// the probe working.
func TestOneAgentsVerdictIsNotWrittenUnderAnothersName(t *testing.T) {
	table := map[int]process{
		5001: {state: "S", argv: "claude --session-id " + idAlpha},
	}
	got := goneIn(table, []watched{
		{id: idAlpha, pid: 5001}, // listed and healthy
		{id: idBeta, pid: 5002},  // not listed at all
	})
	if got[idAlpha] {
		t.Errorf("the live agent %s was marked gone while %s was the one missing from the listing", idAlpha, idBeta)
	}
	if !got[idBeta] {
		t.Errorf("the missing agent %s was not marked gone, so a batch answers only for whoever happens to be first", idBeta)
	}
}
