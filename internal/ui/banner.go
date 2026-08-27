package ui

// The banner every conversation opens with: Claude Code's own, in Wake's terms.
//
// Claude Code puts a sprite and three facts at the top of a session - what you
// are running, what it is running as, and where. Wake had none of it, so a pane
// opened with no answer to "which of these thirty things am I looking at".
//
// # It is a block, not chrome
//
// It is the first entry Room.renderAll and DM.renderAll emit, which makes it
// part of the transcript rather than a frame around one. Three properties fall
// out of that and all three are the reason it goes there:
//
//   - **it scrolls away**, which is what Claude Code does - a banner pinned to
//     every frame costs four rows of a pane you may have thirty of, forever;
//   - **it re-wraps for free**, because renderAll is what a width change
//     re-derives, so there is no second path to keep in step;
//   - **it cannot disagree with the transcript's own width**, since it is
//     measured by the same transcript that measures every other block.
//
// # The room and a conversation say different things
//
// A conversation is one session and says what Claude Code says: the directory,
// then the agent's name, the model and the effort. The **room is not a session**
// - it is the fleet - so it says only what it is and where it is running.
// Anything else there would be a fact about whichever agent the cursor happened
// to be on, changing under a cursor that moves for unrelated reasons, and the
// awareness strip already reports the fleet every frame.

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// Version is what the banner claims to be. A constant rather than a build
// stamp: there is no release process to derive one from yet, and a version that
// is a lie about a commit is worse than one that is honestly a name.
const Version = "0.1.0"

const (
	// bannerName is the product, and the one word in the banner set in Text.
	bannerName = "Wake"

	// warnGlyph opens the MCP row. Claude Code's own, and one cell wide.
	warnGlyph = "⚠"

	// spriteGap is the blank column between two sprites in the trio, and
	// bannerGap the columns between the sprite block and the words. Both are
	// spaces, so neither is subject to the ambiguity below.
	spriteGap  = " "
	bannerGap  = "   "
	spriteRise = 1

	// factSep joins the facts on a conversation's session line - the name, the
	// model, the effort - the way Claude Code dot-separates its own.
	factSep = " · "
)

// The sprite, in half-block glyphs.
//
// # These are ambiguous-width, and that is a bet
//
// U+2580, U+2584 and U+2588 are East Asian **Ambiguous**: one column in a
// Western locale and two in a terminal told to treat ambiguous characters as
// double width, which iTerm2, Ghostty and WezTerm all expose. lipgloss.Width
// answers 1 unconditionally, because it cannot know the setting - so a doubled
// terminal draws this sprite at twice the width Wake measured.
//
// The bet is already made everywhere else in this build: every character of
// lipgloss's rounded border is ambiguous too, and that border frames every pane.
// So this adds glyphs to a class that already exists rather than opening one.
// What it must not do is make the *failure* worse, which is why bannerBlock
// drops the sprite entirely below a width it fits - a banner that loses its
// picture is a banner; a banner that wraps its picture is a broken frame at the
// top of every conversation.
var (
	// spriteRows is one sprite: a rounded body with two eyes.
	spriteRows = []string{
		"▄▀▀▀▀▄",
		"█ ▀▀ █",
		"▀▄▄▄▄▀",
	}

	// spriteStyle is the sprite's colour: Claude's own `clawd_body`, which is
	// the same value as Accent and is spelled through it so the two cannot
	// drift apart.
	spriteStyle = lipgloss.NewStyle().Foreground(Accent)

	// bannerNameStyle is the product's name, and bannerFactStyle everything
	// under it - the version, the model, the directory. Two weights rather than
	// two colours, which is what Claude Code does: the name is what you look
	// for, the facts are what you read once.
	bannerNameStyle = lipgloss.NewStyle().Foreground(Text).Bold(true)
	bannerFactStyle = lipgloss.NewStyle().Foreground(Muted)
)

// spriteWidth is one sprite in cells, measured rather than counted: the glyphs
// are multi-byte and len() would answer in bytes.
func spriteWidth() int { return lipgloss.Width(spriteRows[0]) }

// trio is three sprites with the middle one raised.
//
// Raised rather than in a line because the room is a *group* and three things at
// one height read as a repeat of one thing. The middle sits a row up, which is
// the smallest arrangement that reads as several.
//
// Every row is padded to the same width, because a ragged block joined beside
// text pushes the text sideways by however much the longest row won.
func trio() string {
	w := spriteWidth()
	total := w*3 + len(spriteGap)*2
	rows := make([]string, 0, len(spriteRows)+spriteRise)

	// The middle's top row, alone above the two outer sprites.
	rows = append(rows, pad(strings.Repeat(" ", w+len(spriteGap))+spriteRows[0], total))
	for i := range spriteRows {
		var b strings.Builder
		b.WriteString(spriteRows[i])
		b.WriteString(spriteGap)
		// The middle is one row further through its own sprite than the outer
		// two, which is what "raised" means: at the outer sprites' last row it
		// has already finished, and draws blank.
		if next := i + spriteRise; next < len(spriteRows) {
			b.WriteString(spriteRows[next])
		} else {
			b.WriteString(strings.Repeat(" ", w))
		}
		b.WriteString(spriteGap)
		b.WriteString(spriteRows[i])
		rows = append(rows, pad(b.String(), total))
	}
	return strings.Join(rows, "\n")
}

// single is one sprite, for a conversation with one agent.
func single() string { return strings.Join(spriteRows, "\n") }

// pad right-fills a row to width so a block is rectangular.
func pad(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// roomBanner is what the room opens with: what this is, and where it runs.
//
// **Wake's own directory, not an agent's.** The room is the fleet and the fleet
// spans directories - a conversation's banner answers "where does this session
// run", and the room has no single answer to give. Where `wake` was started is
// the one location fact that is true of the room itself.
func roomBanner(width int) block {
	return bannerBlock(trio(), []string{shortPath(wakeDir())}, width)
}

// wakeDir is where this Wake was started, resolved once.
//
// Cached because renderAll runs on every re-wrap and this is a syscall, and
// because the answer cannot change: nothing in this process chdirs, and a
// directory that moved under it would not make an older answer less true about
// where it was started.
var wakeDir = sync.OnceValue(func() string {
	dir, err := os.Getwd()
	if err != nil {
		// Not worth a notice: the banner drops a line it cannot fill, exactly
		// as it does for a model nothing has reported yet.
		return ""
	}
	return dir
})

// dmBanner is what a conversation opens with: Claude Code's facts about the
// session in front of you, in Claude Code's order - the directory, then what it
// is running as.
//
// The fact row grows as the session reports: the agent's name shows at once, and
// the model, effort and cap join it when the first `init` lands - which is why
// it is drawn last, so it grows into empty space rather than pushing the
// directory down. The lines are filtered rather than laid out at a fixed height
// for the same reason.
func dmBanner(a Agent, width int) block {
	b := bannerBlock(single(), []string{shortPath(a.Cwd), sessionLine(a)}, width)
	if w := mcpWarning(a.MCPNeedsAuth); w != "" {
		// Under the block rather than inside it, which is where Claude Code
		// draws it: the facts describe the session and this is a thing to *do*,
		// so it gets its own row and its own colour instead of joining a list
		// of statements about what is running.
		b.text += "\n\n" + w
	}
	return b
}

// mcpWarning is Claude Code's row, or "" when there is nothing to say.
//
// Empty is the overwhelmingly common case and must cost nothing: most sessions
// hold no MCP servers, and a row reserved for a warning nobody has would be
// spent on every conversation in the fleet, forever.
//
// # Why it does not say `· run /mcp`, which is what Claude Code says
//
// **Not because the command is inert - it works.** That was the guess, and it
// was wrong. Recorded from a headless session (2026-08-14 config-surface
// findings, `testdata/stream/bare-mcp.jsonl`), `/mcp` answers at zero cost and
// with no model turn:
//
//	6 MCP server(s): 2 connected, 2 connecting, 2 not connected, 0 disabled.
//	Use `/mcp` in the terminal for details.
//
// It is **read-only from here, and says so itself**. Authenticating needs a
// terminal. So naming it would send somebody to a command that reports the same
// counts this row is already showing them and then tells them to go elsewhere -
// advice given at exactly the moment they had already failed once.
//
// The row states the fact and stops. What would license the other half is a way
// to authenticate *from* a headless session, which is a thing claude does not
// currently have rather than a thing Wake has not built.
//
// warnStyle is dm_blocks.go's, reused rather than redeclared: one style for
// "something needs attention" across the pane, which is what stops this row and
// a blocked tool call being two different yellows.
func mcpWarning(needsAuth int) string {
	if needsAuth < 1 {
		return ""
	}
	noun := "servers need"
	if needsAuth == 1 {
		noun = "server needs"
	}
	return warnStyle.Render(fmt.Sprintf("%s %d MCP %s authentication", warnGlyph, needsAuth, noun))
}

// sessionLine is what this session is: the agent's name, then - once the model
// has come off an init - the model, the effort if one was chosen, and the spend
// ceiling if there is one, dot-separated the way Claude Code separates its own
// facts. Minus the plan, which Wake cannot see.
//
// The name leads and shows on its own, because Wake knows it at spawn and it is
// the fact that answers "which of these thirty am I looking at". The model gates
// the other three: it arrives on an init, so a cap or an effort drawn before it
// would describe a model the pane has not seen. dmBanner draws this row last, so
// it grows in place rather than pushing the directory down.
//
// **The cap is in effort's voice, not the model's.** Both are what Wake *asked
// for*: init reports the model every turn, so naming one is a claim about what
// is running, and nothing reports either of these back at all. Hence "capped at"
// rather than any phrasing that would read as progress toward a ceiling - see
// rpc.SessionStatus.Budget.
func sessionLine(a Agent) string {
	parts := make([]string, 0, 4)
	if a.Name != "" {
		parts = append(parts, a.Name)
	}
	if model := modelName(a.Model, a.ContextWindow); model != "" {
		parts = append(parts, model)
		if a.Effort != "" {
			parts = append(parts, a.Effort+" effort")
		}
		if a.Budget != "" {
			parts = append(parts, "capped at $"+a.Budget)
		}
	}
	return strings.Join(parts, factSep)
}

// bannerBlock lays a sprite beside a name and its facts.
//
// **The sprite is dropped rather than wrapped when it does not fit**, and the
// words are kept: the words are the information and the sprite is the ornament,
// so a narrow pane loses the ornament. Wrapping it instead would put a broken
// picture at the top of every conversation, and it is the one block a reader
// cannot scroll past to ignore because it is the first thing there is.
func bannerBlock(sprite string, facts []string, width int) block {
	lines := make([]string, 0, len(facts)+1)
	lines = append(lines, bannerNameStyle.Render(bannerName)+"  "+bannerFactStyle.Render("v"+Version))
	for _, f := range facts {
		if f != "" {
			lines = append(lines, bannerFactStyle.Render(f))
		}
	}
	words := strings.Join(lines, "\n")

	art := spriteStyle.Render(sprite)
	if lipgloss.Width(sprite)+len(bannerGap)+lipgloss.Width(words) > width {
		return block{text: words}
	}
	return block{text: lipgloss.JoinHorizontal(lipgloss.Top, art, bannerGap, words)}
}
