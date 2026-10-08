package render

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// docs/notes/bugs.md BUG-50. glamour decodes a numeric character reference, so
// `&#x1b;[8m` in a reply is a real ESC followed by an SGR - and a complete SGR
// was all the output fence asked for. A reply could conceal (8), blink (5) or
// reverse (7) the text after it. The style emits none of those, so the run is
// dropped whole and the text it would have hidden is drawn plainly.
func TestAnEntityCannotSmuggleAnSGRTheStyleDoesNotEmit(t *testing.T) {
	for _, tc := range []struct{ name, src, code string }{
		{"conceal", "before&#x1b;[8mafter", "8"},
		{"blink", "before&#27;[5mafter", "5"},
		{"reverse", "before&#x1b;[7mafter", "7"},
		// Dropped whole, so the bold beside the conceal goes with it; the run is
		// never rewritten to keep its allowed half.
		{"conceal beside bold", "before&#x1b;[1;8mafter", "8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Markdown(tc.src, 60)
			for _, run := range ansiPattern.FindAllString(out, -1) {
				if _, ok := sgrKinds(run)[tc.code]; ok {
					t.Errorf("the render keeps SGR %s, which the style never emits: %q", tc.code, out)
				}
			}
			if text := stripANSI(out); !strings.Contains(text, "beforeafter") {
				t.Errorf("the refused run was not dropped whole: %q", text)
			}
		})
	}
}

// reflow, inside glamour, re-opens the last SGR it saw on every row it wraps, so
// a refused run drawn as text came back once a row, each copy cells glamour had
// measured as none - the row the real binary drew past its pane.
func TestARefusedRunInAWrappedParagraphIsNotRepeatedAsText(t *testing.T) {
	src := strings.Repeat("tide ", 12) + "&#x1b;[8m" + strings.Repeat("gauge ", 24) + "&#x1b;[0m done"
	for _, width := range []int{40, 80} {
		out := Markdown(src, width)
		for _, row := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(row); w > width {
				t.Errorf("width %d: a row is %d cells: %q", width, w, stripANSI(row))
			}
		}
		for _, run := range ansiPattern.FindAllString(out, -1) {
			if _, ok := sgrKinds(run)["8"]; ok {
				t.Errorf("width %d: the render keeps the conceal: %q", width, run)
			}
		}
		if text := stripANSI(out); strings.Contains(text, "[8m") || strings.Count(text, "gauge") != 24 {
			t.Errorf("width %d: the refused run is drawn, or the words it hid are not:\n%s", width, text)
		}
	}
}

// A dropped run is silent, so a fence narrower than the style would lose styling
// unseen. Every run glamour emits for the probe and for every recorded answer
// must leave the fence as it entered.
func TestTheOutputFenceDropsNoRunTheRendererEmits(t *testing.T) {
	docs := append([]string{styleProbe}, recordedAnswers(t)...)
	for _, dark := range []bool{true, false} {
		r, err := glamour.NewTermRenderer(glamour.WithStyles(claudeStyle(dark)), glamour.WithWordWrap(80))
		if err != nil {
			t.Fatalf("build a renderer: %v", err)
		}
		for _, doc := range docs {
			out, err := r.Render(doc)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if in, kept := ansiPattern.FindAllString(out, -1), ansiPattern.FindAllString(stylingOnly(out), -1); !slices.Equal(in, kept) {
				t.Errorf("the fence dropped runs the renderer emits (dark %v): %d in, %d kept, from %.60q", dark, len(in), len(kept), doc)
			}
		}
	}
}

// recordedAnswers is every assistant text block in the stream corpus, through the airlock.
func recordedAnswers(t *testing.T) []string {
	t.Helper()
	files, _ := filepath.Glob("../../testdata/stream/*.jsonl")
	var out []string
	for _, path := range files {
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(blob), "\n") {
			evs, err := core.DecodeLine([]byte(line))
			if err != nil {
				continue
			}
			for _, ev := range evs {
				if ev.Kind == core.KindAssistantText {
					out = append(out, ev.Text)
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no recorded answers: the sweep would assert nothing")
	}
	return out
}

// An extended colour's arguments are arguments: in `38;5;7` the 7 is an index,
// not reverse, and a colour the style can emit is kept.
func TestAnEntitySmuggledColourIsReadAsAColour(t *testing.T) {
	out := Markdown("before&#x1b;[38;5;7mafter", 60)
	if !strings.Contains(out, "\x1b[38;5;7m") {
		t.Errorf("the colour was not kept: %q", out)
	}
	if text := stripANSI(out); strings.Contains(text, "[38;5;7m") {
		t.Errorf("a colour the style emits was drawn as text: %q", text)
	}
}

// styleProbe is every construct claudeStyle styles, with fences in several of
// chroma's lexers - a diff among them, which paints a background.
const styleProbe = "# One\n\n## Two\n\n###### Six\n\n" +
	"Plain, *emphasis*, **strong**, ~~struck~~, `code`, [a link](https://example.com), " +
	"![an image](https://example.com/x.png) and <https://example.com/auto>.\n\n" +
	"> a quote with **strong** in it\n\n" +
	"- an item\n- [ ] a task\n- [x] a done task\n\n1. first\n2. second\n\n" +
	"| a | b |\n|---|---|\n| 1 | 2 |\n\n---\n\nterm\n: its definition\n\n" +
	"```go\npackage main\n\n// a comment\nfunc main() { s, n := \"x\", 42; _ = s; _ = n }\n```\n\n" +
	"```diff\n@@ -1 +1 @@\n- old\n+ new\n```\n\n" +
	"```python\ndef f(x):\n    # c\n    return 'y' + str(x)\n```\n\n" +
	"```js\nconst x = /re/g; let y = `t${x}`; // c\n```\n\n" +
	"```bash\necho \"$HOME\" | grep -v x # c\n```\n\n" +
	"```json\n{\"a\": 1, \"b\": [true, null]}\n```\n\n" +
	"```yaml\na: b # c\n```\n\n" +
	"```html\n<div class=\"x\">&amp;</div>\n```\n\n" +
	"```rust\nfn main() { let x: u8 = 1; }\n```\n\n" +
	"```sql\nSELECT * FROM t WHERE a = 'b';\n```\n\n" +
	"```\nan unlabelled fence\n```\n"

// The fence keeps exactly the parameters the style emits, derived rather than
// listed: the probe is rendered in both palettes straight through glamour - what
// stylingOnly is handed - and every parameter it emits must be one styleSGR
// names, and every one styleSGR names must be emitted. None of them hides,
// flashes or inverts text.
func TestTheOutputFenceKeepsExactlyWhatTheStyleEmits(t *testing.T) {
	emitted := map[string]int{}
	for _, dark := range []bool{true, false} {
		r, err := glamour.NewTermRenderer(glamour.WithStyles(claudeStyle(dark)), glamour.WithWordWrap(80))
		if err != nil {
			t.Fatalf("build a renderer: %v", err)
		}
		out, err := r.Render(styleProbe)
		if err != nil {
			t.Fatalf("render the probe: %v", err)
		}
		for _, run := range ansiPattern.FindAllString(out, -1) {
			maps.Copy(emitted, sgrKinds(run))
		}
	}
	if !maps.Equal(emitted, styleSGR) {
		t.Errorf("the style emits %v and the fence keeps %v: the two must be one set",
			slices.Sorted(maps.Keys(emitted)), slices.Sorted(maps.Keys(styleSGR)))
	}
	for _, code := range []string{"5", "6", "7", "8"} {
		if _, ok := styleSGR[code]; ok {
			t.Errorf("the fence keeps SGR %s, which blinks, inverts or hides text", code)
		}
	}
}

// sgrKinds is a run's parameters read the way a terminal reads them, written out
// here rather than reached through the fence: a plain code is itself, and an
// extended colour is its introducer and form, with the count of arguments that
// follow - never read as codes.
func sgrKinds(run string) map[string]int {
	kinds := map[string]int{}
	params := strings.Split(run[len("\x1b["):len(run)-1], ";")
	for i := 0; i < len(params); i++ {
		p := params[i]
		if (p == "38" || p == "48" || p == "58") && i+1 < len(params) {
			switch params[i+1] {
			case "5":
				kinds[p+";5"] = 1
				i += 2
				continue
			case "2":
				kinds[p+";2"] = 3
				i += 4
				continue
			}
		}
		kinds[p] = 0
	}
	return kinds
}
