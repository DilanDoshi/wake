//go:build !unix

// The half that admits it cannot. Wake is built for macOS and Linux; without
// process groups there is nothing to record at spawn and nothing to signal
// afterwards, and without a way to ask about another process there is no
// liveness probe either. Every answer here is the one that means "unknown",
// which never kills anything and never declares anything dead. A daemon that
// crashes on such a platform leaves its agents running, and says so rather
// than pretending it cleaned up.

package daemon

import (
	"context"
	"errors"
)

var errNoProcess = errors.New("no such process")

func groupLeader(int) bool { return false }

type process struct {
	state string
	argv  string
}

func inspect(context.Context, int) (process, error) {
	return process{}, errors.New("asking the OS about another process is not supported on this platform")
}

func (p process) zombie() bool { return false }
