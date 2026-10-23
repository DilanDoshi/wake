//go:build darwin || linux

package daemon

import "testing"

// The network watch opens on a platform that has one, and stopping it leaves
// nothing running. No test can make the kernel report a change; the parsers'
// own tests say which messages count.
func TestTheNetworkWatchOpensAndStopsCleanly(t *testing.T) {
	base := settledGoroutines()
	changes, stop := watchRoutes()
	if changes == nil {
		t.Fatal("no network watch on a platform that has one")
	}
	stop()
	waitForGoroutines(t, base)
}
