package main

// The lines that send the operator back to a fleet name it as it is now: a bare
// `wake` starts a new fleet and a bare verb reaches the unnamed one, so only a
// socket set by hand, which names no fleet, is reached bare.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// fleetLines is what each line says for a named fleet, the unnamed one and a
// socket set by hand.
var fleetLines = []struct{ fleet, reopen, status, stop string }{
	{"dev", "wake --fleet dev", "wake status --fleet dev", "wake stop --fleet dev"},
	{"default", "wake --fleet default", "wake status --fleet default", "wake stop --fleet default"},
	{"", "wake", "wake status", "wake stop"},
}

// fleetsHome is a scratch HOME holding a fleet called to, renamed from from when
// they differ, and returns the socket a window opened under from holds.
func fleetsHome(t *testing.T, from, to string) string {
	t.Helper()
	home, err := os.MkdirTemp("", "wakeh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	fleets := filepath.Join(home, ".wake", "fleets")
	if err := os.MkdirAll(filepath.Join(fleets, to), 0o700); err != nil {
		t.Fatal(err)
	}
	if from != to {
		if err := os.Symlink(to, filepath.Join(fleets, from)); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(fleets, from, "daemon.sock")
}

func TestCtrlQsLineNamesTheFleet(t *testing.T) {
	for _, tc := range fleetLines {
		confirmed := parkAllLine(2, tc.fleet, nil)
		if want := "`" + tc.reopen + "` reopens the room"; !strings.Contains(confirmed, want) {
			t.Errorf("on fleet %q a confirmed ⌃Q printed %q, want %q", tc.fleet, confirmed, want)
		}
		failed := parkAllLine(2, tc.fleet, errTest)
		for _, want := range []string{"`" + tc.status + "` says what is still running", "`" + tc.reopen + "` reopens the room"} {
			if !strings.Contains(failed, want) {
				t.Errorf("on fleet %q an unconfirmed ⌃Q printed %q, want %q", tc.fleet, failed, want)
			}
		}
	}
}

// ⌃O names the way back as well as the way to look and the way to end it, and
// the end names this fleet: a bare `wake stop` ends the unnamed one.
func TestTheDetachLineNamesTheWayBackToTheFleet(t *testing.T) {
	for _, tc := range fleetLines {
		running := detachLine(rpc.Status{Running: true, Fleet: tc.fleet, Sessions: []rpc.SessionStatus{{ID: "a", State: rpc.StateIdle}}}, nil)
		if want := tc.reopen + " · " + tc.status + " · " + tc.stop; !strings.HasSuffix(running, want) {
			t.Errorf("on fleet %q ⌃O printed %q, want it to end %q", tc.fleet, running, want)
		}
		orphans := detachLine(rpc.Status{Fleet: tc.fleet, Sessions: []rpc.SessionStatus{{ID: "a", State: rpc.StateOrphaned}}}, nil)
		if !strings.HasSuffix(orphans, "    "+tc.status) {
			t.Errorf("on fleet %q a crashed daemon's line is %q, want it to end %q", tc.fleet, orphans, tc.status)
		}
	}
}

func TestTheEmergencyLineNamesTheFleet(t *testing.T) {
	for _, tc := range fleetLines {
		want := "`" + tc.reopen + "` reopens the room, `" + tc.status + "` says what is there"
		if got := emergencyLine(tc.fleet); !strings.Contains(got, want) {
			t.Errorf("on fleet %q ⌃C⌃C printed %q, want %q", tc.fleet, got, want)
		}
	}
}

// ⌃Q's line names the fleet the last report named - after a /wake-name, the new
// name - not the socket's: the daemon drops a rename's old names as it exits.
func TestCtrlQsLineNamesTheFleetTheLastReportNamed(t *testing.T) {
	d := startFakeDaemon(t, 0, twoLiveAgents())
	seed := twoLiveAgents()
	seed.Fleet = "dev"
	got := leavingLine(ctrlQTwice(ui.NewRoomApp(nil, ui.Stream{}, &seed)), d.socket)
	if !strings.Contains(got, "`wake --fleet dev` reopens the room") {
		t.Errorf("after ⌃Q on fleet dev, `wake` printed %q", got)
	}
}

// A report that named no fleet - a daemon from before reports did - leaves the
// name to the socket, through the link a rename left.
func TestCtrlQsLineFallsBackToTheSocketsFleet(t *testing.T) {
	socket := fleetsHome(t, "atlas", "dev")
	got := leavingLine(ctrlQTwice(ui.NewRoomApp(nil, ui.Stream{}, statusOf(twoLiveAgents()))), socket)
	if !strings.Contains(got, "`wake --fleet dev` reopens the room") {
		t.Errorf("after ⌃Q on a socket under atlas, renamed dev, `wake` printed %q", got)
	}
}

func TestLeavingWithoutCtrlQNamesTheFleetTheDaemonReports(t *testing.T) {
	st := twoLiveAgents()
	st.Fleet = "dev"
	d := startFakeDaemon(t, 0, st)
	if got := leavingLine(ui.NewRoomApp(nil, ui.Stream{}, nil), d.socket); !strings.HasSuffix(got, "wake stop --fleet dev") {
		t.Errorf("⌃O on fleet dev printed %q", got)
	}
}

// ctrlQTwice arms and confirms ⌃Q, as the operator does.
func ctrlQTwice(a ui.App) tea.Model {
	armed, _ := a.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	m, _ := armed.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	return m
}

// ⌃C⌃C asks the App nothing - its loop is what stopped - so the name is read off
// the socket when it fires, through the link the running daemon keeps.
func TestTheEmergencyExitNamesTheFleetARenameMoved(t *testing.T) {
	socket := fleetsHome(t, "atlas", "dev")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		read <- string(b)
	}()
	k := newKillSwitch(os.Stdin, w, nil, nil, nil)
	k.socket = socket
	k.restoreTerminal()
	_ = w.Close()
	if got := <-read; !strings.Contains(got, emergencyLine("dev")) {
		t.Errorf("⌃C⌃C under atlas, renamed dev, wrote %q, want %q", got, emergencyLine("dev"))
	}
}

func TestStatusNamesTheFleetADaemonWouldReap(t *testing.T) {
	got := formatStatus(rpc.Status{Fleet: "dev", Sessions: []rpc.SessionStatus{{ID: idAlpha, State: rpc.StateOrphaned}}}, "")
	if !strings.Contains(got, "Starting a daemon with `wake --fleet dev` reaps") {
		t.Errorf("`wake status` on fleet dev printed %q", got)
	}
}

func TestStopNamesTheFleetInEveryWayOnward(t *testing.T) {
	if _, err := stoppedOutcome(rpc.Status{Running: true, Fleet: "dev"}, nil, true); err == nil || !strings.Contains(err.Error(), "check `wake status --fleet dev`") {
		t.Errorf("a daemon still answering on fleet dev failed with %v", err)
	}
	for _, tc := range []struct {
		name string
		st   rpc.Status
		want string
	}{
		{"parked", rpc.Status{Fleet: "dev", Sessions: []rpc.SessionStatus{{ID: idAlpha, State: rpc.StateParked}}},
			"`wake --fleet dev` starts a daemon that holds them again, and `wake stop --fleet dev` is what forgets them"},
		{"orphans", rpc.Status{Fleet: "dev", Sessions: []rpc.SessionStatus{{ID: idAlpha, State: rpc.StateOrphaned}}},
			"Starting a daemon with `wake --fleet dev` reaps"},
		{"a short sweep", rpc.Status{Fleet: "dev", ProbeIncomplete: true}, "Check `wake status --fleet dev`"},
	} {
		var out bytes.Buffer
		_ = reportNothingToStop(tc.st, &out)
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("`wake stop` with no daemon and %s printed %q, want %q", tc.name, out.String(), tc.want)
		}
	}
	frames, errs := make(chan rpc.Frame), make(chan error, 1)
	close(frames)
	errs <- os.ErrDeadlineExceeded
	if err := waitForTheDaemonToLetGo(frames, errs, "dev"); err == nil || !strings.Contains(err.Error(), "check `wake status --fleet dev`") {
		t.Errorf("a daemon that did not let go of fleet dev failed with %v", err)
	}
}

func TestAttachNamesTheFleetItCannotOpen(t *testing.T) {
	d := startFakeDaemon(t, 0, rpc.Status{Running: true, Fleet: "dev", Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "marco", State: rpc.StateParked},
		{ID: idBeta, Name: "sydney", State: rpc.StateOrphaned},
	}})
	for who, want := range map[string]string{
		"marco":  "run `wake --fleet dev` and type `/resume marco` to bring this conversation back, or `wake fork marco --fleet dev`",
		"sydney": "check `wake status --fleet dev`",
	} {
		if _, _, err := liveSession(d.socket, who); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("`wake attach %s` on fleet dev failed with %v, want %q", who, err, want)
		}
	}
}

func TestTheManagerLineNamesTheFleet(t *testing.T) {
	withScriptedAgent(t, "")
	socket := fleetsHome(t, "dev", "dev")
	d := startRealDaemonAt(t, socket)

	var out bytes.Buffer
	if err := startManager(d.socket, spawnOpts{}, &out); err != nil {
		t.Fatalf("`wake manager` on fleet dev was refused: %v", err)
	}
	if !strings.Contains(out.String(), "`wake --fleet dev` opens the room") {
		t.Errorf("`wake manager` on fleet dev said %q", out.String())
	}
}

// Since bare `wake` started making fleets, the unnamed one is reached by its word.
func TestFleetsNamesTheWordForTheUnnamedFleet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out bytes.Buffer
	if err := printFleets(&out); err != nil {
		t.Fatalf("printFleets: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "`wake --fleet default` is the unnamed fleet") || strings.Contains(got, "a bare `wake` is the unnamed fleet") {
		t.Errorf("an empty listing said %q", got)
	}
}
