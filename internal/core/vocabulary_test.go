package core

import "testing"

func TestIsModelReply(t *testing.T) {
	cases := map[string]bool{
		"Current model: Opus 5 (1M context) (effort: xhigh)\nUsage: /model <name>.": true,
		"Current model: Sonnet 5 (effort: medium)":                                 true,
		"  Current model: Fable 5 (effort: low)":                                   true,
		"Sure, the current model is opus":                                          false,
		"":                                                                         false,
	}
	for in, want := range cases {
		if got := IsModelReply(in); got != want {
			t.Errorf("IsModelReply(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestModelFromModelReply(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		ok             bool
	}{
		// The recorded shape: a model name that itself carries a parenthesised
		// note, the effort clause, and a second usage line - all of which the
		// parse must strip off without eating the "(1M context)" the name owns.
		{"the recorded reply", "Current model: Opus 5 (1M context) (effort: xhigh)\nUsage: /model <name>.", "Opus 5 (1M context)", true},
		{"no note", "Current model: Sonnet 5 (effort: medium)", "Sonnet 5", true},
		{"leading space", "  Current model: Fable 5 (effort: low)", "Fable 5", true},
		{"not a model reply", "Sure, the current model is opus", "", false},
		{"the prefix and nothing else", "Current model:", "", false},
		{"empty", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ModelFromModelReply(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Errorf("ModelFromModelReply(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestEffortFromModelReply(t *testing.T) {
	lvl, ok := EffortFromModelReply("Current model: Opus 5 (1M context) (effort: xhigh)")
	if !ok || lvl != "xhigh" {
		t.Fatalf("got (%q,%v), want (\"xhigh\",true)", lvl, ok)
	}
	if _, ok := EffortFromModelReply("Current model: Opus 5"); ok {
		t.Error("a reply with no effort clause must not parse")
	}
	if _, ok := EffortFromModelReply("Current model: X (effort: bogus)"); ok {
		t.Error("an effort not in EffortCommands must not parse")
	}
}
