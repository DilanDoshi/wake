// Replacing a file this daemon owns, without a reader ever seeing half of it.
//
// # Why this is one function and not three
//
// It was three: `roster.go`, `parkbook.go` and `manager.go` each held the same
// CreateTemp → defer Remove → Write → chmod → Close → Rename sequence, and by
// the third one it had **already drifted** — that copy chmod'd the *path* after
// closing rather than the *fd* before it, which is a TOCTOU on a path in a
// directory the write does not otherwise depend on. Harmless where it stood, and
// exactly the drift CLAUDE.md's no-parallel-implementations rule predicts: three
// copies is where a fourth reader has to work out which of them is right.
//
// The `what` argument is the only thing that differed besides the bytes, and it
// is there because the error is read by a person: *"replace the park book"* and
// *"replace the manager's MCP config"* are different diagnoses, and a shared
// helper that reported "replace file" would have taken that away.
//
// # What each step is for
//
// The **temp file in the destination's own directory** is what makes the rename
// atomic — rename(2) is only atomic within a filesystem, and a temp in $TMPDIR
// may be on another one. The **deferred Remove** is a no-op once the rename has
// succeeded and is what stops a failed write leaving debris beside a file the
// next daemon reads. The **chmod on the fd** rather than on the path is the
// half that had drifted: it names the file this call created and nothing else,
// so it cannot be redirected by anything that appears at that path in between.
// And the **Close before the Rename** is not tidiness: a reader that sees the
// new name must see all of the bytes.
//
// The two **syncs** are for a reader after a power cut or a force restart,
// which is what reads these files back (docs/notes/bugs.md BUG-53): the file's
// before the rename, so the name never points at bytes still in a cache, and
// the directory's after it, so the rename itself survives. On macOS File.Sync
// is F_FULLFSYNC, a few milliseconds a write (measured 2026-10-22) on files
// written when a session changes, never per frame.

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomically replaces path with body, or leaves it exactly as it was.
// Exported for cmd/wake's friends.json, which windows read while another writes.
//
// what names the thing being written, for the error a person reads. perm is the
// mode the file ends up with, set on the descriptor before anything can open it
// by name.
func WriteFileAtomically(path, what string, body []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create %s temp file: %w", what, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // no-op once the rename succeeds

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", what, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", what, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", what, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", what, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %s %s: %w", what, path, err)
	}
	syncDir(filepath.Dir(path), what)
	return nil
}

// removeFile deletes path durably: a removal a power cut undid would bring back
// a file the next daemon reads. A missing file is the ordinary case, not an
// error.
func removeFile(path, what string) error {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove %s %s: %w", what, path, err)
	}
	syncDir(filepath.Dir(path), what)
	return nil
}

// syncDir makes a rename or a removal in dir durable. A failure is said rather
// than returned: the change is in place and readable, and only whether a power
// cut keeps it is in doubt - a caller told the write failed would retry or warn
// for nothing.
func syncDir(dir, what string) {
	d, err := os.Open(dir)
	if err == nil {
		err = d.Sync()
		_ = d.Close()
	}
	if err != nil {
		logf("wake: the %s was replaced but its directory could not be synced, so a power cut may undo it: %v", what, err)
	}
}
