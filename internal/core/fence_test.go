package core

import "testing"

// Unfence undoes Fenced, whatever the body holds, so a room that reads a bang back
// off a transcript sees the text the block was built from.
func TestUnfenceUndoesFenced(t *testing.T) {
	for name, body := range map[string]string{
		"one row":           "!ls",
		"rows":              "!ls\na\nb",
		"empty":             "",
		"holds a fence":     "!cat README.md\n```\ncode\n```",
		"holds a long run":  "!echo ````` x",
		"blank rows inside": "!ls\n\na",
	} {
		if got := Unfence(Fenced(body)); got != body {
			t.Errorf("%s: Unfence(Fenced(%q)) = %q", name, body, got)
		}
	}
}

func TestUnfenceLeavesAnythingElseAsItCame(t *testing.T) {
	for name, text := range map[string]string{
		"plain":              "!ls\na",
		"one fence row":      "```",
		"unclosed":           "```\n!ls",
		"closed by a longer": "```\n!ls\n````",
		"no run":             "``\n!ls\n``",
	} {
		if got := Unfence(text); got != text {
			t.Errorf("%s: Unfence(%q) = %q, want it unchanged", name, text, got)
		}
	}
}
