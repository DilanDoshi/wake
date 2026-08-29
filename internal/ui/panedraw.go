package ui

// What App sets on a pane for the draw, and does not keep.
//
// Four facts a conversation needs in order to be drawn and has no business
// holding between frames: the selection (App owns the one selection, and a
// copy here would go stale against it), the menu block above the composer,
// whether an answerable card is in it, and the title its composer wears while
// the draft is an answer rather than a message.
//
// Together in one file because they are one idea - set on the way to View,
// never folded into - and because dm.go had grown to five lines under the
// hard maximum, where the next edit is a build failure rather than a review
// comment. Split by subject, which is the rule; the line count is only what
// made it urgent.

// WithAsk says an answerable card is drawn in this pane, which quiets the
// transcript behind it. See askdim.go for what that costs.
func (d DM) WithAsk(v bool) DM {
	d.behindAsk = v
	return d
}

// WithWriting titles the composer for a draft that is an answer rather than a
// message. "" leaves the pane's own name in place.
func (d DM) WithWriting(t string) DM {
	d.writing = t
	return d
}

// WithSelection is the selection this pane draws.
func (d DM) WithSelection(m marked) DM {
	d.sel = m
	return d
}

// WithComposerSelection is the query-box selection this pane draws.
func (d DM) WithComposerSelection(m marked) DM {
	d.csel = m
	return d
}

// WithMenu is the menu block this pane draws. See DM.menu.
func (d DM) WithMenu(menu string) DM {
	d.menu = menu
	return d
}
