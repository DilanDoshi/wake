// How the daemon resolves the Unix supervisor a launch runs under. Split out of
// spawn.go, which owns starting a session: the supervisor a launch runs under is
// supporting detail, and spawn.go was two lines past the 800-line hard max once
// it carried both the supervisor resolution and the debug-file claim that main's
// launch already had.

package daemon

import (
	"os"

	"github.com/DilanDoshi/wake/internal/core"
)

// newAgentLauncher resolves the Unix supervisor every agent this daemon starts
// runs under, so the daemon keeps a durable, off-disk handle to each agent's
// process group that outlives the daemon itself - which is what lets the crash
// reaper and the empty-daemon reclaim reach an agent's whole tree after this
// process is gone. It is a var so a test can put one agent on the direct path;
// production always uses the real one. On a platform without a supervisor it
// returns an empty launcher and the agent runs directly, unchanged.
// DirectAgentLauncherEnv, when "1" in the environment, makes an agent run on the
// direct path with no supervisor. Only tests set it - production never does, so a
// production daemon always runs the real supervisor - and it is read rather than
// a call so it reaches every daemon a test process fathers: the in-process one,
// the forked `wake daemon`, and the real binary a pty test runs, all through
// os.Environ() inheritance. Tests default to direct so the pty and process-table
// suites do not pay the supervisor re-exec cost of a race-instrumented test
// binary on every spawn; the supervised path has its own daemon tests
// (activation_unix_test.go) and make live. It is the lease variables' pattern:
// product processes never set it.
const DirectAgentLauncherEnv = "WAKE_TEST_DIRECT_LAUNCHER"

var newAgentLauncher = defaultAgentLauncher

func defaultAgentLauncher() (core.AgentLauncher, error) {
	if os.Getenv(DirectAgentLauncherEnv) == "1" {
		return core.AgentLauncher{}, nil
	}
	return core.SelfAgentLauncher()
}
