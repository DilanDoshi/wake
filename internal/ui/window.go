package ui

// The window around the frame: SideMargin blank columns either side of it, so no
// text is drawn against the terminal's edge (owner's 2026-11-05 ask). The frame
// and every hit-test in it work in the frame's own columns; this is the one place
// the window's are turned into them.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// marginPad is one row's left margin.
var marginPad = strings.Repeat(" ", SideMargin)

// Margined is the App as the terminal runs it: told the window's size less its
// margins, its mouse moved into the frame, and its frame drawn one margin in.
//
// It holds the App boxed, as App.Update returns it, so wrapping a message's
// result copies three words rather than the whole model.
type Margined struct {
	tea.Model

	// frame is the newest frame width, the window's less both margins: what a
	// mid-drag frame is clipped to, so what a press is bound by.
	frame int
}

// Update moves a window size and a mouse cell into the frame, then hands the
// message on. A press in a margin is the edge cell's, so the window's edge is
// still a target and a drag past it ends on the edge rather than off the frame.
func (m Margined) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.frame = max(v.Width-2*SideMargin, 0)
		v.Width = m.frame
		msg = v
	case tea.MouseMsg:
		v.X = clamp(v.X-SideMargin, 0, max(m.frame-1, 0))
		msg = v
	}
	var cmd tea.Cmd
	m.Model, cmd = m.Model.Update(msg)
	return m, cmd
}

// View is the frame one margin in. Bubble Tea erases a short row to its end, so
// the right margin needs no blanks of its own.
func (m Margined) View() string {
	return marginPad + strings.ReplaceAll(m.Model.View(), "\n", "\n"+marginPad)
}
