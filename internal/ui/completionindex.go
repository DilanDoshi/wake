package ui

// The project half of `@`: typed text searches the session's files, as Claude
// Code's own `@` does (owner's 2026-09-27 ruling, docs/notes/decisions.md).
//
// **One index per opening, ranked per change.** A menu that offers paths runs
// one `git ls-files` when it opens - a tea.Cmd, one at a time and tagged with
// its directory, as a directory read is - and holds the answer until it closes.
// A keystroke ranks what is held, so typing never costs a walk, and a rebuild
// that moved neither the query nor the index (a fleet report) reuses the rank.
//
// **A path steps; anything else searches.** A bare `@`, a text ending in a
// separator and one starting with `/`, `~` or `.` are somebody walking
// directories, so they keep the listing. Any other text is ranked over the
// index: git's files and every directory above them, as Claude Code's `@`
// offers both. ⇥ on a directory leaves the draft ending in a separator, which
// is a step into its listing.
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

// indexedPath is one file or directory and its lower case, folded once per
// index rather than once per keystroke.
type indexedPath struct {
	path, fold string
	dir        bool
}

// rankedPath is one match as rankPaths orders it: tier, width as drawn (a
// directory draws its separator), kind, then path.
type rankedPath struct {
	tier, width, kind int
	path              string
}

// The kinds, in the order a tie goes: a file finishes the mention, where a
// directory is one more step.
const (
	fileKind = iota
	dirKind
)

// rankCache is a search's ranked rows, keyed on the query and the index they
// rank, so a rebuild that moved neither - a fleet report - reuses them and the
// rank is paid per change, never per event.
type rankCache struct {
	query string
	index *fileIndex
	rows  []string
	rest  int
}

// gitLocation is what would point `git -C dir` at another repository if Wake's
// own environment carried it (a hook, a wrapper), so the lister runs without it.
var gitLocation = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_NAMESPACE"}

// lsFiles runs git over dir and returns what it printed, cut at indexMaxBytes,
// with how many names the cut dropped. A variable so a test can fake a slow,
// huge, failing or non-git one without running git.
var lsFiles = func(dir string) ([]byte, int, error) {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(gitLocation, name)
	})
	return runCapped(indexTimeout, env, gitBinary, "-C", dir, "ls-files", "-co", "--exclude-standard", "-z")
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
	return p.query != "" && (p.index == nil || !p.index.failed)
}

// reranked is this menu with its search ranked: the rows, and how many more -
// the matches it does not draw and the names never indexed. Nothing until git
// has answered, which is what a menu over a slow repository offers: the names,
// and no paths. The rank it carried is reused while its key still holds.
func (p pathMenu) reranked() pathMenu {
	if !p.searching() || (p.rank.query == p.query && p.rank.index == p.index) {
		return p
	}
	var files []indexedPath
	left := 0
	if p.index != nil {
		files, left = p.index.files, p.index.left
	}
	top, total := rankPaths(files, p.query, completionRows)
	rows := make([]string, len(top))
	for i, row := range top {
		rows[i] = agentPrefix + row
	}
	p.rank = rankCache{query: p.query, index: p.index, rows: rows, rest: total - len(top) + left}
	return p
}

// indexingPaths starts an opening's git: for a menu offering paths from a
// directory it holds no index of, when none is out.
func (a App) indexingPaths() (App, tea.Cmd) {
	p := a.completion.paths
	if p.root == "" || p.index != nil || p.indexing != "" {
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
func (a App) pathsIndexed(index *fileIndex) (App, tea.Cmd) {
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
// indexMaxFiles of them, then the directories above them. dropped is how many
// names the byte cap cut; the tail after the last NUL is the one it cut
// through, counted there and never offered.
func parseIndex(dir string, out []byte, dropped int) fileIndex {
	rest := string(out)
	index := fileIndex{dir: dir, left: dropped}
	files := make([]indexedPath, 0, min(strings.Count(rest, nameEnd), indexMaxFiles))
	for prev := ""; ; {
		name, after, ok := strings.Cut(rest, nameEnd)
		if !ok {
			break
		}
		if len(files) == indexMaxFiles {
			index.left += 1 + strings.Count(after, nameEnd)
			break
		}
		if name != prev && !strings.Contains(name, "\n") {
			shown := core.Contained(name)
			files = append(files, indexedPath{path: shown, fold: strings.ToLower(shown)})
		}
		prev, rest = name, after
	}
	index.files = append(files, directoriesOf(files)...)
	return index
}

// directoriesOf is every directory above files, once each, derived when the
// index lands rather than per keystroke. A directory already seen has had its
// parents seen too, so the walk up stops there.
func directoriesOf(files []indexedPath) []indexedPath {
	seen := make(map[string]bool)
	var dirs []indexedPath
	for _, f := range files {
		for p := f.path; ; {
			cut := strings.LastIndex(p, pathSeparator)
			if cut < 0 || seen[p[:cut]] {
				break
			}
			p = p[:cut]
			seen[p] = true
			dirs = append(dirs, indexedPath{path: p, fold: strings.ToLower(p), dir: true})
		}
	}
	return dirs
}

// rankPaths is index narrowed to query: the best k rows - a directory drawn
// with its separator - and how many matched in all. One pass keeping k, since
// it runs on the goroutine that draws, over up to indexMaxFiles names and their
// directories (BenchmarkRankPaths).
//
// The dotfile rule is the listing's over a whole path: a path with a hidden
// segment is offered only to a query with one.
func rankPaths(index []indexedPath, query string, k int) ([]string, int) {
	q := strings.ToLower(query)
	tail := q[strings.LastIndex(q, pathSeparator)+1:]
	reachesHidden := hiddenPath(q)
	best := make([]rankedPath, 0, k+1)
	total := 0
	for _, f := range index {
		tier, ok := pathTier(f.fold, q, tail)
		if !ok || (!reachesHidden && hiddenPath(f.fold)) {
			continue
		}
		total++
		r := rankedPath{tier: tier, width: len(f.path), kind: fileKind, path: f.path}
		if f.dir {
			r.width, r.kind = r.width+len(pathSeparator), dirKind
		}
		if i, _ := slices.BinarySearchFunc(best, r, compareRanked); i < k {
			best = slices.Insert(best, i, r)[:min(len(best)+1, k)]
		}
	}
	rows := make([]string, len(best))
	for i, r := range best {
		rows[i] = r.path
		if r.kind == dirKind {
			rows[i] += pathSeparator
		}
	}
	return rows, total
}

// pathTier is where a folded path ranks for q, whose tail - what follows its
// last separator, or all of it - tiers against the path's name. Spelling q
// through the path is every tier's precondition, so it is asked first; it also
// spells q's head through the directories, since the separator it matches sits
// at or before the name's.
func pathTier(path, q, tail string) (int, bool) {
	if !spelt(path, q) {
		return 0, false
	}
	name := path[strings.LastIndex(path, pathSeparator)+1:]
	switch {
	case strings.HasPrefix(name, tail):
		return tierPrefix, true
	case strings.Contains(name, tail):
		return tierWithin, true
	case spelt(name, tail):
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

// compareRanked orders matches: tier, the shorter row as drawn, a file before a
// directory, then lexically.
func compareRanked(a, b rankedPath) int {
	return cmp.Or(cmp.Compare(a.tier, b.tier), cmp.Compare(a.width, b.width), cmp.Compare(a.kind, b.kind),
		strings.Compare(a.path, b.path))
}

// hiddenPath reports whether any segment of p is a dotfile.
func hiddenPath(p string) bool {
	return strings.HasPrefix(p, dotPrefix) || strings.Contains(p, pathSeparator+dotPrefix)
}

// runCapped runs one lister in env (nil is Wake's own) under bangRun's two
// bounds - its whole group killed at the deadline, and WaitDelay for a pipe
// something it left still holds - and keeps at most indexMaxBytes of its output.
func runCapped(timeout time.Duration, env []string, name string, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out := &indexOutput{kept: bangOutput{limit: indexMaxBytes}}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
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
