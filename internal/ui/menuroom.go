package ui

// How many rows a pane can give the menu block pinned over its composer. The
// pane clips a menu to this from the bottom (DM.menuRows, Room.menuRows); the
// resume picker asks it first so it can draw only what fits and keep the
// session ↵ resumes on screen.

// menuUp stands in for a menu when a pane is measured for one: the chrome asks
// only whether a menu is drawn, since a drawn menu takes the composerGap's row.
const menuUp = " "

// menuRoom is the most rows this pane can give a menu block: what is left after
// the rest of the chrome and one row of transcript, measured with a menu up.
func (d DM) menuRoom() int {
	d.menu = menuUp
	return d.height - d.baseChrome() - minTranscriptHeight
}

// menuRoom is the room's, DM.menuRoom's rule.
func (r Room) menuRoom() int {
	r.menu = menuUp
	return r.height - r.baseChrome() - minTranscriptHeight
}

// menuRoom is what pane id can give its menu block at this size, asked of the
// pane the way transcriptRows asks it for its transcript.
func (a App) menuRoom(id string, width, height int) int {
	if id == "" {
		return a.roomFor().SetSize(width, height).menuRoom()
	}
	return a.dmFor(id).WithCompacting(a.compactingSince(id)).SetSize(width, height).menuRoom()
}
