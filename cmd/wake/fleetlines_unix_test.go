//go:build unix

package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A fleet whose daemon died is read off the roster it left, and that report names
// the fleet too, so `wake attach` points at this fleet's status, not the unnamed one's.
func TestAttachOnAFleetWhoseDaemonDiedNamesIt(t *testing.T) {
	socket := fleetsHome(t, "dev", "dev")
	sessionID := uuid.NewString()
	writeRoster(t, socket, sessionID, startBlockedProcess(t, sessionID))

	if _, _, err := resolveSession(socket, "anyone"); err == nil || !strings.Contains(err.Error(), "check `wake status --fleet dev`") {
		t.Errorf("`wake attach` on fleet dev with its daemon gone failed with %v", err)
	}
}
