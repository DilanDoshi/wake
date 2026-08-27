// The task label: the second half of a session's identity, so a row reads
// `sydney <> dev-5748` rather than a bare name.
//
// # Where it comes from, and why not a counter
//
// A counter (`dev-5748`-as-an-ordinal, `session-3`) is unique and says nothing.
// At the fleet this product is sized for, the question a label has to answer is
// "which of these thirty is the one I care about", and the two facts that
// answer it are already on the spawn: the directory the client was in, and the
// branch checked out there. The branch wins when there is one, because a person
// running several agents over one repository is running them over several
// branches - that is what worktrees are for - and the directory names would all
// be the same. Outside a repository, or on a detached HEAD, the directory is
// the useful half and is used instead.
//
// # Why the daemon derives it rather than the client sending it
//
// The daemon already receives the client's directory: rpc.Frame.Dir exists
// because one daemon serves every repository on the machine, and spawnDir is
// where a session's cwd is decided. Deriving the label from the same field
// keeps one source for "where is this session", and keeps the wire from growing
// a field whose only job is to be displayed.
//
// # Why git is read rather than run
//
// Wake must be cheap to leave open, and the spawn path is the one place a
// per-session cost is paid. `git rev-parse --abbrev-ref HEAD` is a process
// exec; .git/HEAD is a file with one line in it, and both answer the same
// question. No process is spawned here and nothing runs on a timer.
//
// Everything read here is a file Wake did not write, so nothing read here is
// trusted: the first line only, bounded, control characters dropped, and the
// result truncated to something a column can hold.

package daemon

import (
	"errors"
	"path/filepath"

	"github.com/DilanDoshi/wake/internal/gitref"
	"strings"
)

const (
	// maxLabelLen bounds a label in runes. `wake status` lays its rows out in
	// fixed columns, and thirty agents is only scannable while the rows line
	// up - a branch name has no bound of its own.
	maxLabelLen = 18

	// truncationMark says a label was cut, rather than letting a truncated
	// branch read as a branch that is really called that.
	truncationMark = "…"
)

// taskLabel is what a session is working on, in one column: the branch checked
// out where it was started, or failing that the directory it was started in.
//
// An empty answer is legitimate and means "no label" - a session started in a
// directory that names nothing renders as a bare name rather than as
// `sydney <> .`, which reads as information and is not any.
func taskLabel(dir string) string {
	if branch := gitBranch(dir); branch != "" {
		return truncateLabel(branch)
	}
	return truncateLabel(cleanLabel(baseName(dir)))
}

// baseName is the last element of a path, or nothing when the path has no last
// element worth showing.
func baseName(dir string) string {
	if dir == "" {
		return ""
	}
	switch base := filepath.Base(filepath.Clean(dir)); base {
	case ".", "..", string(filepath.Separator):
		return ""
	default:
		return base
	}
}

// gitBranch is the branch checked out at dir, or nothing.
func gitBranch(dir string) string {
	// The branch only. A detached head has no name, and the label falls back
	// to the directory's own - see labelFor. internal/gitref answers both
	// callers because internal/ui needs the same walk and may not import this
	// package; it used to be implemented twice and the two had drifted.
	return cleanLabel(gitref.Of(dir).Branch)
}

// cleanLabel drops what a column cannot hold. Control characters would move the
// cursor rather than print, and a label is drawn inside a row somebody is
// scanning.
//
// **C1 goes with C0**, which it did not until docs/notes/bugs.md BUG-9: a
// terminal in 8-bit mode reads U+009B as CSI, an escape sequence introducer with
// no ESC in front of it, so `x\u009b2J` cleared the screen through a predicate
// that was only looking below 0x20. This was the one stripper in the tree
// missing that range, and a label is reachable - it is derived from a branch
// name in a directory an agent can `git checkout` in, and settable over the
// socket with /task.
func cleanLabel(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// truncateLabel cuts a label to the column, in runes rather than bytes: a
// branch name can hold anything a filesystem can, and cutting a multi-byte
// character in half produces a replacement glyph in the middle of a row.
func truncateLabel(s string) string {
	runes := []rune(s)
	keep := maxLabelLen - len([]rune(truncationMark))
	if len(runes) <= maxLabelLen {
		return s
	}
	return string(runes[:keep]) + truncationMark
}

// normalizeLabel folds a label an operator typed into the one form a row can
// hold, or says why it cannot be one.
//
// It is the same two bounds a derived label passes through and not a second
// set: cleanLabel drops the control characters that would move a cursor rather
// than print, and truncateLabel cuts to the column in runes and marks the cut.
// A label somebody types is held to exactly what a branch name is held to,
// because they land in the same column beside the same thirty rows.
//
// It is deliberately *not* normalizeName. A label is prose - the founding
// message's own example is `alex <> ui fixes`, with a space in it - and it is
// never resolved, never unique, and never reaches an argv. Everything
// normalizeName exists for is about a name being typed at, and none of it
// applies here.
//
// Empty is a refusal rather than "put it back to the branch". Re-deriving is a
// verb nothing has asked for, and it is indistinguishable from an argument that
// was dropped on the way.
func normalizeLabel(requested string) (string, error) {
	label := truncateLabel(cleanLabel(requested))
	if label == "" {
		return "", errors.New(labelNothing)
	}
	return label, nil
}
