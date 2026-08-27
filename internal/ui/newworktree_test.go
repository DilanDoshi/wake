package ui

// `/new … --worktree <name>`: the same token the shell verb takes, so one
// grammar covers both surfaces.

import (
	"strings"
	"testing"
)

func TestNewParsesAWorktreeOnItsOwn(t *testing.T) {
	got, err := parseNew("--worktree fix-42")
	if err != nil {
		t.Fatalf("parseNew: %v", err)
	}
	if got.Worktree != "fix-42" {
		t.Errorf("worktree = %q, want fix-42", got.Worktree)
	}
	if got.Name != "" {
		t.Errorf("name = %q, want none - the daemon draws one from the pool", got.Name)
	}
}

// The flag composes with both of the shapes that already existed, and in either
// order: it is stripped before the `in` keyword is looked for, so it never
// stands where a name or a directory is counted.
func TestNewParsesAWorktreeBesideANameAndADirectory(t *testing.T) {
	for _, arg := range []string{
		"sydney --worktree fix-42 in /repos/api",
		"--worktree fix-42 sydney in /repos/api",
		"sydney in /repos/api --worktree fix-42",
	} {
		t.Run(arg, func(t *testing.T) {
			got, err := parseNew(arg)
			if err != nil {
				t.Fatalf("parseNew(%q): %v", arg, err)
			}
			if got.Name != "sydney" || got.Worktree != "fix-42" || got.Dir != "/repos/api" {
				t.Errorf("parseNew(%q) = name %q, dir %q, worktree %q; want sydney, /repos/api, fix-42", arg, got.Name, got.Dir, got.Worktree)
			}
		})
	}
}

// A flag with nothing after it is a sentence somebody did not finish, and both
// available guesses are silent - start with no worktree, which is the one thing
// they said they did not want, or take the next word as a name.
func TestNewRefusesAWorktreeWithNoName(t *testing.T) {
	if _, err := parseNew("sydney --worktree"); err == nil {
		t.Error("`--worktree` with no name was accepted, so an agent asking for isolation would have got none")
	}
}

// The name becomes a directory, so the same fence the daemon holds runs here
// too - and it names what is legal rather than leaving git to say it after a
// socket round trip.
func TestNewRefusesAWorktreeNameThatIsNotOneSegment(t *testing.T) {
	for _, arg := range []string{"--worktree a/b", "--worktree ..", "--worktree -force"} {
		t.Run(arg, func(t *testing.T) {
			_, err := parseNew(arg)
			if err == nil {
				t.Fatalf("parseNew(%q) was accepted, and the name becomes a directory under the repository", arg)
			}
			if !strings.Contains(err.Error(), "worktree") {
				t.Errorf("the refusal does not name the flag it is about: %q", err)
			}
		})
	}
}

// And the shapes that existed before still parse to exactly what they did, with
// no worktree - which is what makes this flag additive rather than a fourth
// grammar.
func TestNewWithoutAWorktreeIsUnchanged(t *testing.T) {
	for _, tc := range []struct{ arg, name, dir string }{
		{"", "", ""},
		{"sydney", "sydney", ""},
		{"in /repos/api", "", "/repos/api"},
		{"sydney in /repos/api", "sydney", "/repos/api"},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			got, err := parseNew(tc.arg)
			if err != nil {
				t.Fatalf("parseNew(%q): %v", tc.arg, err)
			}
			if got.Worktree != "" {
				t.Errorf("parseNew(%q) invented the worktree %q", tc.arg, got.Worktree)
			}
			if got.Name != tc.name {
				t.Errorf("parseNew(%q) name = %q, want %q", tc.arg, got.Name, tc.name)
			}
			if tc.dir != "" && got.Dir != tc.dir {
				t.Errorf("parseNew(%q) dir = %q, want %q", tc.arg, got.Dir, tc.dir)
			}
		})
	}
}
