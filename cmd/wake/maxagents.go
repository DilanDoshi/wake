package main

// `--max-agents N`: how many sessions a fleet runs at once, the manager
// included - fifty unless set, and never set above fifty (owner's 2026-10-20
// ruling). It is set on the fleet's running daemon, so it works on a fleet
// already open and lasts until that fleet stops.

import (
	"fmt"
	"net"
	"strconv"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// maxAgentsFlagName is the flag, spelled once.
const maxAgentsFlagName = "--max-agents"

// maxAgentsSet is what the room says once the cap is sent.
const maxAgentsSet = "this fleet now runs up to %d %s at once"

// fleetMaxAgents is this invocation's --max-agents, or 0. Set by run before any
// verb connects and spent by the first connect, so a reconnect cannot put back
// a cap another window has since changed.
var fleetMaxAgents int

// maxAgentsFlag takes `--max-agents N` off the arguments, fleetFlag's way: legal
// anywhere, once, N from 1 to rpc.MaxAgentsCeiling. Zero is "not given".
func maxAgentsFlag(args []string) (rest []string, n int, err error) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] != maxAgentsFlagName {
			rest = append(rest, args[i])
			continue
		}
		if n != 0 {
			return nil, 0, fmt.Errorf("%s was given twice: a fleet has one cap", maxAgentsFlagName)
		}
		if i+1 >= len(args) {
			return nil, 0, fmt.Errorf("%s needs a number: how many sessions the fleet runs at once, 1 to %d",
				maxAgentsFlagName, rpc.MaxAgentsCeiling)
		}
		i++
		v, err := strconv.Atoi(args[i])
		if err != nil || v < 1 || v > rpc.MaxAgentsCeiling {
			return nil, 0, fmt.Errorf("%s takes a number from 1 to %d, not %q", maxAgentsFlagName, rpc.MaxAgentsCeiling, args[i])
		}
		n = v
	}
	return rest, n, nil
}

// maxAgentsGoesWith refuses the flag on a verb that opens no fleet: it sets a
// fleet's cap, so it rides a command that opens the room or a conversation.
func maxAgentsGoesWith(args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case cmdNew, cmdAttach, cmdManager:
		return nil
	}
	return fmt.Errorf("%s sets how many sessions a fleet runs at once, so it goes with wake, %s, %s or %s - not %s",
		maxAgentsFlagName, cmdNew, cmdAttach, cmdManager, args[0])
}

// sendMaxAgents gives this connection's daemon the operator's cap, when there is
// one. On the verb's own connection: a daemon with no agents exits when its last
// client leaves, so a cap sent on a connection of its own could be lost to a
// fresh daemon before the verb arrived.
func sendMaxAgents(conn net.Conn) error {
	n := fleetMaxAgents
	if n == 0 {
		return nil
	}
	if err := rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameSetMaxAgents, MaxAgents: n}); err != nil {
		return fmt.Errorf("setting the fleet's cap: %w", err)
	}
	fleetMaxAgents = 0
	noun := "sessions"
	if n == 1 {
		noun = "session"
	}
	notice.Report(maxAgentsSet, n, noun)
	return nil
}
