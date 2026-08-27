package core

import (
	"strings"
	"testing"
)

// Every added directory reaches claude as **its own argv word**, behind its own
// flag.
//
// Asserted by index and never on a joined string, which is the whole point of
// this test: `strings.Join(args, " ")` cannot tell `--add-dir /a /b` (three
// words) from `--add-dir "/a /b"` (two), and the second is a session handed one
// directory that does not exist while neither real one is added - with nothing
// anywhere saying so. A directory may hold a space, so the two are not
// theoretical spellings of one thing.
func TestEveryAddedDirectoryIsItsOwnArgvWord(t *testing.T) {
	want := []string{"/repo/lib", "/repo/docs", "/Users/someone/a b"}
	args, err := NewSession(Config{SessionID: "uuid-1", AddDir: want}).buildArgs()
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}

	var got []string
	for i, a := range args {
		if a != "--add-dir" {
			continue
		}
		if i+1 >= len(args) {
			t.Fatalf("--add-dir is the last word in %v, so it names no directory", args)
		}
		got = append(got, args[i+1])
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("the argv carries %q, want %q as separate words", got, want)
	}
}

func TestBuildArgsCarriesTheDebugFlags(t *testing.T) {
	args, err := NewSession(Config{
		SessionID: "uuid-1",
		Debug:     "api,hooks",
		DebugFile: "/fleet/debug/alex.log",
	}).buildArgs()
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	joined := " " + strings.Join(args, " ") + " "
	for _, want := range []string{" --debug api,hooks ", " --debug-file /fleet/debug/alex.log "} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q\ngot: %s", want, joined)
		}
	}
}

// The other half of every optional flag: nothing chosen leaves the flag off
// entirely, so claude applies its own default rather than parsing an empty one.
func TestBuildArgsLeavesOffTheSpawnFlagsNobodyChose(t *testing.T) {
	args, err := NewSession(Config{SessionID: "uuid-1"}).buildArgs()
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	joined := " " + strings.Join(args, " ") + " "
	for _, absent := range []string{"--add-dir", "--debug", "--debug-file"} {
		if strings.Contains(joined, " "+absent+" ") {
			t.Errorf("args carry %q for a Config that chose none\ngot: %s", absent, joined)
		}
	}
	if !strings.Contains(joined, " --print ") {
		t.Fatal("args carry no --print, so this test asserted nothing about what was left off")
	}
}
