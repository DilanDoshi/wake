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
// **A listing steps; the index searches.** This file lists one directory, for
// a draft that is walking them - a bare `@`, a path ending in a separator or
// starting with `/`, `~` or `.` - and ⇥ on a directory steps into it. Other
// typed text is ranked over the project's files and directories
// (completionindex.go): one bounded git per menu opening, off this goroutine,
// rather than a recursive scan per character typed, which is what "cheap to
// leave open" prices at thirty. That reverses the old "one directory, never a
// walk" (owner's 2026-09-27 ruling); a directory git does not answer for still
// gets the listing.
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

	// query is the typed text when it searches rather than steps, and root the
	// directory the search is over: the session's own.
	query, root string

	// index is root's git answer - nil until it lands, and dropped by carrying
	// when the menu closes - and indexing the directory a git is out for.
	index    *fileIndex
	indexing string

	// rank is a search's ranked rows and the key they were ranked for.
	rank rankCache
}

// pathScanMsg is one finished read: a directory's listing, or with index set a
// git's answer for it. It names the directory it was of, which is what tells a
// menu's own answer from one it has stopped waiting for.
type pathScanMsg struct {
	dir     string
	entries []pathEntry
	index   *fileIndex
}

// pathMenuFor is which directory a mention offers from and what it matches
// there. Pure - the read is scanning's.
func (a App) pathMenuFor(typed string) pathMenu {
	root := a.completionAgent().Cwd
	if root == "" {
		return pathMenu{}
	}
	dir, base := filepath.Split(typed)
	p := pathMenu{want: filepath.Join(root, dir), typed: dir, base: base, root: root}
	if a.focus != "" { // only a conversation searches; the room lists
		p.query = searchQuery(typed)
	}
	return p
}

// rows is the path half of the menu as it is drawn now, and how many more it
// has than it returns: a search's ranking, or the listing - where it steps, or
// where git is silent (pathMenu.lists). A listing that has nothing to offer a
// search leaves the ranking standing: a head naming no directory at the root
// is still a search through deeper ones.
func (p pathMenu) rows() ([]string, int) {
	if !p.lists() {
		return p.rank.rows, p.rank.rest // ranked by bounded; see reranked
	}
	if listed := p.listed(); len(listed) > 0 || !p.searching() {
		return listed, 0
	}
	return p.rank.rows, p.rank.rest
}

// listed is the listing this menu asked for, narrowed to what has been typed
// since it arrived. Nothing at all until the read has answered for this
// directory, which is what a menu over a stalled mount offers - the names, and
// no paths.
func (p pathMenu) listed() []string {
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

// carrying is what a rebuilt menu keeps from the one it replaces: the read and
// the git that are out, the listing when the new menu offers from the same
// directory, the index while it searches the same one, and the rank while it
// ranks that index - reranked decides whether the query moved.
//
// The read and the git are carried whatever the new menu is, because the
// goroutine exists whether or not anything still wants its answer - dropping it
// here is what would let a second one start beside it.
func (p pathMenu) carrying(prev pathMenu) pathMenu {
	p.out, p.indexing = prev.out, prev.indexing
	if p.want != "" && p.want == prev.dir {
		p.dir, p.entries = prev.dir, prev.entries
	}
	if prev.index != nil && prev.index.dir == p.root {
		p.index = prev.index
	}
	if prev.rank.index == p.index {
		p.rank = prev.rank
	}
	return p
}

// scanning is what a keystroke owes the menu it rebuilt: the git and the
// directory read it needs, and an opening's one FramePeers (completionpeers.go).
// The keystroke path is its only caller, which is what keeps them all off a
// fleet report.
func (a App) scanning() (App, tea.Cmd) {
	a, scan := a.scanningPaths()
	a, ask := a.askingPeers()
	return a, tea.Batch(scan, ask)
}

// scanningPaths starts the git an opening owes and the read this menu needs, if
// it needs one - a search needs none until git is silent - and none is out.
func (a App) scanningPaths() (App, tea.Cmd) {
	a, index := a.indexingPaths()
	p := a.completion.paths
	if p.want == "" || p.want == p.dir || p.out != "" || !p.lists() {
		return a, index
	}
	a.completion.paths.out = p.want
	return a, tea.Batch(index, scanPaths(p.want))
}

// scanPaths reads one directory off the draw goroutine. The read is separated
// from the tea.Cmd the way bangRun is, so a test can run it synchronously.
func scanPaths(dir string) tea.Cmd {
	return func() tea.Msg { return pathScanMsg{dir: dir, entries: readDirBounded(dir)} }
}

// pathsScanned folds a finished read into the menu that asked for it, and asks
// for another when the draft moved to a different directory while it read.
func (a App) pathsScanned(m pathScanMsg) (App, tea.Cmd) {
	if m.index != nil {
		return a.pathsIndexed(m.index)
	}
	if m.dir != a.completion.paths.out {
		// A read nothing is waiting on: the keys moved to another pane, or the
		// menu was rebuilt for another directory before this answered.
		return a, nil
	}
	a.completion.paths.out = ""
	a.completion.paths.dir, a.completion.paths.entries = m.dir, m.entries
	a.completion = a.completion.bounded()
	return a.scanningPaths() // an answer is not a keystroke, so it never asks
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
