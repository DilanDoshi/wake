package ui

// `/new … --add-dir <dir> --debug <filter> --debug-file <name>`: the same
// tokens the shell verb takes, so one grammar covers both surfaces.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewParsesTheSpawnPathsBesideANameAndADirectory(t *testing.T) {
	got, err := parseNew("sydney --add-dir /repos/lib in /repos/api --debug-file syd --add-dir /repos/docs --debug api,hooks")
	if err != nil {
		t.Fatalf("parseNew: %v", err)
	}
	if got.Name != "sydney" || got.Dir != "/repos/api" {
		t.Errorf("name %q dir %q, want sydney and /repos/api - the flags are stripped before the words are counted", got.Name, got.Dir)
	}
	if strings.Join(got.AddDir, " ") != "/repos/lib /repos/docs" {
		t.Errorf("AddDir = %v, want both directories in the order they were written", got.AddDir)
	}
	if got.Debug != "api,hooks" || got.DebugFile != "syd" {
		t.Errorf("debug = %q, file = %q", got.Debug, got.DebugFile)
	}
}

// A relative directory is resolved here for the reason `in <dir>` is: the
// daemon refuses a relative path rather than resolving it, because it would
// resolve against the daemon's own directory.
func TestNewResolvesAnAddedDirectoryAgainstTheClient(t *testing.T) {
	base, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	got, err := parseNew("--add-dir lib")
	if err != nil {
		t.Fatalf("parseNew: %v", err)
	}
	if want := filepath.Join(base, "lib"); strings.Join(got.AddDir, " ") != want {
		t.Errorf("AddDir = %v, want %q", got.AddDir, want)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	got, err = parseNew("--add-dir ~/lib")
	if err != nil {
		t.Fatalf("parseNew: %v", err)
	}
	if want := filepath.Join(home, "lib"); strings.Join(got.AddDir, " ") != want {
		t.Errorf("AddDir = %v, want %q - nothing expands ~ on the way from a composer", got.AddDir, want)
	}
}

// A word that reads as a flag cannot reach the argv as one: resolving it
// against this client's directory happens first, so `-rf` is a directory called
// `-rf` and never a flag claude would read.
func TestAnAddedDirectoryThatLooksLikeAFlagIsResolvedIntoADirectory(t *testing.T) {
	base, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	got, err := parseNew("--add-dir -rf")
	if err != nil {
		t.Fatalf("parseNew: %v", err)
	}
	if want := filepath.Join(base, "-rf"); strings.Join(got.AddDir, " ") != want {
		t.Errorf("AddDir = %v, want %q", got.AddDir, want)
	}
}

// Every flag is fenced here as well as at the daemon, and the refusal names
// the flag it is about rather than leaving the operator to guess which of four
// was wrong.
func TestNewRefusesWhatItWouldNotSend(t *testing.T) {
	for _, tc := range []struct{ arg, names string }{
		{"--add-dir", "add-dir"},
		{"--debug-file", "debug-file"},
		{"--debug-file a/b", "debug-file"},
		{"--debug-file /tmp/log", "debug-file"},
		{"--debug api;rm --debug-file syd", "debug"},
		{"--debug api", "debug-file"},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			_, err := parseNew(tc.arg)
			if err == nil {
				t.Fatalf("parseNew(%q) was accepted", tc.arg)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("the refusal %q does not name %q", err, tc.names)
			}
		})
	}
}

// Every flag `/new` parses has to survive the last hop onto the frame, and this
// is `cmd/wake`'s TestEverySpawnOptReachesTheSpawnFrame for the second surface.
//
// It exists because that hop has no natural failure: parseNew validates, and
// newAgent copies field by field, so a flag added to newFlags and not to the
// frame literal is typed, read, validated, named in newUsage and then silently
// dropped. Proven by mutation - `AddDir: nil` in newAgent left this whole
// package green before this test, and so did `Worktree: ""`, which had been the
// case since the worktree flag shipped.
//
// Derived from newFlags' own fields: a field with no entry in the table below
// fails loudly rather than being skipped.
func TestEveryNewFlagReachesTheSpawnFrame(t *testing.T) {
	fresh(t)

	// One draft that sets all of them, and the value each field must carry.
	// --debug needs --debug-file beside it, which is why this is a draft rather
	// than a value per field.
	const draft = "/new --worktree fix-42 --add-dir /repos/lib --debug api,hooks --debug-file syd" +
		" --max-budget-usd 7.50 --fallback-model opus,sonnet"
	want := map[string]string{
		"Worktree":      "fix-42",
		"AddDir":        "/repos/lib",
		"Debug":         "api,hooks",
		"DebugFile":     "syd",
		"MaxBudgetUSD":  "7.50",
		"FallbackModel": "opus,sonnet",
	}

	fields := reflect.TypeOf(newFlags{})
	for i := range fields.NumField() {
		if _, ok := want[fields.Field(i).Name]; !ok {
			t.Fatalf("newFlags.%s has no expected value here, so this test would skip it silently", fields.Field(i).Name)
		}
	}
	if fields.NumField() != len(want) {
		t.Fatalf("newFlags has %d fields and %d are expected here", fields.NumField(), len(want))
	}

	m, cmd := typeAndSubmit(newRoomApp(t).withSize(200, 40), draft)
	frame := sentFrame(t, m.(App), cmd)

	carried := map[string]bool{}
	fv := reflect.ValueOf(frame)
	for i := range fv.NumField() {
		switch f := fv.Field(i); {
		case f.Kind() == reflect.String:
			carried[f.String()] = true
		case f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.String:
			for j := range f.Len() {
				carried[f.Index(j).String()] = true
			}
		}
	}
	for name, value := range want {
		if !carried[value] {
			t.Errorf("newFlags.%s does not reach the spawn frame: `/new` reads it, validates it and names "+
				"it in the usage line, and then drops it before the socket", name)
		}
	}
}

// A flag standing where a value should be is refused by name. --add-dir is why
// it has to be: absoluteDir turns any word into a path, so without this
// `--add-dir --worktree fix` starts an agent called fix with a directory called
// --worktree and nothing says so.
func TestNewRefusesAFlagStandingWhereAValueShouldBe(t *testing.T) {
	for _, arg := range []string{
		"--add-dir --worktree fix",
		"--worktree --add-dir /lib",
		"--debug-file --debug api",
	} {
		t.Run(arg, func(t *testing.T) {
			got, err := parseNew(arg)
			if err == nil {
				t.Fatalf("parseNew(%q) was accepted as %+v", arg, got)
			}
			if !strings.Contains(err.Error(), "another flag") {
				t.Errorf("the refusal %q does not say a flag stood where a value should be", err)
			}
		})
	}
}

// The flags are matched case-insensitively, which is the composer's own rule
// for `in`: what is typed into a chat surface is not an argv. Stated in
// newFlagNamed's comment and asserted nowhere until now.
func TestNewReadsAFlagWhateverCaseItIsTypedIn(t *testing.T) {
	got, err := parseNew("--Worktree fix-42 --ADD-DIR /repos/lib")
	if err != nil {
		t.Fatalf("parseNew: %v", err)
	}
	if got.Worktree != "fix-42" || strings.Join(got.AddDir, " ") != "/repos/lib" {
		t.Errorf("parseNew read %+v, want the flags taken whatever case they were typed in", got.newFlags)
	}
}

// A flag given twice with two different values is a refusal for the same reason
// the shell verb's is: one of them would be silently ignored. --add-dir is the
// exception, because a session's reach is a list.
func TestNewRefusesTwoDebugLogsAndTakesTwoDirectories(t *testing.T) {
	if _, err := parseNew("--debug-file one --debug-file two"); err == nil {
		t.Error("two debug log names were accepted; one of them would be silently ignored")
	}
	if _, err := parseNew("--add-dir /a --add-dir /b"); err != nil {
		t.Errorf("two added directories were refused: %v", err)
	}
}

// And the shapes that existed before still parse to exactly what they did, with
// none of these - which is what makes the flags additive rather than a new
// grammar.
func TestNewWithoutTheSpawnPathsIsUnchanged(t *testing.T) {
	for _, arg := range []string{"", "sydney", "in /repos/api", "sydney in /repos/api", "--worktree fix"} {
		t.Run(arg, func(t *testing.T) {
			got, err := parseNew(arg)
			if err != nil {
				t.Fatalf("parseNew(%q): %v", arg, err)
			}
			if len(got.AddDir) != 0 || got.Debug != "" || got.DebugFile != "" {
				t.Errorf("parseNew(%q) invented %+v", arg, got.newFlags)
			}
		})
	}
}
