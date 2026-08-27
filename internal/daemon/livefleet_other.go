//go:build !unix

// The half that admits it cannot look, for reap_other.go's reason: an answer of
// "unknown" never declares anything dead, and a platform with no way to ask
// loses the liveness probe rather than gaining a fleet it believes is gone.

package daemon

import (
	"context"
	"errors"
)

func processTable(context.Context) (map[int]process, error) {
	return nil, errors.New("this platform cannot list processes, so Wake cannot tell an agent that lost its process from one that is merely quiet")
}
