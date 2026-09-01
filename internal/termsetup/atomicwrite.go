package termsetup

// Replacing the operator's real config without a reader ever seeing half of it.
//
// internal/daemon/atomicfile.go does the same CreateTemp -> write -> chmod fd ->
// close -> Rename dance for the files the daemon owns; this is its sibling for
// the one file in this tree that is *not* Wake's own regenerable state. It is a
// separate copy rather than an import because internal/termsetup is a CLI leaf
// and internal/daemon is not a dependency it should pull in for one function.
//
// The one thing the daemon's version never faces and this one must: a terminal
// config is often a **symlink** into a dotfile repo. A bare rename onto the link
// would replace it with a regular file and orphan the real dotfile, so the temp
// and the rename target are the symlink's *resolved* destination - the link is
// preserved and its target is what gets replaced atomically.

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic replaces path's contents with body, or leaves the file
// exactly as it was. A symlinked path is followed so the link survives and its
// target is replaced; a path that does not yet exist is created in place.
func writeFileAtomic(path string, body []byte, perm os.FileMode) error {
	// Follow a symlink to its target so the rename lands on the real file
	// rather than replacing the link. EvalSymlinks fails for a path that does
	// not exist yet (a brand-new config), which is exactly when path is already
	// the place to write.
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".wake-*")
	if err != nil {
		return fmt.Errorf("create temp beside %s: %w", target, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // no-op once the rename succeeds

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", target, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", target, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", target, err)
	}
	if err := os.Rename(name, target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	return nil
}
