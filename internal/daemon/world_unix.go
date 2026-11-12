//go:build unix

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
)

// processRows lists every process with its parent, group, start time and command,
// in one ps, in UTC and the C locale so a start time reads the same at the ending
// as when it was noted. A listing that is not this machine's is refused as
// processTable refuses one.
func processRows(ctx context.Context) ([]procRow, error) {
	cmd := exec.CommandContext(ctx, "ps", "-Aww", "-o", "pid=,ppid=,pgid=,lstart=,command=")
	cmd.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ps -A: %w", err)
	}
	rows := parseProcRows(out)
	if !slices.ContainsFunc(rows, func(r procRow) bool { return r.pid == os.Getpid() }) {
		return nil, notThisMachinesListing(len(rows))
	}
	return rows, nil
}
