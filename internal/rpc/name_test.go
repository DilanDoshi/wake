package rpc

import "testing"

// Every run of whitespace becomes one hyphen and the ends are dropped, so a
// spaced name lands as the one `@`-token Wake stores; a single word is unchanged.
func TestHyphenateNameFoldsWhitespaceIntoOneHyphen(t *testing.T) {
	for in, want := range map[string]string{
		"foo bar":        "foo-bar",
		"  foo   bar  ":  "foo-bar",
		"foo\tbar\nbaz":  "foo-bar-baz",
		"alex":           "alex",
		"":               "",
		"already-hyphen": "already-hyphen",
	} {
		if got := HyphenateName(in); got != want {
			t.Errorf("HyphenateName(%q) = %q, want %q", in, got, want)
		}
	}
}
