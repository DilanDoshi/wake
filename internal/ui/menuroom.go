package ui

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
func (d DM) menuRoom() int { return d.height - d.baseChrome() - minTranscriptHeight }

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
