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

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomically replaces path with body, or leaves it exactly as it was.
//
// what names the thing being written, for the error a person reads. perm is the
// mode the file ends up with, set on the descriptor before anything can open it
// by name.
func writeFileAtomically(path, what string, body []byte, perm os.FileMode) error {
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", what, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %s %s: %w", what, path, err)
	}
	return nil
}
