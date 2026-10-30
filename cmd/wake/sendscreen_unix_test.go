//go:build unix

// One message between two fleet agents, on a real screen. An agent replying to a
// cross-session message names the sender by the socket address it came from,
// and the room draws the message once - from the receiver's stream - under the
// recipient's name, never the address.

package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestASendToAFleetAgentByItsAddressIsDrawnOnceUnderItsName(t *testing.T) {
	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	s, names := scriptedGridScreen(t, scriptSends, 200, 60)
	from := names[0]
	if from == "robin" {
		from = names[1]
	}
	if err := registerSession(config, sessionRecord{PID: 1, ID: "0e5a1d3c-7d1e-4c3a-9a51-1f0c3b2a9d77",
		Name: "outsider", Socket: filepath.Join(config, "socks", "1.sock")}); err != nil {
		t.Fatalf("register a session outside the fleet: %v", err)
	}

	// By socket address, then by `name [ref]`: each is drawn once, by robin's own
	// stream when it hears it. A session outside the fleet is drawn from the call.
	for _, step := range []struct{ send, hear string }{
		{"@" + from + " send robin one", "@robin hear " + from + " one"},
		{"@" + from + " sendref robin two", "@robin hear " + from + " two"},
		{"@" + from + " send outsider three", ""},
	} {
		words := strings.Fields(step.send)[3]
		s.send(step.send + "\r")
		s.await(sentSaid + words)
		if step.hear != "" {
			s.send(step.hear + "\r")
			s.await(heardSaid + words)
		}
	}
	s.settle()

	out := s.text()
	for _, words := range []string{"one", "two", "three"} {
		if n := strings.Count(out, sentPrefix+words); n != 1 {
			t.Errorf("%q is drawn %d times, want once", sentPrefix+words, n)
		}
	}
	if n := len(regexp.MustCompile(`(?m)^↪ `+from+`\b.* → robin  `).FindAllString(out, -1)); n != 2 {
		t.Errorf("%s → robin is drawn %d times, want once per message", from, n)
	}
	if !regexp.MustCompile(`(?m)^↪ `+from+`\b.* → outsider  `).MatchString(out) || strings.Contains(out, "uds:") || strings.Contains(out, "[0a1b2c]") {
		t.Errorf("the room draws an address rather than the recipient's name")
	}
	if t.Failed() {
		t.Log(s.dump())
	}
}
