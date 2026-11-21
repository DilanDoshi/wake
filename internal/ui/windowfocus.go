package ui

// The terminal window's own focus, as cmd/wake's tea.WithReportFocus (DECSET 1004)
// reports it: a window another application has the keyboard in draws no caret and
// no focus colour. bubbles' cursor hides itself and stops blinking on BlurMsg and
// lights and re-arms on FocusMsg, both reaching the focused composer by update's
// fall-through; this file adds the border, and keeps a pane that takes the keys
// behind a blurred window dark. The zero value is focused, so a terminal that never
// reports is drawn as before, and nothing here writes a frame or starts a timer.

// withWindowFocus records whether the window has the keyboard and tells every
// composer, so each draws its box plain while it does not.
func (a App) withWindowFocus(yes bool) App {
	a.windowBlurred = !yes
	return a.composersFocused()
}

// WindowFocused says whether the window holds the keyboard. A composer in a window
// that does not is drawn plain and its caret held off, so a pane that takes the
// keys behind it does not light up.
func (c Composer) WindowFocused(yes bool) Composer {
	c.windowBlurred = !yes
	if !yes {
		c.ta.Cursor.Blur()
	}
	return c
}
