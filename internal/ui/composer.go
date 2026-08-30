package ui

// The input box: the text area, the target line above it, and the box itself.
// The legend under it is legend.go's.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// spawnedMode is the permission mode every session this build *starts* in, and
// it is a fallback rather than a belief: what an agent is in now lives in
// App.modes, keyed by session, and arrives from a receipt or from any turn's
// init. This is what to show before either has been seen.
//
// It duplicates internal/daemon's spawnPermissionMode, which is the value that
// actually reaches the command line. internal/ui may not import the daemon -
// the UI never touches a process - so the boundary is paid for with this
// constant and this comment: change one and change the other, or the indicator
// starts lying again in a quieter way. The spelling comes from core either way,
// because the word is Claude's.
const spawnedMode = core.PermissionModeAuto

const (
	// composerHeight is what an *empty* box costs. It grows a row per wrapped
	// row of the draft from there - see Composer.fit - so a pane spends nothing
	// on a box nobody is typing in and the rows only ever go where the typing
	// is.
	composerHeight = 1

	// boxFrameWidth is the border BoxStyle spends on the left and right.
	boxFrameWidth = 2

	// boxBorderRows is the top and bottom edge of the box, which is what the
	// draft's own rows are drawn between.
	boxBorderRows = 2

	// maxComposerRows is the tallest the box grows however long the draft is.
	// Past it the draft scrolls under the cursor rather than taking more of the
	// pane - and a pane too short even for this bounds it further, so the
	// transcript always keeps minTranscriptHeight.
	maxComposerRows = 10

	// textAreaFrameWidth is the border plus BoxStyle's side padding, which is
	// what the text area inside it does not get to use.
	textAreaFrameWidth = 4

	// minComposerWidth is the narrowest the composer draws. Below it the box
	// has no room left for the prompt and a character, so View floors the
	// width instead of shrinking further — which means a caller budgeting
	// fewer columns than this gets a block wider than it asked for.
	minComposerWidth = 8

	// titleInset is the run of border kept between the pane's name and the
	// top-right corner, and titleMinBorder is the least border a titled edge
	// keeps in total - so a long name is cut rather than eating its own frame.
	titleInset     = 1
	titleMinBorder = 6
)

// The target line: where ↵ will send, and what that costs. §7 makes the cost
// visible because a broadcast is N full turns, and a person who can see "· 30
// turns" before pressing enter is a person who can decide not to.
const (
	// targetFormat is a target that is not an agent - which today is only the
	// absence of one.
	targetFormat = "→ %s"

	// broadcastFormat is @all, with the number of turns it is about to start.
	broadcastFormat = "→ %s%s · %d turns"

	// targetResolved is one agent: the prefix and the name, exactly as it was
	// typed, so a mention that resolved and a mention that did not look
	// different before ↵ rather than after it.
	targetResolved = "→ %s%s"

	// directMention is a mention under the cheap reading, and it says which
	// reading that is even though it is the default.
	//
	// **Absence would be the memory problem §7 exists to close.** If only the
	// expensive mode annotated the line, `→ @alex` would mean direct by having
	// nothing said about it, and an operator would have to remember that the
	// build has a mode at all in order to read a line that is silent about it.
	// This project has been here: a legend that says nothing about a key is
	// the failure the legend rule was written for, one surface over.
	//
	// The word comes from MentionMode.String rather than being spelled here,
	// so the composer and the notice row that reports a flip cannot end up
	// calling the same mode two things.
	directMention = "→ %s%s · %s"

	// openMention is the same mention under the expensive reading, with the
	// count of turns it starts - broadcastFormat's unit, because it is
	// broadcastFormat's cost.
	//
	// §7 draws this line as `→ @alex · 19 also see this`. The count here is
	// the whole fleet rather than the bystanders, and it is stated in turns,
	// because "what does ↵ cost" is the question the target line exists to
	// answer and `@all · 20 turns` is the answer it already gives one key
	// over. Two units on one line would make the expensive route the one that
	// reads cheaper.
	openMention = "→ %s%s · %s · %d turns"

	// noTarget is a draft addressed to nobody. It names the two things that
	// would address it rather than saying "no target", because the reader's
	// question at that moment is what to type.
	noTarget = agentPrefix + "name or " + agentPrefix + core.BroadcastName
)

// targetLine is where ↵ will send this draft, and what it costs.
//
// The mention arms sit above the plain resolved one rather than replacing it:
// `→ @manager` is a resolved route that mention mode does not decide, and so
// is the draft nobody addressed, which resolves to the manager without a
// mention. Annotating those with a mode would be the composer claiming a
// choice was made where none was offered.
func targetLine(r roomRoute, turns int) string {
	switch {
	case r.Broadcast:
		return fmt.Sprintf(broadcastFormat, agentPrefix, r.Resolved, turns)
	case r.mentioned && r.mode == MentionOpen:
		return fmt.Sprintf(openMention, agentPrefix, r.Resolved, r.mode, turns)
	case r.mentioned:
		return fmt.Sprintf(directMention, agentPrefix, r.Resolved, r.mode)
	case r.Resolved != "":
		return fmt.Sprintf(targetResolved, agentPrefix, r.Resolved)
	default:
		return fmt.Sprintf(targetFormat, noTarget)
	}
}

// Composer is the input box: text area, permission mode, key hints.
//
// Its methods take value receivers and return a new Composer. That copies the
// mode, which is a value - and it does NOT copy the draft, which is not.
// textarea.Model holds its buffer behind pointers, so two Composers share one
// text area: type into a copy and the original reads the same characters.
// Measured against bubbles v1.0.0, and pinned by
// TestTwoComposersShareOneTextArea.
//
// So hold the Composer the DM hands back rather than stashing an older copy and
// expecting it to keep the draft it had. dm.go's Composer accessor says the
// same thing from the other side.
//
// Reset is the exception and not by accident: it replaces the text area's value
// rather than appending into it, so clearing a copy leaves the original's draft
// intact. TestResetReturnsANewComposer pins that.
type Composer struct {
	ta textarea.Model
	// mode is the permission mode the legend names, set by whoever draws this
	// composer rather than held as a belief here - see WithMode.
	mode string

	// target is the line above the legend: where ↵ will send what is being
	// typed. Empty draws no line at all, which is how a DM's composer is
	// spelled - a DM has exactly one recipient and its own header names them,
	// so a target line there would be a row spent restating the pane.
	target string

	// title is the pane's own name, set into the top edge of the box the way
	// Claude Code labels its own input. Empty draws an unlabelled border.
	title string

	// bar is the pre-rendered info line drawn between the target and the legend
	// - where this session is, what it runs as, how full it is. The composer
	// only places it: the pane builds it, because it reads the filesystem and a
	// draw-loop read is what geometry.go exists to keep off this path. Empty
	// draws no line, like target. Already styled and width-clipped by statusBar,
	// so it is placed verbatim rather than re-measured here.
	bar string

	// color is the identity hue /color gave this composer's agent, drawn into
	// the border and the @name title so the box you type into is told apart by
	// more than the name text. Set by the DM pane, which is one agent; empty for
	// the room and for an uncoloured agent, both of which keep the accent.
	color string

	// taWidth is the width handed to the text area, kept because measuring the
	// draft needs a second text area laid out identically and Model.Width
	// reports the content width rather than the one SetWidth was given.
	taWidth int

	// maxRows bounds how tall the box grows, set by the pane that draws it.
	// Zero means nobody has said, which reads as maxComposerRows - a composer
	// built by a test or a future caller is bounded rather than unbounded.
	maxRows int

	// hist is where a prompt-history walk has got to, and the draft it started
	// from. Per composer because each pane walks its own history. See prompts.go.
	hist promptWalk

	// arms is which of App's arms this legend names instead of the ordinary
	// labels. Set at draw time by the pane, exactly as mode is and for the same
	// reason: an arm is a fact about App and may not have a second home.
	arms legendArms

	// blurred is a composer that does not have the keys, drawn in the ordinary
	// border rather than the accent.
	//
	// False by default, so a composer nobody has said anything about is the
	// marked one: a room with nothing beside it is the only place to type, and
	// a single unmarked box would lose the accent in the common case. It only
	// starts meaning something once there are two on screen - which is what
	// this field is for, since one accent among two answers "where do I type"
	// wrongly half the time. See App.withFocus.
	blurred bool

	// attachments are images dropped into this draft, each drawn in the box as
	// an `[Image #id]` chip. The chip is the only tie between the draft text and
	// the bytes: an attachment sends only while its chip survives in the draft
	// (Images), so deleting the chip drops the image. See composerimage.go.
	attachments []imageAttachment

	// nextImageID is the id the next dropped image gets, monotonic within a
	// draft so a chip's number never reuses one a deleted chip had. Reset clears
	// it with the rest.
	nextImageID int
}

// NewComposer returns an empty, focused composer showing the mode sessions are
// actually spawned with.
func NewComposer() Composer {
	ta := textarea.New()
	ta.Placeholder = ""
	ta.Prompt = "> "
	ta.ShowLineNumbers = false
	ta.SetHeight(composerHeight)
	// A newline is ⌥↵ or ⌃J, and **not** ↵ - which is what the text area binds
	// by default, along with ⌃M, the same byte. App.key takes ↵ for *send*
	// before the composer ever sees it, so out of the box this binding was two
	// spellings of a key that could not arrive: the box was multi-line and
	// nothing could put a second line in it.
	//
	// ⇧↵ is not among these because it cannot be: keyprobe_test.go has it
	// producing no KeyMsg under either keyboard protocol, and a terminal with
	// no protocol sends it as the byte for ↵, which is send. **Claude Code does
	// not bind it either** - `/terminal-setup` configures the *terminal* to emit
	// ESC CR for the chord, and bubbletea names that `alt+enter`. So ⇧↵ works
	// here exactly when it works there, through the same terminal setup, and ⌃J
	// is the fallback that needs none.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter", "ctrl+j"),
		key.WithHelp("alt+enter", "newline"),
	)
	// Focus makes the cursor visible and returns the blink command. Discarded
	// here: a composer has no Init to run it in, and one that is not the pane
	// with the keys must not blink. App.Init starts the focused composer's blink
	// and App.refocusBlink restarts it whenever the keys arrive - see Refocus.
	_ = ta.Focus()
	return Composer{ta: ta, mode: spawnedMode}
}

// Refocus makes the cursor visible again and restarts its blink, returning the
// command that drives it. The pane taking the keys calls this through
// App.refocusBlink: bubbles keys every blink message to one cursor's id, so a
// focus change routes the previous pane's blink to a cursor that rejects it and
// the loop dies - leaving this caret frozen off half the time, invisible until a
// keystroke moves the cursor and textarea.Update revives it.
//
// The command is computed before the return so the mutated text area is what is
// copied back: `return c, c.ta.Focus()` would copy c first and blink a stale one.
func (c Composer) Refocus() (Composer, tea.Cmd) {
	cmd := c.ta.Focus()
	return c, cmd
}

// Update handles one message: everything goes to the text area.
//
// Nothing is intercepted here, and ⇧⇥ is the reason that rule is written down.
// It used to be caught here to cycle c.mode, which moved a label and reached no
// agent. It is App.key's now and it writes a frame; the label moves when the
// daemon answers. The text area binds neither it nor ⇥, so the App takes both
// without shadowing anything.
func (c Composer) Update(msg tea.Msg) (Composer, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.Type == tea.KeyRunes && !k.Alt {
		// A run of typed characters arrives as ONE KeyMsg whose String() is the
		// characters themselves, and bubbles matches its keymap on that string.
		// So the word "up" is tea.KeyUp as far as the text area is concerned,
		// "end" is tea.KeyEnd, "delete" deletes - and typing
		// "clean up and shut down ... delete space" left "  space" in the box.
		// Inserted here instead, where a rune is a rune.
		//
		// Alt is excluded because alt+f and friends are real bindings, and a
		// single rune is included because one formula cannot drift from two.
		c.ta.InsertString(string(k.Runes))
		return c.fit().reposition(), nil
	}
	// Grown to the bound *before* the update, then fitted back down after.
	//
	// bubbles repositions its view at the end of every Update, against the
	// height the box has at that moment - which is the height it had before the
	// key that just added a row. It scrolls down to keep the cursor in that
	// smaller box and never scrolls back up when the box grows, so a newline
	// left the draft showing from its second line for good. Standing at the
	// bound while the update runs means there is nothing to scroll until the
	// draft genuinely exceeds it.
	c.ta.SetHeight(c.bound())
	var cmd tea.Cmd
	c.ta, cmd = c.ta.Update(msg)
	return c.fit().reposition(), cmd
}

// reposition puts the text area's view back over its cursor.
//
// bubbles does this at the end of every Update, and two things here mean that
// is not enough. InsertString is the path typed runes take and it is not an
// Update at all, so nothing repositions while somebody types. And an Update
// that *does* run repositions against the height the box had **before** fit
// grew it - which leaves the view scrolled down over a box that no longer needs
// scrolling, showing the draft from its second line.
//
// So it is run after the fit, from one seam, for both. The message matches
// nothing in the type switch; repositionView is unconditional at the end of it.
//
// **The reposition runs at the bound, not at the fitted height.** fit sizes the
// box to the draft's *content* rows, but a cursor sitting at the end of a row
// the draft exactly filled is on the phantom next wrapped row - one past the
// content. Repositioning against the fitted height then reads that phantom row
// as below the box and scrolls the first line off to reach it, and because the
// cursor then sits at the top of the scrolled view nothing ever scrolls back:
// the first line was gone for good, intermittently, whenever a soft-wrap landed
// exactly on the edge. Standing at the bound while repositionView runs keeps the
// phantom row inside the box for any draft that fits, so it never scrolls; the
// height is restored after, so the box still draws at its fitted size. This is
// the grow-to-bound the typed-rune path skips before Update, moved to the one
// seam both paths share. Above the bound - the cap - the tail still wins, which
// is what keeps what is being typed on screen.
//
// **The render first is not redundant.** bubbles scrolls its viewport through
// ScrollDown, which returns without doing anything while the viewport holds no
// lines - and the lines are only ever set by rendering. So a reposition with
// nothing rendered since the last keystroke is silently a no-op, and the draft
// stays pinned to its first row however far the cursor has gone. It is one
// render per keystroke, which is work per change; View renders it again per
// frame either way.
func (c Composer) reposition() Composer {
	fitted := c.ta.Height()
	c.ta.SetHeight(c.bound())
	_ = c.ta.View()
	c.ta, _ = c.ta.Update(nil)
	c.ta.SetHeight(fitted)
	return c
}

// View renders the box and the hint line beneath it at the given pane width.
//
// Every line of the result fits within width — the hint is truncated rather
// than allowed to run at its natural length. The grid joins panes on their
// widest line, so one over-wide hint would shove its neighbours out of place.
func (c Composer) View(width int) string {
	width = max(width, minComposerWidth)
	c = c.SetWidth(width)
	rows := []string{c.box(width)}
	if c.target != "" {
		rows = append(rows, " "+AccentStyle.MaxWidth(width-hintIndentWidth).Render(c.target))
	}
	// The info bar above the legend: what this session is, over what a key does.
	// Placed flush-left and verbatim - statusBar already truncated it to width,
	// so it fits, and it carries its own SGR that a second measure would cut.
	if c.bar != "" {
		rows = append(rows, c.bar)
	}
	// The mode is withheld from a blurred pane: it is the one legend entry that
	// reads as a claim about the pane rather than about the keys, and the keys
	// are somewhere else. See modeFormat.
	mode := c.mode
	if c.blurred {
		mode = ""
	}
	hint := HintStyle.Render(hintFitting(mode, width-hintIndentWidth, c.arms))
	return strings.Join(append(rows, " "+hint), "\n")
}

// box is the bordered input with the pane's name set into its top edge, the
// way Claude Code labels its own.
//
// The top line is drawn here rather than by lipgloss because a border cannot
// carry a label: the style renders the sides and the bottom, and this renders
// the top as three pieces so the name keeps its own colour. Right-aligned, one
// column in from the corner, and dropped entirely when the box is too narrow to
// hold it with a segment of border either side - a title that eats its own
// frame is worse than no title.
func (c Composer) box(width int) string {
	body := c.boxStyle().BorderTop(false).Width(width - boxFrameWidth).Render(c.ta.View())
	b := lipgloss.RoundedBorder()
	edge := width - boxFrameWidth // the run of border between the two corners

	// Right-aligned, one column in from the corner: the lead is whatever is
	// left once the label and that inset are taken. See titledEdge, which the
	// card's own frame draws both of its edges through.
	rule := lipgloss.NewStyle().Foreground(c.boxStyle().GetBorderTopForeground())
	lead := edge - titleInset
	if c.title != "" && edge > titleMinBorder {
		lead = edge - lipgloss.Width(" "+ansi.Truncate(c.title, edge-titleMinBorder, ellipsis)+" ") - titleInset
	}
	top := titledEdge(b.TopLeft, b.Top, b.TopRight, c.title, edge, lead, rule, c.titleStyle())
	return lipgloss.JoinVertical(lipgloss.Left, top, body)
}

// titleStyle draws the pane's @name in the top edge: its agent's identity hue
// when /color gave it one, headerStyle's accent otherwise. Bold either way, the
// way Claude Code labels its own input. Unlike the border below, it is not
// dropped on blur - the title has always been drawn bold whether the pane holds
// the keys or not, so a blurred coloured pane still names its agent in colour.
func (c Composer) titleStyle() lipgloss.Style {
	if style, ok := identityStyle(c.color); ok {
		return style.Bold(true)
	}
	return headerStyle
}

// SetWidth lays the draft out for a pane this wide.
//
// It must be called when the pane is sized and not only when it is drawn. The
// text area wraps the draft and scrolls its single row as keystrokes arrive, so
// a width set on the copy inside View leaves the model that takes the keys
// wrapping at bubbles' default 40 columns - with its one-row viewport scrolled
// to a wrapped row the real width does not have, which draws an empty box over
// a draft that is still there. That is what the first person to run this build
// hit, and no test in the tree could see it because none rendered a screen.
func (c Composer) SetWidth(width int) Composer {
	c.taWidth = max(width, minComposerWidth) - textAreaFrameWidth
	c.ta.SetWidth(c.taWidth)
	// The wrap moved, so the row count did. Re-fitting here rather than in View
	// keeps it work per change: this is called when a pane is sized, and View
	// is called per frame.
	return c.fit()
}

// composerRowsIn is how many rows a pane this tall can lend the draft: what is
// left after the transcript's floor and the chrome the draft does not own,
// never more than maxComposerRows and never less than one.
//
// **The pane decides, not the composer.** A box that grew to its own cap in a
// pane too short for it would make the frame taller than the terminal, and a
// frame one row too tall scrolls the alt screen away on every draw - which is
// this project's most expensive rendering failure and the reason DM.chrome
// exists.
func composerRowsIn(height, overhead int) int {
	return min(max(height-minTranscriptHeight-overhead, 1), maxComposerRows)
}

// WithMaxRows bounds how tall the box may grow and re-fits it. The pane owns
// this number because only the pane knows what the transcript can spare.
func (c Composer) WithMaxRows(n int) Composer {
	c.maxRows = max(n, 1)
	return c.fit()
}

// overhead is the rows the composer spends on everything that is not the
// draft, which is what a pane subtracts before deciding what it can lend.
func (c Composer) overhead() int {
	rows := boxBorderRows + 1 // the box's two edges, and the hint line
	if c.target != "" {
		rows++
	}
	return rows
}

// fit sizes the box to the draft: one row per wrapped row, up to the bound.
//
// Called when the draft or the pane changes and never per frame - a box that
// re-measured itself on every draw would be work per frame that is already
// work per change.
func (c Composer) fit() Composer {
	bound := c.bound()
	c.ta.SetHeight(min(c.draftRows(bound), bound))
	return c
}

// bound is the tallest this box may be drawn: what its pane said it could
// lend, or the cap when nobody has said.
func (c Composer) bound() int {
	if c.maxRows <= 0 {
		return maxComposerRows
	}
	return c.maxRows
}

// draftRows is how many rows the wrapped draft needs, up to bound.
//
// **Measured by rendering, not by re-wrapping.** bubbles' word wrap has its own
// rules for spaces and double-width runes, and a second copy of them here would
// be the parallel implementation this project refuses - one that drifts
// silently, because both would still produce *a* number.
//
// **Rendered through a text area of its own**, which is the part that is not
// tidiness. textarea.Model holds its viewport behind a pointer, so every copy of
// a Composer shares one - and that viewport is *scrolled*, by the reposition
// that keeps the cursor on screen. Measuring through it returns the height of
// the window rather than of the draft (it reported 1 for a three-line draft),
// and resetting it to measure would undo the cursor-follow this same change
// added. A fresh one shares nothing and starts unscrolled: SetValue does not
// run Update, so nothing repositions it.
//
// The text area pads to its height with rows carrying the prompt and nothing
// else, and a blank line somebody typed renders identically. Interior blanks
// survive because this keeps the last row with content; trailing ones are added
// back from the value, which is the only place they can still be told apart.
func (c Composer) draftRows(bound int) int {
	value := c.ta.Value()
	if value == "" {
		return 1
	}
	probe := textarea.New()
	probe.Prompt = c.ta.Prompt
	probe.ShowLineNumbers = false
	probe.SetWidth(c.taWidth)
	probe.SetHeight(bound)
	probe.SetValue(value)

	bare := strings.TrimRight(c.ta.Prompt, " ")
	last := 0
	for i, row := range strings.Split(ansi.Strip(probe.View()), "\n") {
		if strings.TrimRight(row, " ") != bare {
			last = i + 1
		}
	}
	trailing := len(value) - len(strings.TrimRight(value, "\n"))
	return max(last, 1) + trailing
}

// WithTitle names the pane this composer belongs to.
func (c Composer) WithTitle(t string) Composer { c.title = t; return c }

// WithBar sets the pre-rendered info line drawn between the box and the legend.
func (c Composer) WithBar(bar string) Composer { c.bar = bar; return c }

// WithColor gives this composer its agent's identity hue, so the border and the
// @name title draw in it. The DM pane sets it; the room does not, because the
// room is not one agent. An empty or unknown name keeps the accent.
func (c Composer) WithColor(name string) Composer { c.color = name; return c }

// Focused says whether this composer is the one keystrokes reach.
//
// It changes how the box is drawn: the accent border, and the caret. Where a
// keystroke actually goes is App.composer's business, and this is the half of
// that decision the operator can see - two composers where only one is live, and
// no way to tell which, is the pane version of a legend that names a key nothing
// binds.
//
// A pane that does not have the keys shows no caret. The blink loop only ever
// runs for the focused composer - App.Update feeds no other - so a caret left on
// in a blurred box is a second, dead cursor claiming "type here" in a box that
// will not take a keystroke. Blink is the caret's off phase (cursor.View), and
// the blurred composer is never fed Update, so setting it here holds the caret
// off until App.refocusBlink turns it back on when the keys arrive.
//
// It is *only* the caret, deliberately: the whole textarea's Blur would swap it
// to BlurredStyle and tint the draft text, but this composer's border is already
// the one thing focus changes here - draft text reads the same in every pane.
func (c Composer) Focused(yes bool) Composer {
	c.blurred = !yes
	if !yes {
		c.ta.Cursor.Blink = true
	}
	return c
}

// boxStyle is the border this composer draws: the receding pane border when
// another pane has the keys, the agent's identity hue when /color gave it one,
// and the shared accent otherwise. The hue is dropped on blur with the accent -
// the border's whole job is answering "where do I type", and a blurred box is
// not the answer whatever its colour.
func (c Composer) boxStyle() lipgloss.Style {
	if c.blurred {
		return BoxStyle
	}
	if hue, ok := identityColor(c.color); ok {
		return ComposerStyle.BorderForeground(hue)
	}
	return ComposerStyle
}

// Value is the current draft.
func (c Composer) Value() string { return c.ta.Value() }

// AtEnd reports whether the cursor is at the end of the draft, which is what
// makes the last token the token being typed. See completion.go, which is the
// only caller and the whole argument.
//
// Derived rather than read: the text area has no absolute cursor position, and
// LineInfo is about one *soft-wrapped* line - so this is the last logical row,
// and the last column of it. StartColumn is where that soft line begins inside
// the row, which is what makes the two terms a column in the row rather than in
// the wrap.
func (c Composer) AtEnd() bool {
	rows := strings.Split(c.ta.Value(), "\n")
	last := len(rows) - 1
	if c.ta.Line() != last {
		return false
	}
	info := c.ta.LineInfo()
	return info.StartColumn+info.ColumnOffset >= len([]rune(rows[last]))
}

// CanCursorUp reports whether ↑ would actually move the text cursor within the
// draft, which is when App.key gives the arrow to the composer rather than the
// roster. False for an empty or single-line draft, and false at the top row of
// a multi-line or soft-wrapped one.
//
// It runs the move on a copy and compares the position rather than trusting
// LineInfo's row count: bubbles' wrap adds a synthetic trailing row when a
// line's width is exactly the wrap width (its `>=`), which Height counts but the
// cursor cannot land on - so a count alone reports a move CursorUp does not make,
// which swallows the arrow (moving neither the cursor nor the roster). Because
// this is exactly the move App.key delegates to, the two can never disagree.
func (c Composer) CanCursorUp() bool {
	return c.cursorMoves((*textarea.Model).CursorUp)
}

// CanCursorDown is CanCursorUp's mirror for ↓.
func (c Composer) CanCursorDown() bool {
	return c.cursorMoves((*textarea.Model).CursorDown)
}

// cursorMoves applies a vertical move to a copy of the text area and reports
// whether the cursor changed rows. The copy shares the read-only draft buffer
// and the idempotent wrap cache by pointer but owns its own row and column, so
// the probe never disturbs the live draft.
func (c Composer) cursorMoves(move func(*textarea.Model)) bool {
	probe := c.ta
	before := cursorRow(probe)
	move(&probe)
	return cursorRow(probe) != before
}

// cursorRow is the cursor's vertical position: its logical line and the wrapped
// row within that line. Deliberately not the column - at an edge bubbles'
// CursorUp/CursorDown shuffles the column *within* a multi-rune grapheme (a
// combining accent, a wide ZWJ sequence) without changing the row, and reading
// that as a move would nudge the cursor into the middle of the glyph rather than
// moving the roster. Vertical movement is exactly a change of row.
func cursorRow(ta textarea.Model) [2]int {
	info := ta.LineInfo()
	return [2]int{ta.Line(), info.RowOffset}
}

// Mode is the permission mode this composer's legend names.
func (c Composer) Mode() string { return c.mode }

// WithMode says which mode the legend names, and it is set at draw time by
// whoever knows *whose* mode this composer is about - App.roomPane and
// App.dmPane. It is not a belief held here: a composer is a text box, and the
// one fact this whole feature exists to keep honest may not have a second home.
func (c Composer) WithMode(mode string) Composer {
	if mode != "" {
		c.mode = mode
	}
	return c
}

// WithArms says which arm the legend names instead of its ordinary labels: ⎋
// as the key that clears the draft, or ↵ as the one that finishes a detach. Set
// at draw time by App.armsFor, for WithMode's reason.
func (c Composer) WithArms(arms legendArms) Composer {
	c.arms = arms
	return c
}

// Reset clears the draft but preserves the permission mode, which is a
// property of the session rather than of one message.
//
// A sent or cleared draft ends a prompt walk: the position counts back from the
// newest prompt, and sending adds one.
func (c Composer) Reset() Composer {
	c.ta.Reset()
	c.hist = promptWalk{}
	c.attachments = nil
	c.nextImageID = 0
	return c
}

// WithDraft replaces the draft and puts the cursor at its end.
//
// Three callers: the picker's typed escape, which hands back a half-written
// command for somebody to finish; a prompt walk; and starts.go's draftMention.
// It is a *replacement* rather than an insert because the first two only ever
// reach this over an empty composer - the picker opens over one and a walk
// owns the whole draft - so there is nothing to preserve and an insert would
// be an API that implies otherwise. draftMention is the one caller that could
// reach a non-empty composer, and it gates itself before calling here rather
// than asking this to insert - dropping a fresh spawn's mention is cheaper
// than silently appending one onto a message the operator is mid-typing.
//
// It clears the walk for Reset's reason - a draft put here from anywhere else is
// not a position in the history - and Composer.walked writes the position back
// after calling it.
func (c Composer) WithDraft(text string) Composer {
	c.ta.SetValue(text)
	c.ta.CursorEnd()
	c.hist = promptWalk{}
	return c
}

// WithTarget says where ↵ will send, and how many turns that costs.
//
// turns is passed rather than read off the route because a target is not
// always a turn - the composer would have to know that a broadcast's targets
// are one turn each to derive it, which is a fact about sending that belongs
// where sending happens. See App.retarget.
//
// It takes a roomRoute rather than a core.Route because mention mode is this
// client's and not the router's: the same text resolves to the same agent
// under both readings, and what differs is who else hears it. See mention.go.
func (c Composer) WithTarget(r roomRoute, turns int) Composer {
	c.target = targetLine(r, turns)
	return c
}

// Target is the line above the legend, for a test that would otherwise have to
// read it back out of a rendered frame.
func (c Composer) Target() string { return c.target }
