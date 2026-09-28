package ui

// The project half of `@`: typed text searches the session's files, as Claude
// Code's own `@` does (owner's 2026-09-27 ruling, docs/notes/decisions.md).
//
// **One index per opening, narrowed per keystroke.** A menu that offers paths
// runs one `git ls-files` when it opens - a tea.Cmd, one at a time and tagged
// with its directory, as a directory read is - and holds the answer until it
// closes. A keystroke ranks what is held, so typing never costs a walk.
//
// **A path steps; anything else searches.** A bare `@`, a text ending in a
// separator and one starting with `/`, `~` or `.` are somebody walking
// directories, so they keep the listing and ⇥'s step into a directory. Any
// other text is ranked over the index, which holds files only.
//
// **Bounded three ways**, since a repository is a directory nobody bounded:
// indexTimeout (the group killed, then bangWaitDelay - bangRun's two bounds and
// its reasons), indexMaxBytes (a writer that claims every write, bangOutput's
// reason) and indexMaxFiles. What a cap leaves out is counted into the menu's
// `more`.
//
// **A git that does not answer falls back to the listing** - no repository, a
// failed exec, a non-zero exit, the deadline. The failure is held on the menu,
// so git is not re-run per keystroke, and reported nowhere, for
// readDirBounded's reason: most directories a menu opens in are not a problem
// worth a row.

import (
	"bytes"
	"cmp"
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	// indexMaxFiles is the most names one index holds.
	indexMaxFiles = 50_000

	// indexMaxBytes is the most of git's answer kept: 160 bytes a name at the
	// entries cap, so that cap binds first in any ordinary repository.
	indexMaxBytes = 8 << 20

	// indexTimeout bounds one git: this repository's answers in about 12ms, so
	// one still going after this is hung on a lock or a mount, not slow.
	indexTimeout = 5 * time.Second

	gitBinary = "git"

	// nameEnd ends each name `ls-files -z` prints, so a space or a newline in
	// one is not a boundary.
	nameEnd = "\x00"

	// pathSeparator is how git spells a path's separator, which on every
	// platform Wake runs on is the OS's own - as completionpath.go spells it.
	pathSeparator = string(os.PathSeparator)
)

// The tiers a search ranks by, best first: the query begins the file's name,
// sits in it, is spelt through it, or is spelt only across its directories.
const (
	tierPrefix = iota
	tierWithin
	tierSpelt
	tierPath
)

// fileIndex is git's answer for one directory: names relative to it, contained
// and bounded, and how many the bounds left out. failed is a git that did not
// answer, which sends the menu back to the listing.
type fileIndex struct {
	dir    string
	files  []indexedPath
	left   int
	failed bool
}

// indexedPath is one name and its lower case, folded once per index rather than
// once per keystroke.
type indexedPath struct{ path, fold string }

// rankedPath is one match and its tier, as rankPaths orders them.
type rankedPath struct {
	tier int
	path string
}

// lsFiles runs git over dir and returns what it printed, cut at indexMaxBytes,
// with how many names the cut dropped. A variable so a test can fake a slow,
// huge, failing or non-git one without running git.
var lsFiles = func(dir string) ([]byte, int, error) {
	return runCapped(indexTimeout, gitBinary, "-C", dir, "ls-files", "-co", "--exclude-standard", "-z")
}

// searchQuery is typed when it searches the project, and "" when it steps
// through directories. See the header.
func searchQuery(typed string) string {
	if typed == "" || strings.HasSuffix(typed, pathSeparator) || strings.IndexAny(typed, pathLeads) == 0 {
		return ""
	}
	return typed
}

// searching reports whether this menu's paths come from the index: a search,
// over a directory git has not failed for.
func (p pathMenu) searching() bool {
	return p.query != "" && !p.index.failed
}

// ranked is a search's rows and how many more it has: the matches it does not
// draw and the names never indexed. Nothing until git has answered - an index
// is empty until then - which is what a menu over a slow repository offers: the
// names, and no paths.
func (p pathMenu) ranked() ([]string, int) {
	top, total := rankPaths(p.index.files, p.query, completionRows)
	rows := make([]string, len(top))
	for i, path := range top {
		rows[i] = agentPrefix + path
	}
	return rows, total - len(top) + p.index.left
}

// indexingPaths starts an opening's git: for a menu offering paths from a
// directory it holds no index of, when none is out.
func (a App) indexingPaths() (App, tea.Cmd) {
	p := a.completion.paths
	if p.root == "" || p.index.dir == p.root || p.indexing != "" {
		return a, nil
	}
	a.completion.paths.indexing = p.root
	return a, indexPaths(p.root)
}

// indexPaths runs git off the draw goroutine, as scanPaths reads a directory.
func indexPaths(dir string) tea.Cmd {
	return func() tea.Msg {
		out, dropped, err := lsFiles(dir)
		index := fileIndex{dir: dir, failed: true}
		if err == nil {
			index = parseIndex(dir, out, dropped)
		}
		return pathScanMsg{dir: dir, index: &index}
	}
}

// pathsIndexed folds a finished git into the menu that asked for it - held only
// while that menu is open over its directory - and starts what the menu wants
// now: the listing a failure falls back to, or the index the draft moved to.
func (a App) pathsIndexed(index fileIndex) (App, tea.Cmd) {
	p := a.completion.paths
	if index.dir != p.indexing {
		return a, nil
	}
	a.completion.paths.indexing = ""
	if index.dir == p.root {
		a.completion.paths.index = index
		a.completion = a.completion.bounded()
	}
	return a.scanningPaths() // an answer is not a keystroke, so it never asks
}

// parseIndex reads git's answer: each name contained (BUG-9, readDirBounded's
// reason), one line, once - a merge prints a name per stage - and at most
// indexMaxFiles of them. dropped is how many names the byte cap cut; the tail
// after the last NUL is the one it cut through, counted there and never offered.
func parseIndex(dir string, out []byte, dropped int) fileIndex {
	rest := string(out)
	index := fileIndex{dir: dir, left: dropped}
	index.files = make([]indexedPath, 0, min(strings.Count(rest, nameEnd), indexMaxFiles))
	for prev := ""; ; {
		name, after, ok := strings.Cut(rest, nameEnd)
		switch {
		case !ok:
			return index
		case len(index.files) == indexMaxFiles:
			index.left += 1 + strings.Count(after, nameEnd)
			return index
		case name != prev && !strings.Contains(name, "\n"):
			shown := core.Contained(name)
			index.files = append(index.files, indexedPath{path: shown, fold: strings.ToLower(shown)})
		}
		prev, rest = name, after
	}
}

// rankPaths is index narrowed to query: the best k matches, and how many
// matched in all. One pass keeping k, since it runs on the goroutine that draws,
// per keystroke, over up to indexMaxFiles names (BenchmarkRankPaths).
//
// The dotfile rule is the listing's over a whole path: a path with a hidden
// segment is offered only to a query with one.
func rankPaths(index []indexedPath, query string, k int) ([]string, int) {
	q := strings.ToLower(query)
	reachesHidden := hiddenPath(q)
	best := make([]rankedPath, 0, k+1)
	total := 0
	for _, f := range index {
		tier, ok := pathTier(f.fold, q)
		if !ok || (!reachesHidden && hiddenPath(f.fold)) {
			continue
		}
		total++
		r := rankedPath{tier: tier, path: f.path}
		if i, _ := slices.BinarySearchFunc(best, r, compareRanked); i < k {
			best = slices.Insert(best, i, r)[:min(len(best)+1, k)]
		}
	}
	rows := make([]string, len(best))
	for i, r := range best {
		rows[i] = r.path
	}
	return rows, total
}

// pathTier is where a folded path ranks for q, and false for a path that does
// not spell q at all - which is every tier's precondition, so it is asked first.
func pathTier(path, q string) (int, bool) {
	if !spelt(path, q) {
		return 0, false
	}
	name := path[strings.LastIndex(path, pathSeparator)+1:]
	switch {
	case strings.HasPrefix(name, q):
		return tierPrefix, true
	case strings.Contains(name, q):
		return tierWithin, true
	case spelt(name, q):
		return tierSpelt, true
	}
	return tierPath, true
}

// spelt reports whether q's bytes appear in s in order.
func spelt(s, q string) bool {
	for i := 0; i < len(s) && q != ""; i++ {
		if s[i] == q[0] {
			q = q[1:]
		}
	}
	return q == ""
}

// compareRanked orders matches: tier, then the shorter path, then lexically.
func compareRanked(a, b rankedPath) int {
	return cmp.Or(cmp.Compare(a.tier, b.tier), cmp.Compare(len(a.path), len(b.path)), strings.Compare(a.path, b.path))
}

// hiddenPath reports whether any segment of p is a dotfile.
func hiddenPath(p string) bool {
	return strings.HasPrefix(p, dotPrefix) || strings.Contains(p, pathSeparator+dotPrefix)
}

// runCapped runs one lister under bangRun's two bounds - its whole group killed
// at the deadline, and WaitDelay for a pipe something it left still holds - and
// keeps at most indexMaxBytes of what it prints.
func runCapped(timeout time.Duration, name string, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out := &indexOutput{kept: bangOutput{limit: indexMaxBytes}}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = out
	cmd.WaitDelay = bangWaitDelay
	bangSetGroup(cmd)
	cmd.Cancel = func() error { return bangKillGroup(cmd) }
	err := cmd.Run()
	return out.kept.buf, out.dropped, err
}

// indexOutput is bangOutput counting the names it drops. bangOutput is a field,
// not embedded, so io.Copy can reach nothing around Write - the bug
// internal/daemon/peers.go's capped fixed.
type indexOutput struct {
	kept    bangOutput
	dropped int
}

func (o *indexOutput) Write(p []byte) (int, error) {
	room := min(max(o.kept.limit-len(o.kept.buf), 0), len(p))
	o.dropped += bytes.Count(p[room:], []byte(nameEnd))
	return o.kept.Write(p)
}
