package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The manager wears yellow by default - on its room turns, its status bar and
// its roster row - so the fleet's coordinator stands apart from the shared
// Accent the ordinary agents default to, without anyone running /color.
func TestManagerDefaultsToYellowAcrossIdentitySurfaces(t *testing.T) {
	mgr := Agent{Name: core.ManagerName, State: rpc.StateWorking}
	want := identityColors["yellow"]
	if got := speakerStyle(mgr).GetForeground(); got != want {
		t.Errorf("manager room speaker = %v, want yellow %v", got, want)
	}
	if got := barStyle(mgr).GetForeground(); got != want {
		t.Errorf("manager status bar = %v, want yellow %v", got, want)
	}
	if got := (Roster{}).headStyle(mgr).GetForeground(); got != want {
		t.Errorf("manager roster head = %v, want yellow %v", got, want)
	}
}

// An explicit /color still wins over the manager's yellow default.
func TestAnExplicitColorOverridesTheManagerDefault(t *testing.T) {
	mgr := Agent{Name: core.ManagerName, Color: "blue", State: rpc.StateWorking}
	if got := speakerStyle(mgr).GetForeground(); got != identityColors["blue"] {
		t.Errorf("manager /color blue = %v, want blue", got)
	}
}

// An ordinary agent with no colour keeps the shared Accent, not the manager's
// yellow - the default is the manager's alone.
func TestAnOrdinaryAgentDoesNotGetTheManagerYellow(t *testing.T) {
	if got := speakerStyle(Agent{Name: "alex"}).GetForeground(); got != Accent {
		t.Errorf("ordinary agent speaker = %v, want Accent %v", got, Accent)
	}
}

// /color none clears an agent to an empty Color, but the manager's cleared
// state is its default: an empty Color on the manager is yellow, not grey, so
// the manager cannot be made hueless. Pins the deliberate ruling.
func TestColorNoneLeavesTheManagerYellow(t *testing.T) {
	cleared := Agent{Name: core.ManagerName, Color: ""}
	if got := speakerStyle(cleared).GetForeground(); got != identityColors["yellow"] {
		t.Errorf("a cleared manager = %v, want the yellow default (it cannot be made hueless)", got)
	}
}
