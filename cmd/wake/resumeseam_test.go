package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/ui"
)

// Resumable is the structured half of the same discovery Listing formats: it
// returns the same session ids, each with the directory discovery proved, so the
// resume picker can build and sort rows without walking the disk itself.
func TestResumableReturnsStructuredDiscovery(t *testing.T) {
	projects := projectsTree(t)
	real := t.TempDir()
	transcript(t, projects, real, importA, real)

	got, err := machineSessions{}.Resumable()
	if err != nil {
		t.Fatalf("Resumable: %v", err)
	}
	var found bool
	for _, d := range got {
		if d.ID != importA {
			continue
		}
		found = true
		if d.Dir != real {
			t.Errorf("Resumable Dir = %q, want the proven directory %q", d.Dir, real)
		}
	}
	if !found {
		t.Fatalf("Resumable did not return the seeded session %s; got %d rows", importA, len(got))
	}
}

// A machine with no sessions is a real, common shape — the first day — and
// Resumable answers it with an empty slice and no error, so the picker can take
// the empty-state path rather than crashing on a nil walk.
func TestResumableOnAnEmptyMachine(t *testing.T) {
	projectsTree(t)
	got, err := machineSessions{}.Resumable()
	if err != nil {
		t.Fatalf("Resumable on an empty machine errored: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Resumable found %d sessions on an empty machine", len(got))
	}
}

// docs/notes/bugs.md BUG-50. A disk session's directory is a name anybody who
// can make a directory chose, and discovery hands it on as the filesystem has
// it: the picker's row is the fence (daemon.OneLine's header). That row folded
// whitespace and nothing else, so an escape in the name reached the terminal.
//
// The whole path a real one takes: a transcript on disk, discovery through
// machineSessions, a bare /resume in the room, the drawn frame.
func TestAResumablesDirCannotDriveTheTerminal(t *testing.T) {
	projects := projectsTree(t)
	dir := filepath.Join(t.TempDir(), "proj\x1b[2J\x1b]0;pwned\aend")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	transcript(t, projects, dir, importA, dir)

	var room tea.Model = ui.NewRoomApp(nil, ui.Stream{}, nil).WithSessions(machineSessions{})
	room, _ = room.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	for _, r := range "/resume" {
		room, _ = room.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	room, walk := room.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if walk == nil {
		t.Fatal("a bare /resume did not walk the disk")
	}
	room, _ = room.Update(walk())

	var drawn []string
	for _, line := range strings.Split(room.View(), "\n") {
		if strings.Contains(line, "proj") {
			drawn = append(drawn, line)
		}
	}
	if len(drawn) == 0 {
		t.Fatalf("the picker drew no row for the session in %q:\n%s", dir, room.View())
	}
	for _, line := range drawn {
		text := sgrRuns.ReplaceAllString(line, "")
		if i := strings.IndexFunc(text, actsOnATerminal); i >= 0 {
			t.Errorf("the picker's row carries %q from the directory's name: %q", text[i:i+1], text)
		}
		if !strings.Contains(text, "end") {
			t.Errorf("the row lost the name around the escape: %q", text)
		}
	}
}

// sgrRuns is the frame's own styling, which the check above looks past.
var sgrRuns = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// actsOnATerminal is C0, DEL and C1 - written out here rather than reached
// through a fence, so a fence narrowed by mistake cannot narrow this with it.
func actsOnATerminal(r rune) bool { return r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f }
