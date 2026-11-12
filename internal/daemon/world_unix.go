//go:build unix

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
)

// processRows lists every process with its parent, group and start time, in one
// ps. A listing without this daemon in it is not a listing of this machine, and
// is refused as processTable refuses it: a partial answer must never read as
// "nothing is running".
func processRows(ctx context.Context) ([]procRow, error) {
	out, err := exec.CommandContext(ctx, "ps", "-Aww", "-o", "pid=,ppid=,pgid=,lstart=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps -A: %w", err)
	}
	rows := parseProcRows(out)
	if !slices.ContainsFunc(rows, func(r procRow) bool { return r.pid == os.Getpid() }) {
		return nil, fmt.Errorf("ps -A listed %d processes and not this daemon (pid %d), so it is not a listing of this machine", len(rows), os.Getpid())
	}
	return rows, nil
}
