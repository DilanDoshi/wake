package main

// `wake stop` ends a named fleet for good: once it is down and done its
// directory goes and its name is free (BUG-21). Every test here runs in a
// scratch HOME with $WAKE_SOCKET unset - never against ~/.wake.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
)

// stoppedFleet is a scratch HOME holding fleet dev with files, and the socket a
// daemon of it binds.
func stoppedFleet(t *testing.T, files ...string) string {
	t.Helper()
	t.Setenv(daemon.SocketEnv, "")
	socket := fleetsHome(t, "dev", "dev")
	for _, file := range files {
		path := filepath.Join(filepath.Dir(socket), file)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return socket
}

func vanished(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

// The live path: a real daemon on a named fleet, holding a park book a ⌃Q left,
// is stopped - saying it forgets that book (BUG-72) - and the fleet with it.
func TestStopRemovesANamedFleetOnceItIsDown(t *testing.T) {
	socket := stoppedFleet(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(socket), "parked.json"), []byte(`[{"id":"`+idAlpha+`","name":"alex"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	plantTranscript(t, idAlpha)
	d := startRealDaemonAt(t, socket)

	var out bytes.Buffer
	if err := stopFleet(socket, &out); err != nil {
		t.Fatalf("stopFleet: %v", err)
	}
	d.waitForExit(t)
	if !vanished(t, filepath.Dir(socket)) {
		t.Errorf("fleet dev's directory survived its stop:\n%s", out.String())
	}
	for _, want := range []string{"forgetting 1 agent parked", "Removed fleet dev"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the stop did not say %q:\n%s", want, out.String())
		}
	}
}

// A fleet already stopped, with nothing parked, is removed by a stop with no
// daemon to ask: the stops from before this fix left exactly these.
func TestStopRemovesAStoppedFleetThatIsDone(t *testing.T) {
	socket := stoppedFleet(t, "daemon.lock", "daemon.log", "mcp.json")

	var out bytes.Buffer
	if err := stopFleet(socket, &out); err != nil {
		t.Fatalf("stopFleet: %v", err)
	}
	if !vanished(t, filepath.Dir(socket)) || !strings.Contains(out.String(), "Removed fleet dev") {
		t.Errorf("a done fleet was not removed:\n%s", out.String())
	}
}

// A stopped fleet's parked sessions are forgotten by its stop (owner's
// 2026-11-11 ruling), each one named, and the fleet removed with them.
func TestStopForgetsAStoppedFleetsParkedSessionsByName(t *testing.T) {
	socket := stoppedFleet(t, "daemon.lock", "daemon.log")
	book := `[{"id":"a","name":"alex"},{"id":"m","name":"manager"}]`
	if err := os.WriteFile(filepath.Join(filepath.Dir(socket), "parked.json"), []byte(book), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := stopFleet(socket, &out); err != nil {
		t.Fatalf("stopFleet: %v", err)
	}
	if !vanished(t, filepath.Dir(socket)) {
		t.Errorf("a stopped fleet with parked sessions survived its stop:\n%s", out.String())
	}
	if want := "Forgot 2 agents parked (alex, manager) and removed fleet dev"; !strings.Contains(out.String(), want) {
		t.Errorf("the stop said %q, want %q", out.String(), want)
	}
}

// What keeps a stopped fleet is said, with what to do about it.
func TestStopSaysWhatKeepsAFleet(t *testing.T) {
	for _, tc := range []struct {
		what, file, says string
	}{
		{"a debug log", "debug/alex.log", "it holds debug/, which Wake does not remove"},
		{"a crash", "parkready.json", "`wake --fleet dev` recovers what it left"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			socket := stoppedFleet(t, "daemon.lock", tc.file)

			var out bytes.Buffer
			if err := stopFleet(socket, &out); err != nil {
				t.Fatalf("stopFleet: %v", err)
			}
			if vanished(t, filepath.Join(filepath.Dir(socket), tc.file)) {
				t.Fatalf("%s was removed", tc.file)
			}
			if !strings.Contains(out.String(), "Fleet dev stays") || !strings.Contains(out.String(), tc.says) {
				t.Errorf("the stop said %q, want it to say why dev stays: %q", out.String(), tc.says)
			}
		})
	}
}

// A command refused, or one that only looks, makes no fleet.
func TestATyposFleetIsNeverMade(t *testing.T) {
	socket := stoppedFleet(t)
	fleets := filepath.Dir(filepath.Dir(socket))
	for _, args := range [][]string{
		{"--fleet", "typo", "bogus"},
		{"--fleet", "typo", "status"},
		{"--fleet", "typo", "stop"},
		{"--fleet", "typo", "fleets"},
	} {
		_ = run(args, io.Discard)
		if !vanished(t, filepath.Join(fleets, "typo")) {
			t.Errorf("wake %s made fleet typo", strings.Join(args, " "))
		}
	}
}
