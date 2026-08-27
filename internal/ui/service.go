package ui

// The manager as a switch: what turns it on, what turns it off, and the one
// place that reads which of those a fleet needs.
//
// # Why this is on by default and a command second
//
// Spec §12 says the manager "holds a permanent seat in every group", and the
// build never had one: the only way to get a manager was `wake manager` at a
// shell, so the room's own composer refused every unaddressed draft until
// somebody left the room to fix it. That is a front door that does not open.
// ManagerFrames is what cmd/wake writes before the room is drawn, and
// `/manager` is the same decision under a keystroke.
//
// # Why two switches over the same three states
//
// ManagerFrames is pure - a report in, at most one frame out - because it has a
// caller with no model to speak of: cmd/wake writes it on the connection before
// tea.NewProgram exists, which is requestFleet's own slot and its own argument.
// App.manager cannot be that function, because it has a fourth arm the pure one
// must not have (**a running manager is parked**, which is what makes it a
// toggle) and because each arm owes the operator a different sentence. What they
// share is everything that could drift: Fleet.manager decides which row is the
// manager, spawnManagerFrame is the one spelling of starting one, and the parked
// arm goes through /resume's own bringBack rather than a second wake.

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// managerFailed names the write that could not happen, so the notice row
	// says which command was typed rather than only what the socket said.
	managerFailed = "starting the manager"

	// managerStarting is said on the keypress, because the daemon may refuse -
	// another window may have started one in the round trip - and the operator
	// should know the command was read either way. The confirmation is
	// startArrived's, off the report that names the session.
	managerStarting = "starting the manager…"

	// managerTakesNoArgument is `/manager <anything>`, refused rather than
	// ignored.
	//
	// /mcp's rule one command over: somebody typing `/manager off` means
	// something specific, and a toggle that fired anyway would do the right
	// thing half the time and the exact opposite the rest - having looked like
	// it read the word. There is nothing to configure here, so saying what the
	// command is is the honest answer.
	managerTakesNoArgument = managerVerb + " turns the manager on when it is off and parks it when it is on. It takes no argument"

	// managerStopFailed and managerStopping are managerFailed's and
	// managerStarting's pair, for their reasons: the notice names the command
	// that was typed, and it is said on the keypress because the daemon may
	// refuse and the operator is owed an answer either way.
	managerStopFailed = "stopping the manager"
	managerStopping   = "stopping the manager…"

	// managerStopTakesNoArgument is managerTakesNoArgument one command over.
	// There is nothing to configure about an ending.
	managerStopTakesNoArgument = managerStopVerb + " ends the manager and releases its name. It takes no argument"

	// managerNotRunning is a stop with nothing to stop. It names the command
	// that starts one, because an operator who typed this wants a manager
	// *state* and should not have to guess which verb reaches the other one.
	//
	// An **ended** manager arrives here too: Fleet.manager reads ended as
	// absent, since the name went back to the pool and there is nothing left to
	// address.
	managerNotRunning = "there is no manager to stop. " + managerVerb + " starts one"

	// managerStopParked refuses a parked manager, and the reason is mechanism
	// rather than policy: the daemon refuses a stop at a session with no
	// process, in **two different shapes**. A manager parked by ⌃C still has a
	// row, so withAgent finds it and agent.submit refuses on its closed `gone`
	// channel - "session <id> has ended". One parked across a restart has no row
	// at all, because a daemon restores nothing, so withAgent refuses instead -
	// "unknown session <id>". Writing the frame would put one of two unrelated
	// daemon sentences in front of the operator depending on when they quit, for
	// a command that could have explained itself once.
	// internal/daemon's TestAStopAtAParkedSessionIsRefusedInBothOfItsShapes
	// pins both, so this refusal cannot outlive the behaviour it assumes.
	//
	// It says *when* it would work, which is forkRefusal's rule: a refusal that
	// is only "no" leaves an operator with a command that does nothing and no
	// idea when it would.
	managerStopParked = "the manager is parked, and a stop only reaches a session with a process. " +
		managerVerb + " wakes it, then " + managerStopVerb + " ends it"
)

// ManagerFrames is what leaves a fleet with a manager running: nothing when one
// already is, a wake when it is parked, a spawn when there is none.
//
// Exported for cmd/wake, which writes it on the connection before the model
// exists - the same slot requestFleet uses, and for the same reason: this is a
// frame internal/ui may not write from a draw goroutine, and there is no
// confirmation here for a later reply to be confused with.
//
// The caller mints newID rather than this function, which keeps it pure and
// table-testable. Wake originates identity in any case: maySpawn refuses a
// spawn under anything that is not a UUID this client minted, because the
// reaper's only proof of a process group is that id in an argv.
//
// A nil report starts nothing. Nil is legitimate - NewRoomApp documents it as
// "wait for the first push" - and the safe reading is that nothing is *known*,
// not that nothing is running; a spawn on no evidence is a second manager every
// time a caller had no seed.
func ManagerFrames(st *rpc.Status, dir, newID string) []rpc.Frame {
	if st == nil {
		return nil
	}
	mgr, found := NewFleet().WithStatus(st).manager()
	switch {
	case found && mgr.State == rpc.StateParked:
		return wakeFrames([]Agent{mgr})
	case found:
		return nil
	default:
		return []rpc.Frame{spawnManagerFrame(newID, dir)}
	}
}

// spawnManagerFrame is the one spelling of "start the manager", so the command
// and the default cannot come apart on what a manager spawn actually is.
//
// **Role is the whole of it.** Without that field the daemon draws an ordinary
// name from the pool and the session comes up as an agent: no MCP config, no
// scope, no tools. Text is empty deliberately - the daemon owns the one manager
// name and refuses it to every other spawn, so a client asking for a name is
// asking for something that would not be the manager.
func spawnManagerFrame(newID, dir string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameSpawn, SessionID: newID, Role: rpc.RoleManager, Dir: dir}
}

// manager is the session the daemon named the manager, live or parked.
//
// **An ended manager is not it**, and that is the whole of the state filter: a
// name is released when a session ends and reissued, so there is nothing to wake
// and a fresh spawn gets the name back. A parked one is the exact opposite - the
// name is still claimed, so a spawn is *refused* and a wake is the only thing
// that works. Getting those two the wrong way round is a refusal in the notice
// row for something nobody asked for.
//
// Both sources, for parkedAgents' reason rather than by analogy with it: a
// manager parked with ⌃C is still a fleet row holding its name, and one left in
// the park book by ⌃Q is not in the fleet at all - a daemon restores nothing, so
// the book is the only path across a restart. They cannot overlap; the daemon
// takes a record out of the book as it launches.
//
// Keyed on the name, which is safe for the reason service() is: daemon/names.go
// refuses core.ManagerName to every ordinary spawn, and names_test.go holds the
// daemon's reserved set equal to this package's routing constants.
func (f Fleet) manager() (Agent, bool) {
	for _, a := range f.Agents() {
		if a.Name == core.ManagerName && a.State != rpc.StateEnded {
			return a, true
		}
	}
	for _, a := range f.parked {
		if a.Name == core.ManagerName {
			return a, true
		}
	}
	return Agent{}, false
}

// manager is `/manager`: the switch, from inside the room.
//
// Three arms over the state the fleet reports, and the middle one is what makes
// this a switch rather than a start. It is addressed to Wake rather than to a
// session, so it works from the room and from inside any conversation - which is
// slash.go's first kind of command, and the reason this is in the map it is in.
func (a App) manager(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	if arg != "" {
		notice.Report("%s", managerTakesNoArgument)
		return a, nil
	}
	mgr, found := a.fleet.manager()
	switch {
	case found && mgr.State == rpc.StateParked:
		// /resume's own tail, not a second wake: same frame, same wait, same
		// sentence about what a returning session does and does not carry.
		return a.bringBack([]Agent{mgr})
	case found:
		return a.parkManager(mgr)
	default:
		return a.startManager()
	}
}

// startManager asks the daemon for a manager and says it asked.
//
// The directory is `wake`'s own working directory, which is what `wake manager`
// at a shell sends and what `/new` defaults to - so the three verbs agree about
// where a session with no directory named for it runs. It is resolved here
// because the daemon would resolve a relative path against *its* directory, one
// process for the whole machine started from whichever repository forked it.
func (a App) startManager() (App, tea.Cmd) {
	dir, err := absoluteDir("")
	if err != nil {
		notice.Report("%s", err.Error())
		return a, nil
	}
	id := uuid.NewString()
	notice.Report("%s", managerStarting)
	return a.awaitingStart(id), a.write(managerFailed, spawnManagerFrame(id, dir))
}

// parkManager turns the service off, and refuses the one state park must not
// touch.
//
// It goes through parkTarget, which is ⌃C's own body once something has decided
// which session: a second copy of "parking closes stdin, and an outstanding ask
// that dies that way is a denial nobody made" would be two places to correct the
// day that rule changes, on the one surface where being wrong costs somebody a
// repository.
func (a App) parkManager(mgr Agent) (App, tea.Cmd) {
	next, cmd, parked := a.parkTarget(mgr.ID, mgr.Name)
	if !parked {
		return a, nil
	}
	notice.Report(parkedFormat, agentPrefix, mgr.Name)
	return next, cmd
}

// managerStop is `/manager-stop`: the ending, where `/manager` only parks.
//
// Two arms refuse and one writes, over the same Fleet.manager the switch reads -
// so the two commands cannot disagree about which row is the manager.
//
// **It does not go through parkTarget, and the difference is the blocked
// state.** parkTarget refuses a blocked agent because closing stdin under an
// outstanding ask is recorded as a denial the operator never made, and the whole
// weight of that argument is that the false "no" *survives the wake*. A stop has
// no wake: the session ends, the name is released, and nothing is ever built on
// the belief. Borrowing the refusal here would leave an operator unable to end
// the one session they are trying to be rid of.
//
// The target is the manager and never App.route's or the roster's, which matters
// more here than anywhere else in this file: stop is the one verb in this
// project nothing brings back, so it may not be aimed by a cursor.
func (a App) managerStop(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	if arg != "" {
		notice.Report("%s", managerStopTakesNoArgument)
		return a, nil
	}
	mgr, found := a.fleet.manager()
	switch {
	case !found:
		notice.Report("%s", managerNotRunning)
		return a, nil
	case mgr.State == rpc.StateParked:
		notice.Report("%s", managerStopParked)
		return a, nil
	default:
		notice.Report("%s", managerStopping)
		return a, a.write(managerStopFailed, rpc.Frame{Kind: rpc.FrameStop, SessionID: mgr.ID})
	}
}
