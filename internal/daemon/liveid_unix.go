//go:build unix

// Asking the operating system whether anything is still running one session.
//
// Distinct from reap_unix.go's inspect, which asks about a *pid* Wake recorded.
// The question here has no pid in it: a second process resuming an id is one
// Wake never spawned and has no handle to, so the only way to see it is to look
// at every command line on the machine. One ps for the whole question, never
// one per id - docs/notes/deferred.md's power section already names the
// daemon's ~86,400 ps spawns a day as the cost that scales, and this runs on an
// operator's keystroke rather than on a timer.

package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
)

// idsInUse reports which of these session ids a live process is running.
//
// An error is "I could not check", and every caller must treat it as a refusal
// rather than as "nothing is there". A machine without a usable ps loses the
// ability to wake a session; it must not gain the ability to wake one twice.
//
// The match is core.SessionArgvMarkers' rather than the bare id, and that is
// what stops a false positive on Wake's own clients: `wake attach <uuid>`
// carries the id in its argv with no flag in front of it, and the one moment
// this is asked is right after somebody parked the session they had attached
// to by id.
//
// -Aww for the same reason reap_unix.go passes -ww: without it ps truncates
// each command to the terminal width, and the identity flags sit near the end
// of a long claude command line. A truncated listing would answer "nothing is
// holding it" every single time, which is the unsafe direction.
//
// An empty listing is refused rather than read as "nothing is running", and
// that is inspect's ruling applied to a different question. `ps -A` on the
// machine this daemon is running on lists at least this daemon, so nothing can
// legitimately produce it - and the one implementation that does (exit 0,
// silence, no stock ps behaves this way) would otherwise answer "the id is
// free" for every id on every call.
func idsInUse(ctx context.Context, ids []string) (map[string]bool, error) {
	out, err := exec.CommandContext(ctx, "ps", "-Aww", "-o", "command=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps -A: %w", err)
	}
	if blank(out) {
		return nil, fmt.Errorf("ps -A exited 0 and listed no process at all, which cannot be true of the machine this daemon is running on")
	}
	listing := string(out)
	in := make(map[string]bool, len(ids))
	for _, id := range ids {
		for _, marker := range core.SessionArgvMarkers(id) {
			if strings.Contains(listing, marker) {
				in[id] = true
				break
			}
		}
	}
	return in, nil
}
