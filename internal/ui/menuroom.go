package ui

import "github.com/charmbracelet/lipgloss"

// How many rows a pane can give the menu block pinned over its composer. The
// pane clips a menu to this from the bottom (DM.menuRows, Room.menuRows); the
// resume picker asks it first so it can draw only what fits and keep the
// session ↵ resumes on screen.

// menuUp stands in for a menu when a pane is measured for one. What the chrome
// asks is only whether a menu is drawn - a drawn menu takes the composerGap's
// row, and SetSize caps a streamed preview differently under one - so it is set
// before the pane is sized, the way the draw sets the real menu.
const menuUp = " "

// menuRoom is the most rows this pane can give a menu block: what is left after
// the rest of the chrome and one row of transcript.
func (d DM) menuRoom() int { return d.menuRoomAt(d.composerRows()) }

// menuRoomAt is menuRoom for a composer already measured at box rows: the box is
// what costs, so a caller that needs more than one answer from it measures once.
func (d DM) menuRoomAt(box int) int { return d.height - d.baseChromeWith(box) - minTranscriptHeight }

// composerRows is the box as the chrome counts it: the draft's rows, without the
// bar that rides inside it.
func (d DM) composerRows() int {
	return lipgloss.Height(d.composer.View(max(d.width, minComposerWidth)))
}

// baseChromeWith is baseChrome for a composer measured at box rows.
func (d DM) baseChromeWith(box int) int {
	return box + d.aboveComposerExtra() + d.checklistRows() + d.queuedRows()
}

// menuRoom is the room's, DM.menuRoom's rule.
func (r Room) menuRoom() int { return r.height - r.baseChrome() - minTranscriptHeight }

// menuRoom is what pane id can give its menu block at this size, asked of the
// pane the way transcriptRows asks it for its transcript.
func (a App) menuRoom(id string, width, height int) int {
	if id == "" {
		return a.roomFor().WithMenu(menuUp).SetSize(width, height).menuRoom()
	}
	return a.dmFor(id).WithMenu(menuUp).WithCompacting(a.compactingSince(id)).SetSize(width, height).menuRoom()
}
