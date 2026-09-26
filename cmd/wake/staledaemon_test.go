package main

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

func TestAStaleDaemonIsNamedAndACurrentOneIsNot(t *testing.T) {
	const ours = "0.1.6+bbbbbbb"
	cases := []struct {
		name  string
		seed  *rpc.Status
		want  bool
		names []string
	}{
		{"no seed", nil, false, nil},
		{"no daemon", &rpc.Status{Build: "0.1.5"}, false, nil},
		{"same build", &rpc.Status{Running: true, Build: ours}, false, nil},
		{"other build", &rpc.Status{Running: true, Build: "0.1.5+aaaaaaa"}, true, []string{"0.1.5+aaaaaaa", ours, "⌃Q⌃Q"}},
		// A daemon from before builds were reported says nothing, and that
		// silence is itself the answer: it is older than this one.
		{"pre-build daemon", &rpc.Status{Running: true}, true, []string{"older", ours, "⌃Q⌃Q"}},
	}
	for _, c := range cases {
		text, stale := staleDaemonNotice(c.seed, ours)
		if stale != c.want {
			t.Errorf("%s: stale = %v, want %v (%q)", c.name, stale, c.want, text)
		}
		for _, n := range c.names {
			if !strings.Contains(text, n) {
				t.Errorf("%s: notice %q does not name %q", c.name, text, n)
			}
		}
	}
}

// Both models the verbs open go through the check: bare `wake`'s room and the
// room beside a conversation (`wake new`, `attach`, `fork`, `import`).
func TestEveryRoomThatOpensOverAStaleDaemonSaysSo(t *testing.T) {
	stale := &rpc.Status{Running: true, Build: "0.0.1+0000000"}
	open := map[string]func(){
		"room": func() { conversationRoom("", stale, nil, ui.Stream{}, &connection{}) },
		"conversation": func() {
			conversation("", rpc.SessionStatus{ID: "a11a0000-0000-4000-8000-00000000a11a"}, stale, nil, ui.Stream{}, &connection{})
		},
	}
	for name, openIt := range open {
		notice.Reset()
		openIt()
		if n, ok := notice.Latest(); !ok || !strings.Contains(n.Text, stale.Build) {
			t.Errorf("%s over a stale daemon: notice = %q, %v", name, n.Text, ok)
		}
	}
	notice.Reset()
}
