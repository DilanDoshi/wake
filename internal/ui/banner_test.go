package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The banner is the *first* block, which is what makes it scroll away.
//
// Everything about it follows from that placement. Drawn as chrome instead it
// would cost four rows of every frame forever, in a build whose whole premise is
// thirty of these open at once - and it would need a second re-wrap path, since
// renderAll is what a width change re-derives.
func TestTheBannerIsTheFirstBlockOfBothTranscripts(t *testing.T) {
	room := NewRoom().SetSize(100, 30)
	blocks := renderRoom(room, roomEvents(room))
	if len(blocks) == 0 {
		t.Fatal("the room renders no blocks at all, so it opens on nothing")
	}
	if !strings.Contains(blocks[0].text, bannerName) {
		t.Errorf("the room's first block is %q, want the banner: drawn anywhere but first it does "+
			"not scroll away, and drawn as chrome it costs rows of every frame", blocks[0].text)
	}

	dm := NewDM("s1", "alex").SetSize(100, 30)
	if got := renderTranscript(dm); len(got) == 0 || !strings.Contains(got[0].text, bannerName) {
		t.Errorf("a conversation's first block is not the banner: %+v", got)
	}
}

// Every row of the trio measures the same, or the words beside it move.
//
// lipgloss.JoinHorizontal lays the right-hand block against the widest row of
// the left one, so a single short row silently indents every line of text by
// however much the longest row won - and the sprite is the one block here whose
// rows are hand-written.
func TestTheTrioIsRectangular(t *testing.T) {
	rows := strings.Split(trio(), "\n")
	if len(rows) != len(spriteRows)+spriteRise {
		t.Fatalf("the trio is %d rows, want %d: one sprite plus the middle's rise", len(rows), len(spriteRows)+spriteRise)
	}
	want := lipgloss.Width(rows[0])
	for i, r := range rows {
		if got := lipgloss.Width(r); got != want {
			t.Errorf("trio row %d measures %d, want %d - a ragged sprite shifts the text beside it", i, got, want)
		}
	}
}

// The middle sprite is raised, which is what makes three read as a group.
//
// Asserted on the shape rather than the pixels: the top row carries only the
// middle sprite, so it has ink to the right of column zero and none at it.
func TestTheMiddleSpriteIsRaisedAboveTheOuterTwo(t *testing.T) {
	rows := strings.Split(trio(), "\n")
	top := rows[0]
	if strings.TrimSpace(top) == "" {
		t.Fatal("the trio's top row is blank, so no sprite is raised and three sit in a line")
	}
	if !strings.HasPrefix(top, " ") {
		t.Errorf("the trio's top row starts with ink (%q): the raised sprite is the middle one, so "+
			"the row above the others must be indented past the first", top)
	}
	// And the row below it carries all three, which is what "the outer two are
	// level" means.
	if lipgloss.Width(strings.TrimRight(rows[1], " ")) != lipgloss.Width(rows[1]) {
		t.Errorf("the second row does not reach the full width, so the right-hand sprite is missing from it")
	}
}

// A pane too narrow for the sprite keeps the words and loses the picture.
//
// The words are the information and the sprite is the ornament. Wrapping it
// instead puts a broken picture at the top of every conversation, and it is the
// one block a reader cannot scroll past to ignore, because it is what they open on.
func TestANarrowPaneDropsTheSpriteAndKeepsTheWords(t *testing.T) {
	wide := roomBanner(100).text
	narrow := roomBanner(20).text

	if !strings.Contains(wide, spriteRows[0]) {
		t.Error("a wide room banner has no sprite in it at all")
	}
	if strings.Contains(narrow, spriteRows[0]) {
		t.Errorf("a 20-column banner still draws the sprite:\n%s", narrow)
	}
	if !strings.Contains(narrow, bannerName) {
		t.Errorf("a narrow banner dropped the words as well as the sprite:\n%s", narrow)
	}
}

// The room says what it is and where; a conversation says what it is running as.
//
// The room is the fleet, not a session, so a model or an effort there would be a
// fact about whichever agent the cursor happened to be on - changing under a
// cursor that moves for unrelated reasons.
func TestTheRoomBannerCarriesNoSessionFacts(t *testing.T) {
	got := roomBanner(100).text
	for _, forbidden := range []string{"effort", "context", "Opus", "Sonnet"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("the room banner mentions %q:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(got, "v"+Version) {
		t.Errorf("the room banner does not name the version:\n%s", got)
	}
}

// A conversation names its model and effort once it has them, and says neither
// before - the model comes off the event stream, so a freshly opened pane has
// not seen one yet and must not draw an empty line where it will go.
func TestAConversationBannerGrowsItsModelLineWhenItHasOne(t *testing.T) {
	cold := dmBanner(Agent{Cwd: "/tmp/repo"}, 100).text
	if strings.Contains(cold, "effort") || strings.Contains(cold, "context") {
		t.Errorf("a conversation with no reported model drew a session line anyway:\n%s", cold)
	}

	warm := dmBanner(Agent{
		Cwd: "/tmp/repo", Model: "claude-opus-4-6-20260501", ContextWindow: 1_000_000, Effort: "xhigh",
	}, 100).text
	for _, want := range []string{"xhigh effort", "context"} {
		if !strings.Contains(warm, want) {
			t.Errorf("a conversation banner is missing %q:\n%s", want, warm)
		}
	}
}

// The cap is named on the session line, in effort's voice rather than the
// model's.
//
// **It says what Wake asked for and never what has been spent.** total_cost_usd
// is a level Wake already reads, so a line claiming progress toward the ceiling
// would be a claim about spend built on a cap this build set itself - and the
// pane's claim about a model is a different and stronger kind, because init
// reports one every turn. Effort has the same standing and the same wording, and
// this follows it.
func TestAConversationBannerNamesTheCapItWasStartedUnder(t *testing.T) {
	capped := dmBanner(Agent{
		Cwd: "/tmp/repo", Model: "claude-opus-4-6-20260501", ContextWindow: 1_000_000,
		Effort: "xhigh", Budget: "5.00",
	}, 100).text
	if !strings.Contains(capped, "5.00") {
		t.Errorf("a session started under a cap does not name it:\n%s", capped)
	}
	if !strings.Contains(capped, "xhigh effort") {
		t.Errorf("the cap displaced the effort rather than joining it:\n%s", capped)
	}

	// A session with no cap says nothing about one, which is the common case:
	// an empty ceiling drawn as a row is a row spent saying nothing happened.
	uncapped := dmBanner(Agent{
		Cwd: "/tmp/repo", Model: "claude-opus-4-6-20260501", ContextWindow: 1_000_000, Effort: "xhigh",
	}, 100).text
	if strings.Contains(uncapped, "capped") || strings.Contains(uncapped, "$") {
		t.Errorf("a session with no cap drew one anyway:\n%s", uncapped)
	}
}

// A cap on a session that has not reported a model yet draws nothing, and so do
// the model and the effort - all three gate on the model, which arrives on an
// init. The one fact that shows before it is the agent's name, which Wake knows
// at spawn and which answers "which of these am I looking at".
func TestACapDrawsNothingBeforeTheModelDoes(t *testing.T) {
	cold := dmBanner(Agent{Name: "aptos", Cwd: "/tmp/repo", Budget: "5.00"}, 100).text
	if strings.Contains(cold, "5.00") {
		t.Errorf("a cap was drawn before the session reported a model:\n%s", cold)
	}
	if !strings.Contains(cold, "aptos") {
		t.Errorf("the banner dropped the agent name along with the unreported cap:\n%s", cold)
	}
}

// The conversation banner names the agent it opens on - the fact that answers
// "which of these thirty am I looking at" - so it shows the moment the pane has
// a name, before the model has come off any init, and leads the model once one
// has.
func TestAConversationBannerNamesTheAgent(t *testing.T) {
	cold := dmBanner(Agent{Name: "aptos", Cwd: "/tmp/repo"}, 100).text
	if !strings.Contains(cold, "aptos") {
		t.Errorf("a conversation banner does not name its agent before the model arrives:\n%s", cold)
	}
	// The name is all it knows: it says nothing about a model or effort unseen.
	if strings.Contains(cold, "effort") || strings.Contains(cold, "context") {
		t.Errorf("a banner with only a name drew model facts anyway:\n%s", cold)
	}

	warm := dmBanner(Agent{
		Name: "aptos", Cwd: "/tmp/repo", Model: "claude-opus-5", ContextWindow: 1_000_000, Effort: "high",
	}, 100).text
	if i, j := strings.Index(warm, "aptos"), strings.Index(warm, "Opus 5"); i < 0 || j < 0 || i > j {
		t.Errorf("the agent name does not lead the model on the fact row:\n%s", warm)
	}
	// And the facts are dot-separated by factSep, not merely present in order - a
	// join by " " or ", " would pass every check above and lose Claude Code's shape.
	if !strings.Contains(warm, "aptos"+factSep+"Opus 5") {
		t.Errorf("the name and model are not joined by factSep:\n%s", warm)
	}
}

// The directory sits above the session's fact row. The fact row is last so that
// it grows into empty space when a model arrives, rather than pushing the
// directory down a line the first time an init lands.
func TestTheConversationBannerPutsTheDirectoryAboveTheFacts(t *testing.T) {
	got := dmBanner(Agent{
		Name: "aptos", Cwd: "/tmp/repo", Model: "claude-opus-5", ContextWindow: 1_000_000, Effort: "high",
	}, 100).text
	dir, facts := strings.Index(got, "repo"), strings.Index(got, "high effort")
	if dir < 0 || facts < 0 {
		t.Fatalf("the banner is missing the directory or the fact row:\n%s", got)
	}
	if dir > facts {
		t.Errorf("the directory is drawn below the fact row rather than above it:\n%s", got)
	}
}
