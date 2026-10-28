package main

// The friend on the usage bar, from this side: which one the machine keeps, in
// ~/.wake/friends.json beside update-check.json (the owner's 2026-10-27 ruling),
// and whether this terminal draws Kitty images. The UI draws it and asks for a
// choice to be kept; see internal/ui/friends.go.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/termsetup"
	"github.com/DilanDoshi/wake/internal/ui"
)

const (
	friendsFile     = "friends.json"
	friendsDirPerm  = 0o700
	friendsFilePerm = 0o600

	// friendsFileEnv keeps the pick somewhere else - a test, a demo take - and
	// friendArtEnv=blocks draws the half-block sprite in any terminal.
	friendsFileEnv  = "WAKE_FRIENDS_FILE"
	friendArtEnv    = "WAKE_FRIEND_ART"
	friendArtBlocks = "blocks"
)

// keptFriend is the file's one field.
type keptFriend struct {
	Friend string `json:"friend"`
}

// friendsFor is the corner a window opens with. Read once, here: a choice made
// in another window reaches this one at its next start.
func friendsFor(env map[string]string) ui.Friends {
	f := ui.Friends{Kitty: drawsKittyImages(env)}
	path, err := friendsPath(env)
	if err != nil {
		f.Save = func(string) error { return err }
		return f
	}
	f.Pick = loadFriend(path)
	f.Save = func(key string) error { return keepFriend(path, key) }
	return f
}

// embeddedTerminals are what a terminal inside another program sets - zellij,
// an editor's terminal - which draw no Kitty images whatever the terminal around
// them does. termsetup knows tmux and screen; these are the rest.
var embeddedTerminals = []string{"ZELLIJ", "NVIM", "VIM_TERMINAL"}

// drawsKittyImages is whether this terminal draws a friend as Kitty images: TERM
// names Kitty or Ghostty (so it holds over ssh too), or cmux, which draws through
// Ghostty and keeps TERM at xterm-256color. A Kitty or Ghostty variable alone is
// not enough: it leaks into every program and terminal started from that shell.
func drawsKittyImages(env map[string]string) bool {
	if env[friendArtEnv] == friendArtBlocks {
		return false
	}
	mux, inMux := termsetup.DetectMultiplexer(env)
	if inMux && mux != termsetup.Cmux {
		return false
	}
	for _, k := range embeddedTerminals {
		if env[k] != "" {
			return false
		}
	}
	switch env["TERM"] {
	case "xterm-kitty", "xterm-ghostty":
		return true
	}
	return mux == termsetup.Cmux && termsetup.Detect(env) == termsetup.Ghostty
}

func friendsPath(env map[string]string) (string, error) {
	if p := env[friendsFileEnv]; p != "" {
		return p, nil
	}
	root, err := daemon.StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, friendsFile), nil
}

// loadFriend is the kept friend, or "" for none kept or a file that cannot be
// read - which the UI opens on its default.
func loadFriend(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var kept keptFriend
	if err := json.Unmarshal(b, &kept); err != nil {
		return ""
	}
	return kept.Friend
}

func keepFriend(path, key string) error {
	b, err := json.Marshal(keptFriend{Friend: key})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), friendsDirPerm); err != nil {
		return err
	}
	// Whole or not at all: another window may be opening on it right now.
	return daemon.WriteFileAtomically(path, "the kept friend", b, friendsFilePerm)
}
