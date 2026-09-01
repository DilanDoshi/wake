package termsetup

// The first-run prompt's memory: has the room already offered this fix once?
//
// A marker file rather than a JSON prefs blob, because there is exactly one
// bit to remember. It lives under configHome/wake - a UI preference about
// what has already been shown, not fleet or session state, so it does not
// belong beside parked.json under the daemon's own directory (CLAUDE.md:
// "Wake owns almost no state... Wake stores only roster, park book, groups,
// layout"). It survives a reinstall the way a browser's "don't show this
// again" does, which is the point: the offer is made once per machine, not
// once per fleet.

import (
	"os"
	"path/filepath"
)

const (
	wakePrefsDirName   = "wake"
	promptedMarkerName = "terminal-setup-prompted"
)

// PromptSeen is whether the first-run terminal-setup notice has already been
// shown, ever, on this machine.
func PromptSeen(configHome string) bool {
	_, err := os.Stat(promptMarkerPath(configHome))
	return err == nil
}

// MarkPromptSeen records that it has. Safe to call more than once.
func MarkPromptSeen(configHome string) error {
	path := promptMarkerPath(configHome)
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, configPerm)
	if err != nil {
		return err
	}
	return f.Close()
}

func promptMarkerPath(configHome string) string {
	return filepath.Join(configHome, wakePrefsDirName, promptedMarkerName)
}
