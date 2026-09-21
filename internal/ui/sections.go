package ui

// The fleet grouped into sections for the roster and board: a header-less top
// block of the manager and every un-tagged session, then each team in the
// daemon's creation order, every group attention-ranked within.
//
// The grouping is a stable partition of an already-ranked slice, so attention.go
// stays pure and untouched and within-section order is exactly today's order
// restricted to that team - no second ranking. The section order is the daemon's
// (Fleet.teamOrder, off the report), because only the daemon sees the whole fleet;
// a client's own order would differ (see the teams spec).
//
// When no team exists the result is one header-less section holding every agent,
// which is the flat roster this build has always drawn - so a fleet with no teams
// renders byte-for-byte as before, and the sections are visible only once a /team
// is typed.

// Section is one drawn group: a team's members under its header, or the top block
// when Team is "".
type Section struct {
	Team   string
	Agents []Agent
}

// sections partitions a ranked slice into the top block then the team sections.
//
// The caller hands in the slice it draws - the roster its Agents(), the board its
// OnRoster() - so the grouping never re-ranks, matching the roster's refusal to
// hold a second opinion on order. An agent whose team is not in the daemon's order
// (an ended row lingering after its team emptied) draws in the top block rather
// than being dropped for want of a section: the sections must cover every agent
// the caller passed, or a click lands on the wrong row.
func (f Fleet) sections(ranked []Agent) []Section {
	ordered := make(map[string]bool, len(f.teamOrder))
	for _, t := range f.teamOrder {
		ordered[t] = true
	}
	var top []Agent
	byTeam := make(map[string][]Agent)
	for _, a := range ranked {
		if a.Team == "" || !ordered[a.Team] {
			top = append(top, a)
			continue
		}
		byTeam[a.Team] = append(byTeam[a.Team], a)
	}
	out := []Section{{Agents: top}}
	for _, team := range f.teamOrder {
		if members := byTeam[team]; len(members) > 0 {
			out = append(out, Section{Team: team, Agents: members})
		}
	}
	return out
}
