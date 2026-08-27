//go:build !unix

// The half that admits it cannot, matching reap_other.go. Without an advisory
// lock nothing can prove another daemon is not running, so the daemon starts -
// refusing would be worse, since the way to reach a fleet is to start - and
// never reaps. That is the same answer every lookup gives on this platform:
// unknown, which never kills anything.

package daemon

import "errors"

type lockfile struct {
	path      string
	exclusive bool
	why       error
}

func takeLock(path string) (*lockfile, error) {
	return &lockfile{
		path: path,
		why:  errors.New("this platform has no advisory file lock, so nothing can prove another daemon is not holding the fleet"),
	}, nil
}

func (l *lockfile) release() error { return nil }

// verify has nothing to defend: without an advisory lock this platform never
// took one, so there is no inode to re-check and nothing reaps here anyway.
func (l *lockfile) verify() {}
