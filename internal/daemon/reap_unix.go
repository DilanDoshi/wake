//go:build unix

// Asking the operating system about a process the daemon did not spawn, or
// spawned and can no longer hear from.
//
// Both answers below are about a pid nothing in this program holds a usable
// handle to. Go has no portable way to read another process's state or argv -
// Linux has /proc, darwin needs a sysctl through unsafe - so this shells out
// to ps(1), which both have and both spell the same way. A machine without ps
// loses the reaper and the liveness probe, not the daemon: every lookup fails
// as *unknown*, and unknown never kills anything and never declares anything
// dead.

package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// errNoProcess is ps saying there is no such pid, which is different from ps
// being unable to answer. The distinction is the whole reason this returns an
// error rather than a bool: "gone" is a fact the liveness probe acts on, and
// "I could not check" must never be mistaken for it.
var errNoProcess = errors.New("no such process")

// groupLeader reports whether pid leads its own process group.
//
// The reaper signals -pid, which reaches a *group*. If this pid is not that
// group's leader then -pid names some other group entirely - possibly the
// daemon's own - so a false here is the difference between reaping an agent
// and killing a bystander.
//
// Note that this is false for a zombie: Getpgid fails on one. That is right
// for the reaper, whose caller has no daemon and therefore no unreaped
// children, and it is why the liveness probe below does not use it.
func groupLeader(pid int) bool {
	pgid, err := syscall.Getpgid(pid)
	return err == nil && pgid == pid
}

// process is what ps says about one pid: its state code and its full argv.
type process struct {
	state string
	argv  string
}

// inspect asks ps about a pid.
//
// -ww is load-bearing on both platforms: without it ps truncates the command
// to the terminal width, and the --session-id everything here turns on sits
// near the end of a long claude command line. A truncated answer would read
// as "this is not the agent" - the safe direction, but for the wrong reason
// and every single time.
//
// A non-zero exit is not enough to mean "there is no such process", and
// treating it that way is dangerous in exactly one direction. A ps that runs
// but rejects these flags - busybox in a container, any implementation that is
// neither procps nor BSD - exits non-zero for *every* pid, so folding that into
// errNoProcess declares an entire living fleet gone and sends every quiet
// agent through OS-confirmed reclaim. That is the file header's contract
// inverted.
//
// The discrimination is that both stock implementations report a missing pid
// by saying nothing at all - exit non-zero, empty stdout, empty stderr - while
// every ps that could not answer the question has something to say about why.
// So silence on both streams is the only shape that counts as an answer.
//
// Silence is necessary and not sufficient, because there is one ps that dies
// with nothing to say and is not an answer at all: **the one this package
// killed itself.** exec.CommandContext SIGKILLs the child when the context
// expires, and Cmd.Wait prefers the *exec.ExitError from Process.Wait over
// ctx.Err() ("If c.Process.Wait returned an error, prefer that"), so a probe
// that outlived probeTimeout comes back as `signal: killed` with both streams
// empty - indistinguishable from a missing pid by every other test here.
// Measured, not inferred: errors.Is(err, context.DeadlineExceeded) is false
// there too, so the context is no help either.
//
// Exited() is the discriminator: it is false for a signal-terminated process,
// so a killed ps falls through to unknown. Getting it wrong does not cost a
// slow probe. noteUnreachable sets a flag nothing clears and suspects() then
// stops asking about that agent at all, so a healthy live agent whose probe
// was slow *once* - a loaded machine, a laptop that suspended mid-probe - is
// permanently silent and SIGKILLed by group at shutdown instead of stopped
// gently.
//
// It errs unknown, which is the harmless direction: a ps that declines a pid
// it considers out of range (darwin does, above PID_MAX) reads as "cannot
// check" rather than "gone", and a process group this daemon recorded is never
// out of range anyway.
func inspect(ctx context.Context, pid int) (process, error) {
	out, err := exec.CommandContext(ctx, "ps", "-ww", "-o", "state=,command=", "-p", fmt.Sprint(pid)).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.Exited() && blank(out) && blank(exit.Stderr) {
			return process{}, errNoProcess
		}
		return process{}, fmt.Errorf("ps -p %d: %w", pid, err)
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		// Exit 0 and nothing on stdout. Neither stock implementation does
		// this - both exit non-zero for a pid that is not there - so it is an
		// implementation nothing here has characterised, and the same rule
		// applies as everywhere else in this file: an answer nobody can
		// account for is not an answer.
		return process{}, fmt.Errorf("ps -p %d: exited 0 without reporting the process or saying why", pid)
	}
	state, argv, _ := strings.Cut(line, " ")
	return process{state: state, argv: strings.TrimSpace(argv)}, nil
}

// zombie reports whether a process has exited without being reaped. It has
// released every descriptor it held, so for anything this package asks it is
// gone - and a zombie is exactly what an agent becomes when core's pump is
// parked in Scan and never reaches Wait, which is the state the liveness
// probe exists to catch.
func (p process) zombie() bool { return strings.HasPrefix(p.state, "Z") }

// blank reports whether a stream carried nothing but whitespace, which is how
// ps says "that pid is not here" and the only way it says it silently.
func blank(b []byte) bool { return len(bytes.TrimSpace(b)) == 0 }
