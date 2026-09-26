package core

import (
	"os"
	"strings"
	"testing"
)

// A launch from a daemon speaking another launcher protocol is a binary
// replaced under a running fleet. It is named as that, rather than falling
// through to verb dispatch as an unknown command nobody typed.
func TestALauncherFromAnotherProtocolSaysTheBinaryWasReplaced(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		marker string
		set    bool
		want   bool
	}{
		{"this protocol", []string{"wake", agentLauncherArg}, agentLauncherProtocol, true, false},
		{"another protocol", []string{"wake", agentLauncherArg}, "0", true, true},
		{"no marker", []string{"wake", agentLauncherArg}, "", false, false},
		{"a verb under a stray marker", []string{"wake", "status"}, "0", true, false},
		{"no arguments", []string{"wake"}, "0", true, false},
	}
	for _, c := range cases {
		lookup := func(string) (string, bool) { return c.marker, c.set }
		err := agentLauncherMismatch(c.args, lookup)
		if (err != nil) != c.want {
			t.Errorf("%s: err = %v, want mismatch %v", c.name, err, c.want)
		}
		if err != nil && !strings.Contains(err.Error(), "⌃Q⌃Q") {
			t.Errorf("%s: %q does not say how to recover", c.name, err)
		}
	}
}

// The mismatch is reported on the status fd as an ERROR frame, because a
// daemon that started the launcher drops its stderr and reads only that frame.
// The daemon on the other end is the *older* one, so the ERROR frame's layout
// is the one part of the launcher protocol no bump may change.
func TestTheMismatchReachesTheDaemonAsItsSpawnError(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := reportAgentLauncherFailure(w, errAgentLauncherReplaced); err != errAgentLauncherReplaced {
		t.Fatalf("report returned %v", err)
	}
	if got := readAgentLauncher(r); got == nil || got.Error() != errAgentLauncherReplaced.Error() {
		t.Errorf("the daemon read %v, want %q", got, errAgentLauncherReplaced)
	}
}
