package core

import (
	"errors"
	"os"
)

// agentLauncherStatusFD and its ERROR frame are frozen across protocols: a
// binary replaced under a running daemon reports the mismatch through them.
const (
	agentLauncherArg        = "--wake-agent-launcher"
	agentLauncherMarkerEnv  = "WAKE_AGENT_LAUNCHER"
	agentLauncherDirEnv     = "WAKE_AGENT_LAUNCHER_DIR"
	agentLauncherProtocol   = "1"
	agentLauncherControlFD  = 3
	agentLauncherStatusFD   = 4
	agentLauncherLifetimeFD = 5
	agentLauncherRelease    = byte('R')
	agentLauncherReady      = byte('R')
	agentLauncherError      = byte('E')
	agentLauncherDone       = byte('D')
)

// AgentLauncher is an opaque capability to re-exec the current Wake binary.
// Its path is deliberately not exported: callers may choose whether a session
// gets the capability, but they cannot redirect the fixed claude target.
type AgentLauncher struct {
	executable string
}

// Active reports whether this launcher can supervise a session. A caller uses
// it to take the ownership-callback path only when there is a supervisor to own
// the process group; the zero AgentLauncher is inactive, which is the direct
// path. StartObserved refuses an ownership callback on an inactive launcher, so
// this is how a caller asks before it hands one over.
func (l AgentLauncher) Active() bool {
	return l.executable != ""
}

type agentLauncherPipes struct {
	control  *os.File
	status   *os.File
	lifetime *os.File
}

var agentLauncherEnv = []string{
	agentLauncherMarkerEnv,
	agentLauncherDirEnv,
}

// errAgentLauncherReplaced is a launch from a daemon older or newer than this
// binary: the file was replaced while that daemon kept running.
var errAgentLauncherReplaced = errors.New("this wake binary was replaced while its fleet's daemon kept running, " +
	"and the two start agents differently: ⌃Q⌃Q the fleet, then reopen it, to run it on this build")

// AgentLauncherMismatch is the error for a launch this binary cannot serve, or
// nil when it is not one.
// It is also written to the status fd as an ERROR frame, which is what the
// daemon that started this process reads as the spawn's error.
func AgentLauncherMismatch() error {
	err := agentLauncherMismatch(os.Args, os.LookupEnv)
	if err != nil {
		if status := os.NewFile(agentLauncherStatusFD, "Wake agent launcher status"); status != nil {
			_ = reportAgentLauncherFailure(status, err)
		}
	}
	return err
}

func agentLauncherMismatch(args []string, lookup func(string) (string, bool)) error {
	marker, set := lookup(agentLauncherMarkerEnv)
	if set && marker != agentLauncherProtocol && len(args) >= 2 && args[1] == agentLauncherArg {
		return errAgentLauncherReplaced
	}
	return nil
}
