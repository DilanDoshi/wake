package ui

// The filesystem half of `@`, and the three bounds that make it affordable on a
// keystroke.
//
// **It is not on the goroutine that draws.** pathScanMax bounds how many
// entries a read may take and nothing bounds how long it may take: a hard NFS
// mount, a cloud-sync placeholder and a stalled sshfs all park in the syscall,
// and Bubble Tea has one Update goroutine that renders *and* answers keys - so
// a read there is a window that stops drawing and stops taking the keys that
// would quit it. It is a tea.Cmd, exactly as a bang is, and the answer arrives
// as a message tagged with the directory it was of so a read that outlived its
// draft is dropped rather than drawn.
//
// **One read at a time, and one read per directory.** A directory that never
// answers must cost one goroutine rather than one per character typed into it,
// so nothing is dispatched while a read is out - which means one stalled mount
// leaves the path half silent until it answers, the names and the commands
// still being offered meanwhile. And a read is a *listing*, held on the menu
// and narrowed per keystroke, so typing a path costs one read rather than one
// per character. The listing is dropped when the menu closes, so a menu opened
// again is a directory read again.
//
// **One directory, never a walk.** Stepping into a subdirectory is ⇥ on the
// directory itself, which is a keystroke somebody chose; a recursive scan of a
// repository per character typed is what "cheap to leave open" prices at thirty.
//
// **Bounded by entries.** os.ReadDir sorts the whole listing - unbounded work
// in a directory nobody bounded, and node_modules is the ordinary case - while
// File.ReadDir(n) stops after n. A bigger directory is completed from the first
// pathScanMax entries, and the menu says how many it left out.
//
// Paths resolve against the target session's Dir, because that is where the
// agent resolves the reference. `@../` reaches outside it: that is what the
// operator typed, on their own machine, and the read is a listing. A session
// Wake knows no directory for offers no paths at all, rather than references
// the agent cannot resolve.

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	// pathScanMax is how many directory entries one read may take.
	pathScanMax = 512

	// dotPrefix marks the entries a bare `@` leaves out. At a repository root
	// the alternative is a menu that opens on .git.
	dotPrefix = "."
)

// pathEntry is one directory entry as a value the model can hold. fs.DirEntry
// is an interface over a read that has finished, and what the menu needs of it
// is two fields.
type pathEntry struct {
	name  string
	isDir bool
}

// pathMenu is the `@` half of one menu: the directory it offers from, the
// listing it has, and the read that is out.
type pathMenu struct {
	// want is the directory this menu offers from, absolute. Empty is a menu
	// with no path half at all - a `/command`, or a session Wake knows no
	// directory for.
	want string

	// typed is the directory part as it was typed, which is how an offer is
	// spelled, and base is the prefix being matched inside it.
	typed, base string

	// dir is what entries is a listing of, and entries is that listing:
	// bounded, and unfiltered, because the prefix and the dotfile rule are a
	// scan of a slice and belong to the keystroke rather than to the read.
	dir     string
	entries []pathEntry

	// out is the directory a read is on a goroutine for, empty for none.
	out string
}

// pathScanMsg is one finished read. It names the directory it was of, which is
// what tells a menu's own answer from one it has stopped waiting for.
type pathScanMsg struct {
	dir     string
	entries []pathEntry
}

// pathMenuFor is which directory a mention offers from and what it matches
// there. Pure - the read is scanning's.
func (a App) pathMenuFor(typed string) pathMenu {
	root := a.completionAgent().Cwd
	if root == "" {
		return pathMenu{}
	}
	dir, base := filepath.Split(typed)
	return pathMenu{want: filepath.Join(root, dir), typed: dir, base: base}
}

// rows is the path half of the menu as it is drawn now: the listing this menu
// asked for, narrowed to what has been typed since it arrived.
//
// Nothing at all until the read has answered for this directory, which is what
// a menu over a stalled mount offers - the names, and no paths.
func (p pathMenu) rows() []string {
	if p.want == "" || p.want != p.dir {
		return nil
	}
	lower := strings.ToLower(p.base)
	out := make([]string, 0, len(p.entries))
	for _, e := range p.entries {
		if !strings.HasPrefix(strings.ToLower(e.name), lower) {
			continue
		}
		if strings.HasPrefix(e.name, dotPrefix) && !strings.HasPrefix(p.base, dotPrefix) {
			continue
		}
		name := e.name
		if e.isDir {
			// A separator rather than a space, so ⇥ on a directory steps into
			// it. See acceptCompletion.
			name += string(os.PathSeparator)
		}
		out = append(out, agentPrefix+p.typed+name)
	}
	slices.Sort(out)
	return out
}

// carrying is what a rebuilt menu keeps from the one it replaces: the read that
// is out, and the listing when the new menu offers from the same directory.
//
// The read is carried whatever the new menu is, because the goroutine exists
// whether or not anything still wants its answer - dropping it here is what
// would let a second one start beside it.
func (p pathMenu) carrying(prev pathMenu) pathMenu {
	p.out = prev.out
	if p.want != "" && p.want == prev.dir {
		p.dir, p.entries = prev.dir, prev.entries
	}
	return p
}

// scanning starts the read this menu needs, if it needs one and none is out.
func (a App) scanning() (App, tea.Cmd) {
	p := a.completion.paths
	if p.want == "" || p.want == p.dir || p.out != "" {
		return a, nil
	}
	a.completion.paths.out = p.want
	return a, scanPaths(p.want)
}

// scanPaths reads one directory off the draw goroutine. The read is separated
// from the tea.Cmd the way bangRun is, so a test can run it synchronously.
func scanPaths(dir string) tea.Cmd {
	return func() tea.Msg { return pathScanMsg{dir: dir, entries: readDirBounded(dir)} }
}

// pathsScanned folds a finished read into the menu that asked for it, and asks
// for another when the draft moved to a different directory while it read.
func (a App) pathsScanned(m pathScanMsg) (App, tea.Cmd) {
	if m.dir != a.completion.paths.out {
		// A read nothing is waiting on: the keys moved to another pane, or the
		// menu was rebuilt for another directory before this answered.
		return a, nil
	}
	a.completion.paths.out = ""
	a.completion.paths.dir, a.completion.paths.entries = m.dir, m.entries
	a.completion = a.completion.bounded()
	return a.scanning()
}

// readDirBounded reads at most pathScanMax entries of one directory.
//
// **A failure is not reported**, and that is a ruling rather than a swallow: on
// most keystrokes of a path being typed the directory named does not exist yet,
// so a notice row here would be one line per character. The consequence of
// being wrong is an empty menu, which is what a menu with nothing to offer
// looks like anyway - and it is recorded as a listing either way, so a path
// that names nothing is read once rather than once per character.
func readDirBounded(dir string) []pathEntry {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	entries, err := f.ReadDir(pathScanMax)
	// io.EOF is an empty directory rather than a failure - ReadDir(n) reports
	// it when there was nothing at all to read.
	if err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	out := make([]pathEntry, 0, len(entries))
	for _, e := range entries {
		// A filename is external bytes too, and this is the third producer of
		// them that reaches the frame through neither the airlock nor bangRun.
		// An agent writes files, so a name carrying an escape sequence is a
		// delivery route rather than a curiosity. docs/notes/bugs.md BUG-9.
		out = append(out, pathEntry{name: core.Contained(e.Name()), isDir: e.IsDir()})
	}
	return out
}
