package ui

// `/wake-name`: what this fleet is called, and renaming it. The daemon moves the
// fleet (daemon/fleetrename.go) and its report names the new one, so every window
// on the fleet says so when the name changes - the asking one included, and one
// that was away when it happened says so on its next report.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// fleetIsCalled is a bare /wake-name: the name, and how to change it.
	fleetIsCalled = "this fleet is %s · " + wakeNameUsage

	// wakeNameUsage is a bare /wake-name on a report naming no fleet - a socket
	// set by hand outside ~/.wake - and the second half of fleetIsCalled.
	wakeNameUsage = wakeNameVerb + " <name> renames it"

	// fleetRenamed is said in every window, with the way back to the fleet.
	fleetRenamed = "fleet %s is now %s · `%s` comes back to it"

	// fleetRenameFailed is a write that never reached the daemon.
	fleetRenameFailed = "asking to rename the fleet"
)

// FleetName is what the last report called this fleet, for the line cmd/wake
// prints once the program has ended.
func (a App) FleetName() string { return a.daemon.fleet }

// WithFleetNamed hands f the fleet's name now and each name a report changes it
// to: cmd/wake's emergency exit runs when the loop that folds them has stopped.
// f runs on the Update goroutine, so it must not block.
func (a App) WithFleetNamed(f func(string)) App {
	a.fleetNamed = f
	if f != nil {
		f(a.daemon.fleet)
	}
	return a
}

// renameFleet is /wake-name. The daemon refuses what it cannot rename, and the
// report that follows a rename is what announces it.
func (a App) renameFleet(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	switch {
	case arg != "":
		return a, a.write(fleetRenameFailed, rpc.Frame{Kind: rpc.FrameRenameFleet, Text: rpc.HyphenateName(arg)})
	case a.daemon.fleet != "":
		notice.Report(fleetIsCalled, a.daemon.fleet)
	default:
		notice.Report("%s", wakeNameUsage)
	}
	return a, nil
}

// withFleetName takes a report's fleet name, saying so when it names another
// fleet than the last one did.
func (a App) withFleetName(st *rpc.Status) App {
	if st == nil {
		return a
	}
	if was := a.daemon.fleet; was != "" && st.Fleet != "" && st.Fleet != was {
		notice.Report(fleetRenamed, was, st.Fleet, rpc.FleetCommand(st.Fleet))
	}
	if st.Fleet != a.daemon.fleet && a.fleetNamed != nil {
		a.fleetNamed(st.Fleet)
	}
	a.daemon.fleet, a.daemon.title = st.Fleet, tabTitle(st.Fleet)
	return a
}
