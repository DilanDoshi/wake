//go:build unix

// One listing of every process on the machine, which is one ask however many
// agents the answer is for.

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// processTable is every process the machine will admit to, keyed by pid.
//
// -Aww for the reason reap_unix.go passes -ww and liveid_unix.go passes -Aww:
// without it ps truncates each command to the terminal width, and the
// --session-id this turns on sits near the end of a long claude command line. A
// truncated listing would fail the argv match for every agent at once, and this
// caller reads a failed argv match as **gone** - so the truncation that is
// merely a false negative over there is a whole fleet declared dead here.
//
// # Why a listing that cannot be trusted is refused rather than read
//
// The per-pid lookup this replaced could afford to be delicate about it:
// `ps -p <pid>` answers about one process, so "there is no such pid" and "ps
// could not answer" had to be separated by the shape of the failure - silence
// on both streams, and Exited() to rule out the probe this package killed
// itself. inspect still does exactly that, for the reaper.
//
// A whole-machine listing is a blunter instrument and needs a blunter guard,
// because absence from it is what "gone" *means* here. A ps that answers
// partially would declare live agents dead, and there is no per-pid shape left
// to notice it by. Two refusals cover it, and both are about the listing rather
// than about any agent in it:
//
//   - **Nothing at all.** No stock ps exits 0 saying nothing, and `ps -A` on the
//     machine this daemon is running on lists at least this daemon. This is
//     liveid_unix.go's ruling, and it is the one that stops a broken ps
//     answering "gone" for every id on every call.
//   - **A listing that does not contain the asker.** The daemon is a process on
//     this machine, so a listing without its own pid in it is not a listing of
//     this machine - and it is the only self-check available that costs nothing
//     and cannot be satisfied by an implementation that merely truncates. It
//     turns "some processes are missing" from an answer into a refusal.
//
// Neither is reachable by a working ps, which is what makes them safe to fail
// closed on. The first is **subsumed by the second** - an empty listing has no
// daemon in it either - and is kept for the message rather than for the verdict,
// which livefleet.go's header states rather than leaving to be discovered by
// whoever mutates it next.
func processTable(ctx context.Context) (map[int]process, error) {
	out, err := exec.CommandContext(ctx, "ps", "-Aww", "-o", "pid=,state=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps -A: %w", err)
	}
	if blank(out) {
		return nil, fmt.Errorf("ps -A exited 0 and listed no process at all, which cannot be true of the machine this daemon is running on")
	}

	table := make(map[int]process, 256)
	for _, line := range strings.Split(string(out), "\n") {
		pid, p, ok := parseProcess(line)
		if !ok {
			continue
		}
		table[pid] = p
	}
	if _, listed := table[os.Getpid()]; !listed {
		return nil, fmt.Errorf("ps -A listed %d processes and not this daemon (pid %d), so it is not a listing of this machine", len(table), os.Getpid())
	}
	return table, nil
}

// parseProcess reads one `pid state command` row. A row it cannot read is
// skipped rather than failing the listing: ps writes a header on some
// implementations if the = suffixes are ignored, and one unreadable row must
// not turn every agent gone - the missing-daemon check above is what catches a
// listing that is unreadable in bulk.
func parseProcess(line string) (int, process, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 0, process{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return 0, process{}, false
	}
	// The command is everything after the pid and the state code, taken from
	// the original line rather than from the fields, so an argument containing
	// runs of spaces survives the match.
	_, rest, _ := strings.Cut(strings.TrimSpace(line), fields[0])
	_, argv, _ := strings.Cut(strings.TrimSpace(rest), fields[1])
	return pid, process{state: fields[1], argv: strings.TrimSpace(argv)}, true
}
