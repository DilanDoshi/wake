package ui

import "slices"

// agentSet is one session's subagent types, behind a pointer so Agent stays
// comparable (commandSet's reason). Immutable: withAgents replaces it.
type agentSet struct{ names []string }

// SubagentTypes is what the session's last init advertised, nil before its
// first turn. A copy, so no caller can rewrite what every Agent copy shares.
func (a Agent) SubagentTypes() []string {
	if a.subagentTypes == nil {
		return nil
	}
	return slices.Clone(a.subagentTypes.names)
}

// withAgents replaces the set when a frame or a report names a different one,
// and keeps the pointer when unchanged so a repeated init costs no copy. Folded
// from the init event and from the report: the report is the only route for a
// late-attached client. See rpc.SessionStatus.Agents.
func (a Agent) withAgents(names []string) Agent {
	if len(names) > 0 && (a.subagentTypes == nil || !slices.Equal(a.subagentTypes.names, names)) {
		a.subagentTypes = &agentSet{names: slices.Clone(names)}
	}
	return a
}
