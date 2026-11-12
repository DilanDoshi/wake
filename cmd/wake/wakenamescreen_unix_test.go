//go:build unix

// /wake-name on a real screen, against a named fleet under a scratch HOME: the
// room says the new name, the machine lists the fleet by it, and ⌃Q's line
// sends the operator back to it by that name.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
)

func TestWakeNameRenamesTheFleetOnARealScreen(t *testing.T) {
	_, old := renamedAtlas(t)

	if names, err := daemon.Fleets(); err != nil || !slices.Equal(names, []string{"dev"}) {
		t.Errorf("`wake fleets` lists %v (%v), want only dev", names, err)
	}
	st, err := daemon.Status(old)
	if err != nil || st.Fleet != "dev" {
		t.Errorf("the old socket reached a daemon calling itself %q (%v), want dev", st.Fleet, err)
	}
}

// ⌃Q after a /wake-name prints the way back by the new name: the old one is gone
// once the daemon exits, and a bare `wake` would start another fleet.
func TestCtrlQAfterAWakeNameNamesTheFleetItReopens(t *testing.T) {
	s, _ := renamedAtlas(t)
	s.send("\x11") // ⌃Q (arm)
	s.await("park all & quit")
	s.send("\x11") // ⌃Q (confirm)
	s.await(leavingDev("1 agent"))
}

// BUG-66, Dilan's flow: a bare `wake`'s own fleet, renamed, then an agent spawned
// and mid-turn at ⌃Q. A guard: main already prints the new name here.
func TestCtrlQAfterAWakeNameAndASpawnNamesTheFleet(t *testing.T) {
	withScriptedAgent(t, scriptInterruptible)
	scratchHome(t)
	t.Setenv("WAKE_SOCKET", "")

	s := startWake(t, 200, 40)
	s.await("group chat")
	s.send("/wake-name dev\r")
	s.await("is now dev")
	s.send("/new otto\r")
	s.await("> @otto")
	s.settle()
	if names := s.rosterNames(); !slices.Contains(names, "otto") {
		t.Fatalf("/new otto spawned no otto; the roster lists %v.\n%s", names, s.dump())
	}
	// /new left "@otto " in the draft. The manager is mid-turn too, on the fleet's
	// /rename, and echoes any line it is sent: the turn is otto's under otto's name.
	s.send("migrate the sessions table\r")
	turn := workingMarker + ": migrate the sessions table"
	s.await(turn)
	lines := s.lines()
	if row := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, turn) }); row < 1 ||
		!strings.HasPrefix(strings.TrimSpace(lines[row-1]), "otto ") {
		t.Fatalf("the turn on screen is not otto's.\n%s", s.dump())
	}
	s.send("\x11") // ⌃Q (arm)
	s.await("park all & quit")
	s.send("\x11") // ⌃Q (confirm)
	s.await(leavingDev("2 agents"))
}

// BUG-70: under HOME=h a bare `wake`'s fleet is named, so /wake-name renames it
// and ⌃Q's line sends the operator back to it.
func TestARelativeHomeKeepsTheFleetsName(t *testing.T) {
	withScriptedAgent(t, "")
	dir := scratchHome(t)
	t.Setenv("HOME", "h")
	t.Setenv("WAKE_SOCKET", "")
	// As a shell sets it: under /private/var the fleet's socket is too long to bind.
	t.Setenv("PWD", dir)

	s := startWakeIn(t, dir, 200, 40)
	s.await("group chat")
	s.send("/wake-name dev\r")
	s.await("is now dev")
	if info, err := os.Stat(filepath.Join(dir, "h", ".wake", "fleets", "dev")); err != nil || !info.IsDir() {
		t.Fatalf("the fleet is not under the directory's own h: %v", err)
	}
	s.send("\x11") // ⌃Q (arm)
	s.await("park all & quit")
	s.send("\x11") // ⌃Q (confirm)
	s.await(leavingDev("1 agent"))
}

// leavingDev is ⌃Q's exit line on fleet dev, up to its way back: words the
// keypress notice ("parking … and closing Wake") never draws.
func leavingDev(agents string) string {
	return "Parking " + agents + " (a turn in flight is interrupted). `wake --fleet dev` reopens the room"
}

// ⌃C⌃C names the fleet the window was last told, even once another hand has ended
// the daemon and, with it, the link from the old name the window opened under.
func TestCtrlCCtrlCAfterAWakeNameNamesTheFleet(t *testing.T) {
	s, _ := renamedAtlas(t)
	if out, err := exec.Command(wakeBinary(t), "stop").CombinedOutput(); err != nil {
		t.Fatalf("`wake stop` from another terminal: %v\n%s", err, out)
	}
	s.await("⌃O, then `wake --fleet dev`")
	s.send("\x03\x03")
	s.await("`wake --fleet dev` reopens the room")
}

// renamedAtlas is a room on a fleet called atlas, under a scratch HOME, renamed
// dev, and the socket it was opened on.
func renamedAtlas(t *testing.T) (*screen, string) {
	t.Helper()
	withScriptedAgent(t, "")
	home := scratchHome(t)
	// A scratch socket where a fleet called atlas keeps its own, so the daemon
	// reads it as that fleet; an agent's WAKE_SOCKET still names it afterwards.
	old := filepath.Join(home, ".wake", "fleets", "atlas", "daemon.sock")
	t.Setenv("WAKE_SOCKET", old)

	s := startWake(t, 200, 40)
	s.await("group chat")
	s.send("/wake-name dev\r")
	s.await("fleet atlas is now dev")
	return s, old
}

// scratchHome is a HOME of the test's own, short enough for a fleet's socket.
func scratchHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("", "wakeh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	return home
}
