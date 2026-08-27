//go:build !unix

package daemon

import (
	"context"
	"errors"
)

// idsInUse cannot be answered here, and says so rather than answering no.
//
// The whole point of the check is that resuming an id a second process holds
// branches the transcript with no error anywhere, so a platform that cannot
// look is a platform that cannot wake a session - which is a missing feature,
// and a missing feature is not trusted while a lying one is.
func idsInUse(context.Context, []string) (map[string]bool, error) {
	return nil, errors.New("this platform cannot list processes, so Wake cannot prove a parked session has no process left")
}
