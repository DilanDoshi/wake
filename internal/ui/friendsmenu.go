package ui

// /friends: who sits on the usage bar. Bare, it opens a menu over the composer
// with the kept friend marked, and the corner previews whichever the cursor is
// on (drawnFriend); ↵ keeps one, esc or typing puts the kept one back. With a
// word it sets that friend at once. The choice is the machine's, kept by
// cmd/wake (decisions.md, 2026-10-27).

import (
	"slices"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/notice"
)

const (
	friendsTitle   = "Friends"
	friendsHint    = "↑↓ move · ↵ choose · esc cancel"
	friendsUnknown = "no friend is called %s · " + friendsVerb + " %s"
	friendsNotKept = "friends: %s sits on the bar, but the choice was not kept: %v"
	noneBlurb      = "an empty corner"
)

func (a App) friends(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	if arg == "" {
		opts := friendOptions()
		a.picker = Picker{Word: friendsCommand, Options: opts, Current: a.corner.pick, Cursor: max(slices.Index(opts, a.corner.pick), 0)}
		return a, nil
	}
	key := strings.ToLower(arg)
	if !slices.Contains(friendOptions(), key) {
		notice.Report(friendsUnknown, arg, friendsList())
		return a, nil
	}
	return a.chooseFriend(key)
}

// friendsAsked numbers choices as they are made and friendsKept keeps them in
// that order (clipboard.go's inOrder): a slow keep finishing after a newer one
// would leave the file on the friend the window no longer shows.
var (
	friendsAsked atomic.Uint64
	friendsKept  inOrder
)

// chooseFriend puts key on the bar and keeps it for the next start.
func (a App) chooseFriend(key string) (App, tea.Cmd) {
	a.corner.pick = key
	save := a.corner.save
	if save == nil {
		return a, nil
	}
	turn := friendsAsked.Add(1)
	return a, func() tea.Msg {
		friendsKept.write(turn, func() {
			if err := save(key); err != nil {
				notice.Report(friendsNotKept, key, err)
			}
		})
		return nil
	}
}

// friendsList is the words /friends takes, as a sentence: "skip, ollie, bo or none".
func friendsList() string {
	opts := friendOptions()
	return strings.Join(opts[:len(opts)-1], ", ") + " or " + opts[len(opts)-1]
}

// friendsView is the /friends menu: a titled box over the composer, like
// /status's, with a row per friend through a card's own option rows.
func (p Picker) friendsView(width int) string {
	w := max(width, minBlockWidth)
	inner := max(w-cardFrameWidth, 1)
	rows := make([]string, len(p.Options))
	for i, key := range p.Options {
		rows[i] = optionRow(friendLabel(key), inner, i == p.Cursor, key == p.Current, AccentStyle)
	}
	body := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(rows, "\n"))
	return titledBox(body, w, BoxStyle.Padding(0), friendsTitle, friendsHint, AccentStyle, HintStyle)
}

// friendLabel is a menu row's words: the friend's name, then its line. Joined
// by factSep rather than padded into a column, since an option row collapses
// runs of spaces.
func friendLabel(key string) string {
	name, blurb := friendNone, noneBlurb
	for _, f := range cast {
		if f.key == key {
			name, blurb = f.name, f.blurb
		}
	}
	return name + factSep + blurb
}
