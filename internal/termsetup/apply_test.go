package termsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every test here roots ConfigHome at t.TempDir(), so nothing ever touches a
// real ~/.config - the whole point of the seam.

func TestApplyCreatesTheFileWhenNoneExists(t *testing.T) {
	home := t.TempDir()
	result, err := Apply(Ghostty, home)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Outcome != Wrote {
		t.Fatalf("Outcome = %v, want Wrote", result.Outcome)
	}
	if result.BackupPath != "" {
		t.Errorf("BackupPath = %q, want none: there was nothing to back up", result.BackupPath)
	}
	content := readFile(t, result.ConfigPath)
	if !strings.Contains(content, InfoFor(Ghostty).Snippet) {
		t.Errorf("the written file does not contain the snippet: %q", content)
	}
	if !strings.Contains(content, marker) {
		t.Errorf("the written file does not contain the marker: %q", content)
	}
}

func TestApplyAppendsAndBacksUpAnExistingFile(t *testing.T) {
	home := t.TempDir()
	path := InfoFor(Kitty).ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		t.Fatal(err)
	}
	original := "font_size 14\n"
	if err := os.WriteFile(path, []byte(original), configPerm); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(Kitty, home)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Outcome != Wrote {
		t.Fatalf("Outcome = %v, want Wrote", result.Outcome)
	}
	if result.BackupPath == "" {
		t.Fatal("no backup was made of a file that already had content")
	}

	backup := readFile(t, result.BackupPath)
	if backup != original {
		t.Errorf("backup = %q, want the original content untouched: %q", backup, original)
	}

	content := readFile(t, path)
	if !strings.Contains(content, original) {
		t.Errorf("the original content was lost: %q", content)
	}
	if !strings.Contains(content, InfoFor(Kitty).Snippet) {
		t.Errorf("the snippet was not appended: %q", content)
	}
	// The original line must still appear exactly once - not duplicated by
	// the append.
	if strings.Count(content, "font_size 14") != 1 {
		t.Errorf("the original line appears %d times: %q", strings.Count(content, "font_size 14"), content)
	}
}

// Applying twice must not double the snippet - the second run has to
// recognise the first run's own marker and do nothing.
func TestApplyIsIdempotent(t *testing.T) {
	home := t.TempDir()
	first, err := Apply(Alacritty, home)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if first.Outcome != Wrote {
		t.Fatalf("first Outcome = %v, want Wrote", first.Outcome)
	}
	afterFirst := readFile(t, first.ConfigPath)

	second, err := Apply(Alacritty, home)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if second.Outcome != AlreadyConfigured {
		t.Fatalf("second Outcome = %v, want AlreadyConfigured", second.Outcome)
	}

	afterSecond := readFile(t, first.ConfigPath)
	if afterFirst != afterSecond {
		t.Errorf("a second Apply changed the file:\nbefore: %q\nafter:  %q", afterFirst, afterSecond)
	}
	if n := strings.Count(afterSecond, marker); n != 1 {
		t.Errorf("the marker appears %d times after two applies, want 1:\n%s", n, afterSecond)
	}
}

// A file that already carries the exact snippet - written by hand, or by
// Claude Code's own /terminal-setup - must be recognised without our marker,
// so Apply never double-adds a second, textually-identical binding.
func TestApplyRecognisesAnEquivalentHandWrittenSnippet(t *testing.T) {
	home := t.TempDir()
	path := InfoFor(Ghostty).ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		t.Fatal(err)
	}
	handWritten := "font-size = 14\n" + InfoFor(Ghostty).Snippet + "\n"
	if err := os.WriteFile(path, []byte(handWritten), configPerm); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(Ghostty, home)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Outcome != AlreadyConfigured {
		t.Fatalf("Outcome = %v, want AlreadyConfigured", result.Outcome)
	}
	if got := readFile(t, path); got != handWritten {
		t.Errorf("the file changed even though it was already configured:\nbefore: %q\nafter:  %q", handWritten, got)
	}
}

func TestStatusReadsWithoutWriting(t *testing.T) {
	home := t.TempDir()
	configured, conflict, path, err := Status(Ghostty, home)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if configured {
		t.Error("a directory with nothing in it reports as configured")
	}
	if conflict {
		t.Error("a directory with nothing in it reports a conflict")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Status created %s; it must never write", path)
	}

	if _, err := Apply(Ghostty, home); err != nil {
		t.Fatal(err)
	}
	configured, _, _, err = Status(Ghostty, home)
	if err != nil {
		t.Fatalf("Status after Apply: %v", err)
	}
	if !configured {
		t.Error("Status did not see the configuration Apply just wrote")
	}
}

// --- Alacritty's own hazard: an existing [keyboard] table ----------------

// Alacritty's docs show key bindings written two ways - an inline array
// under [keyboard], or repeated [[keyboard.bindings]] entries, which is the
// shape Snippet uses. Appending the second form onto a file already using
// the first is a TOML redefinition error, so Apply must refuse rather than
// risk a config that no longer parses.
func TestApplyRefusesAlacrittyWhenAKeyboardTableAlreadyExists(t *testing.T) {
	home := t.TempDir()
	path := InfoFor(Alacritty).ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		t.Fatal(err)
	}
	existing := "[keyboard]\nbindings = [\n  { key = \"N\", mods = \"Control\", action = \"CreateNewWindow\" },\n]\n"
	if err := os.WriteFile(path, []byte(existing), configPerm); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(Alacritty, home)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Outcome != Conflict {
		t.Fatalf("Outcome = %v, want Conflict", result.Outcome)
	}
	if len(result.ManualSteps) == 0 {
		t.Error("a Conflict result carries no manual steps")
	}
	if got := readFile(t, path); got != existing {
		t.Errorf("Apply touched a file it refused to write to:\nbefore: %q\nafter:  %q", existing, got)
	}
}

// Status must see the same conflict Apply would refuse on, without writing
// anything - the CLI's own preview step relies on this to skip the
// confirmation prompt for a change that is about to be declined anyway.
func TestStatusReportsTheAlacrittyConflict(t *testing.T) {
	home := t.TempDir()
	path := InfoFor(Alacritty).ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[keyboard]\n"), configPerm); err != nil {
		t.Fatal(err)
	}

	configured, conflict, _, err := Status(Alacritty, home)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if configured {
		t.Error("an unrelated [keyboard] table reports as already configured")
	}
	if !conflict {
		t.Error("Status did not see the [keyboard] table conflict")
	}
}

// A [keyboard] table is not itself dangerous once our own marker is already
// there - a second run must still recognise its own idempotent case first.
func TestApplyIsIdempotentEvenWithAKeyboardTableConflict(t *testing.T) {
	home := t.TempDir()
	if _, err := Apply(Alacritty, home); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	result, err := Apply(Alacritty, home)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if result.Outcome != AlreadyConfigured {
		t.Fatalf("Outcome = %v, want AlreadyConfigured (our own [[keyboard.bindings]] block is itself a "+
			"[keyboard] table, and must not be mistaken for a conflict)", result.Outcome)
	}
}

// Other terminals have no such hazard and must never refuse this way.
func TestOnlyAlacrittyCanConflict(t *testing.T) {
	for _, e := range []Emulator{Ghostty, Kitty} {
		if InfoFor(e).conflicts != nil {
			t.Errorf("%v has a conflict check; only Alacritty's TOML format needs one", e)
		}
	}
}

func TestAlacrittyKeyboardTableConflict(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    bool
	}{
		{"no keyboard table", "font_size = 14\n", false},
		{"exact table header", "[keyboard]\n", true},
		{"table header with surrounding whitespace", "  [keyboard]  \n", true},
		{"a nested table is not the table itself", "[keyboard.bindings]\n", false},
		{"our own array-of-tables entry is not the inline-array table", "[[keyboard.bindings]]\n", false},
		{"empty file", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := alacrittyKeyboardTableConflict([]byte(tc.content)); got != tc.want {
				t.Errorf("alacrittyKeyboardTableConflict(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

// A terminal this package refuses to write to must say so through Apply
// itself, rather than requiring every caller to check AutoWritable first.
func TestApplyOnAManualOnlyTerminalWritesNothing(t *testing.T) {
	home := t.TempDir()
	result, err := Apply(ITerm2, home)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Outcome != ManualOnly {
		t.Fatalf("Outcome = %v, want ManualOnly", result.Outcome)
	}
	if len(result.ManualSteps) == 0 {
		t.Error("ManualOnly result carries no steps")
	}
	entries, err := os.ReadDir(home)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("Apply on a manual-only terminal created %v under the config home", entries)
	}
}

// --- Undo --------------------------------------------------------------

// The central claim: Undo removes exactly what Apply added, and nothing an
// operator wrote before or after it - proven as a round trip rather than by
// asserting on the diff, so it cannot pass by accident.
func TestUndoRemovesExactlyWhatApplyAdded(t *testing.T) {
	for _, tc := range []struct {
		name     string
		original string
	}{
		{"nothing existed before", ""},
		{"existing content with a trailing newline", "font_size 14\n"},
		{"existing content with no trailing newline", "font_size 14"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			path := InfoFor(Kitty).ConfigPath(home)
			if tc.original != "" {
				if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.original), configPerm); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := Apply(Kitty, home); err != nil {
				t.Fatalf("Apply: %v", err)
			}

			undone, err := Undo(Kitty, home)
			if err != nil {
				t.Fatalf("Undo: %v", err)
			}
			if undone.Outcome != Removed {
				t.Fatalf("Outcome = %v, want Removed", undone.Outcome)
			}

			got := readFile(t, path)
			// removeBlock's one documented imprecision: content with no
			// trailing newline comes back with one added, because Apply
			// normalises that before appending its own block.
			want := tc.original
			if want != "" && !strings.HasSuffix(want, "\n") {
				want += "\n"
			}
			if got != want {
				t.Errorf("after undo = %q, want %q", got, want)
			}
			if strings.Contains(got, marker) {
				t.Errorf("the marker survived undo: %q", got)
			}
		})
	}
}

// Undo on a file Apply never touched must refuse rather than guess - there
// is no marker to find, so nothing may be removed.
func TestUndoOnAnUnconfiguredFileDoesNothing(t *testing.T) {
	home := t.TempDir()
	path := InfoFor(Ghostty).ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		t.Fatal(err)
	}
	original := "some setting = true\n"
	if err := os.WriteFile(path, []byte(original), configPerm); err != nil {
		t.Fatal(err)
	}

	result, err := Undo(Ghostty, home)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if result.Outcome != NothingToUndo {
		t.Fatalf("Outcome = %v, want NothingToUndo", result.Outcome)
	}
	if got := readFile(t, path); got != original {
		t.Errorf("Undo changed a file it found no marker in:\nbefore: %q\nafter:  %q", original, got)
	}
}

// A hand edit after Apply that disturbs the exact block - inserting a line
// in the middle of it - must also refuse: Undo matches the suffix exactly
// or not at all, never a fuzzy diff that could delete part of somebody's own
// edit along with the marker.
func TestUndoRefusesWhenTheBlockWasEditedSince(t *testing.T) {
	home := t.TempDir()
	if _, err := Apply(Alacritty, home); err != nil {
		t.Fatal(err)
	}
	path := InfoFor(Alacritty).ConfigPath(home)
	edited := readFile(t, path) + "\n# a line the operator added after wake ran\n"
	if err := os.WriteFile(path, []byte(edited), configPerm); err != nil {
		t.Fatal(err)
	}

	result, err := Undo(Alacritty, home)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if result.Outcome != NothingToUndo {
		t.Fatalf("Outcome = %v, want NothingToUndo: an edited block must not be guessed at", result.Outcome)
	}
	if got := readFile(t, path); got != edited {
		t.Errorf("Undo touched a file whose block no longer matched exactly:\nbefore: %q\nafter:  %q", edited, got)
	}
}

// Undo must never fall back to restoring the .bak file: anything written to
// the config between Apply and Undo would be silently discarded if it did.
func TestUndoNeverRestoresFromBackup(t *testing.T) {
	home := t.TempDir()
	path := InfoFor(Kitty).ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		t.Fatal(err)
	}
	original := "font_size 14\n"
	if err := os.WriteFile(path, []byte(original), configPerm); err != nil {
		t.Fatal(err)
	}
	applied, err := Apply(Kitty, home)
	if err != nil {
		t.Fatal(err)
	}
	if applied.BackupPath == "" {
		t.Fatal("expected a backup")
	}

	// An edit lands after Apply, changing the part of the file the backup
	// itself predates.
	withEdit := strings.Replace(readFile(t, path), "font_size 14", "font_size 16", 1)
	if err := os.WriteFile(path, []byte(withEdit), configPerm); err != nil {
		t.Fatal(err)
	}

	result, err := Undo(Kitty, home)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != Removed {
		t.Fatalf("Outcome = %v, want Removed", result.Outcome)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "font_size 16") {
		t.Errorf("the post-Apply edit was lost - undo restored from backup instead of stripping the block: %q", got)
	}
	if strings.Contains(got, marker) {
		t.Errorf("the marker survived undo: %q", got)
	}
}

// Undo on a manual-only terminal must also do nothing, rather than looking
// for a marker in a file this package never wrote.
func TestUndoOnAManualOnlyTerminalWritesNothing(t *testing.T) {
	home := t.TempDir()
	result, err := Undo(VSCode, home)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if result.Outcome != ManualOnly {
		t.Fatalf("Outcome = %v, want ManualOnly", result.Outcome)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
