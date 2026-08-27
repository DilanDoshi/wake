package core

import "os"

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
