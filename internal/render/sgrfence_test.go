package render

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
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
// a refused run drawn as text came back once a row.
func TestARefusedRunInAWrappedParagraphIsNotRepeatedAsText(t *testing.T) {
	src := strings.Repeat("tide ", 12) + "&#x1b;[8m" + strings.Repeat("gauge ", 12) + "&#x1b;[0m done"
	text := stripANSI(Markdown(src, 40))
	if strings.Contains(text, "[8m") || strings.Count(text, "gauge") != 12 {
		t.Errorf("the refused run is drawn, or the words it hid are not:\n%s", text)
	}
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
