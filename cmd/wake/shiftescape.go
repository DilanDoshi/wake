package main

// Shift-clicks. A terminal that keeps its own selection on Shift (Ghostty, cmux,
// Kitty) does not deliver a Shift-click to the program unless it is asked to:
// XTSHIFTESCAPE, `CSI > 1 s`. Wake asks once the program is up, so a Shift-click
// on a sidebar agent opens it below the focused pane, and gives the terminal's
// own behaviour back - `CSI > 0 s` - on every way out, since the setting outlives
// the program and would otherwise leave the shell without Shift-drag selection.
// The terminal has no way to read the setting first, so "back" is its default.
// A terminal that does not know the sequence ignores it.
const (
	shiftEscapeOn  = "\x1b[>1s"
	shiftEscapeOff = "\x1b[>0s"
)
