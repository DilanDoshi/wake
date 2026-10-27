package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

)

// Kitty images only where the terminal is known to draw them: TERM names Kitty
// or Ghostty (as it does over ssh too), or cmux, which draws through Ghostty and
// keeps TERM at xterm-256color. A variable leaked from an outer terminal - into
// a multiplexer, an editor's terminal, another terminal started from its shell -
// is not that terminal, so those get the half-block sprite.
func TestOnlyATerminalThatDrawsKittyImagesIsSentThem(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"ghostty", map[string]string{"TERM": "xterm-ghostty", "TERM_PROGRAM": "ghostty"}, true},
		{"cmux", map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "xterm-256color", "CMUX_PANEL_ID": "1"}, true},
		{"kitty", map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "3"}, true},
		{"kitty over ssh", map[string]string{"TERM": "xterm-kitty"}, true},
		{"ghostty told xterm-256color", map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "xterm-256color"}, false},
		{"alacritty from kitty's shell", map[string]string{"TERM": "alacritty", "KITTY_WINDOW_ID": "3"}, false},
		{"tmux in kitty", map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/t,1,0", "KITTY_WINDOW_ID": "3"}, false},
		{"screen in ghostty", map[string]string{"TERM": "xterm-ghostty", "TERM_PROGRAM": "ghostty", "STY": "1.pts"}, false},
		{"zellij in ghostty", map[string]string{"TERM": "xterm-ghostty", "TERM_PROGRAM": "ghostty", "ZELLIJ": "0"}, false},
		{"neovim's terminal in cmux", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "ghostty", "CMUX_PANEL_ID": "1", "NVIM": "/tmp/nvim.1/0"}, false},
		{"vim's terminal in kitty", map[string]string{"TERM": "xterm-kitty", "VIM_TERMINAL": "900"}, false},
		{"iterm2", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM": "xterm-256color"}, false},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm", "TERM": "xterm-256color"}, false},
		{"vscode", map[string]string{"TERM_PROGRAM": "vscode", "TERM": "xterm-256color"}, false},
		{"apple terminal", map[string]string{"TERM_PROGRAM": "Apple_Terminal", "TERM": "xterm-256color"}, false},
		{"unknown", map[string]string{"TERM": "xterm-256color"}, false},
		{"ghostty told blocks", map[string]string{"TERM": "xterm-ghostty", friendArtEnv: friendArtBlocks}, false},
		{"kitty told blocks", map[string]string{"TERM": "xterm-kitty", friendArtEnv: friendArtBlocks}, false},
	} {
		if got := drawsKittyImages(c.env); got != c.want {
			t.Errorf("%s: images = %v, want %v", c.name, got, c.want)
		}
	}
}

// The pick is kept across a restart, and anything that is not a kept pick -
// no file, a broken one, a file another build wrote - opens on the default.
func TestAKeptFriendSurvivesARestartAndAnythingElseReadsAsNone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", friendsFile)
	if got := loadFriend(path); got != "" {
		t.Errorf("no file read as %q", got)
	}
	if err := keepFriend(path, "bo"); err != nil {
		t.Fatal(err)
	}
	if got := loadFriend(path); got != "bo" {
		t.Errorf("a kept bo read back as %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != friendsFilePerm {
		t.Errorf("the file is %v, want %v", perm, os.FileMode(friendsFilePerm))
	}
	if err := os.WriteFile(path, []byte("{not json"), friendsFilePerm); err != nil {
		t.Fatal(err)
	}
	if got := loadFriend(path); got != "" {
		t.Errorf("a broken file read as %q", got)
	}
}

// The file is the machine's, under ~/.wake, unless WAKE_FRIENDS_FILE names
// another - which is how this suite keeps every run off the owner's own pick.
func TestTheKeptFriendIsTheMachinesUnlessTheEnvironmentNamesAnother(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, err := friendsPath(map[string]string{}); err != nil || got != filepath.Join(home, ".wake", friendsFile) {
		t.Errorf("with no override the pick is kept at %q (%v), want ~/.wake/%s", got, err, friendsFile)
	}
	other := filepath.Join(t.TempDir(), "mine.json")
	if got, err := friendsPath(map[string]string{friendsFileEnv: other}); err != nil || got != other {
		t.Errorf("WAKE_FRIENDS_FILE=%s kept it at %q (%v)", other, got, err)
	}
}

// A window opens on what the file keeps, and a choice it makes lands in the file.
func TestAWindowOpensOnTheKeptFriendAndKeepsItsChoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), friendsFile)
	if err := keepFriend(path, "ollie"); err != nil {
		t.Fatal(err)
	}
	f := friendsFor(map[string]string{friendsFileEnv: path, "TERM": "xterm-ghostty"})
	if f.Pick != "ollie" || !f.Kitty || f.Save == nil {
		t.Fatalf("the window opens with %+v, want ollie as images and a way to keep a choice", f)
	}
	if err := f.Save("bo"); err != nil {
		t.Fatal(err)
	}
	if got := loadFriend(path); got != "bo" {
		t.Errorf("the choice kept %q, want bo", got)
	}
}

// Two windows choosing while a third opens: every read sees a whole choice,
// never a file caught between its truncate and its write.
func TestAWindowOpeningWhileAnotherKeepsAChoiceReadsAWholeOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), friendsFile)
	if err := keepFriend(path, "skip"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for _, key := range []string{"bo", "ollie"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := keepFriend(path, key); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := loadFriend(path); got != "skip" && got != "bo" && got != "ollie" {
			t.Errorf("a window opened on %q while choices were kept", got)
			break
		}
	}
	close(stop)
	wg.Wait()
}
