package termsetup

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigHomeRespectsXDG(t *testing.T) {
	env := map[string]string{"XDG_CONFIG_HOME": "/custom/config", "HOME": "/Users/someone"}
	if got := ConfigHome(env); got != "/custom/config" {
		t.Errorf("ConfigHome = %q, want the XDG override", got)
	}
}

func TestConfigHomeDefaultsToHomeDotConfig(t *testing.T) {
	env := map[string]string{"HOME": "/Users/someone"}
	want := filepath.Join("/Users/someone", ".config")
	if got := ConfigHome(env); got != want {
		t.Errorf("ConfigHome = %q, want %q", got, want)
	}
}

// The three paths this package writes to, each under its own subdirectory -
// getting one of these wrong means either writing nowhere the terminal reads
// from, or writing somewhere it does not expect.
func TestConfigPathsForTheWritableTerminals(t *testing.T) {
	home := "/Users/someone/.config"
	for _, tc := range []struct {
		e    Emulator
		want string
	}{
		{Ghostty, filepath.Join(home, "ghostty", "config")},
		{Kitty, filepath.Join(home, "kitty", "kitty.conf")},
		{Alacritty, filepath.Join(home, "alacritty", "alacritty.toml")},
	} {
		if got := InfoFor(tc.e).ConfigPath(home); got != tc.want {
			t.Errorf("%v ConfigPath = %q, want %q", tc.e, got, tc.want)
		}
	}
}

// Every terminal this package does not write to must say so, and must carry
// at least one manual step - a terminal with AutoWritable false and nothing
// to read instead is a dead end.
func TestManualOnlyTerminalsHaveNoSnippetAndSomeSteps(t *testing.T) {
	for _, e := range []Emulator{ITerm2, AppleTerminal, VSCode, WezTerm, Unknown} {
		info := InfoFor(e)
		if info.AutoWritable {
			t.Errorf("%v is marked AutoWritable", e)
		}
		if info.Snippet != "" {
			t.Errorf("%v has a Snippet %q despite not being auto-writable", e, info.Snippet)
		}
		if len(info.ManualSteps) == 0 {
			t.Errorf("%v has no manual steps", e)
		}
	}
}

// The auto-writable three are the inverse: a snippet, no manual steps.
func TestAutoWritableTerminalsHaveASnippetAndNoManualSteps(t *testing.T) {
	for _, e := range []Emulator{Ghostty, Kitty, Alacritty} {
		info := InfoFor(e)
		if !info.AutoWritable {
			t.Errorf("%v is not marked AutoWritable", e)
		}
		if info.Snippet == "" {
			t.Errorf("%v has no Snippet", e)
		}
		if len(info.ManualSteps) != 0 {
			t.Errorf("%v has manual steps despite being auto-writable: %v", e, info.ManualSteps)
		}
	}
}

// The exact string the owner verified against real Ghostty. Anyone changing
// this snippet changes what the owner already confirmed works.
func TestGhosttySnippetIsTheVerifiedString(t *testing.T) {
	want := `keybind = shift+enter=text:\x1b\r`
	if got := InfoFor(Ghostty).Snippet; got != want {
		t.Errorf("Ghostty snippet = %q, want the verified %q", got, want)
	}
}

// Kitty's send_text takes a mode argument before the text - omitting it is
// not "the default mode", it is a different, invalid directive shape per
// kitty's own docs.
func TestKittySnippetHasTheModeArgument(t *testing.T) {
	got := InfoFor(Kitty).Snippet
	if !strings.Contains(got, "send_text all ") {
		t.Errorf("Kitty snippet = %q, want the mode argument \"all\" before the text", got)
	}
	if strings.Contains(got, `\x1b`) {
		t.Errorf("Kitty snippet = %q, kitty's own docs use \\e rather than \\x1b", got)
	}
}

// TOML has no \x escape - only \uXXXX - so \x1b\r would be a parse error.
// This is the exact bug filed against Claude Code's own /terminal-setup for
// Alacritty; the fix is asserted here so it cannot regress back to \x.
// tomlUnicodeEscToken is built by runtime string concatenation rather
// than typed as one literal: this exact six-character token has, in
// this repository's own editing history, been silently decoded into a
// raw ESC control byte when typed as a contiguous run in a tool call -
// see knowledge.go's header comment. Splitting it here is the same fix
// repeated at the one other place this file has to name it.
var tomlUnicodeEscToken = "\\" + "u001b"

func TestAlacrittySnippetUsesTheTOMLUnicodeEscapeNotHex(t *testing.T) {
	got := InfoFor(Alacritty).Snippet
	if strings.Contains(got, `\x1b`) {
		t.Errorf("Alacritty snippet = %q, contains the invalid TOML \\x escape", got)
	}
	if !strings.Contains(got, tomlUnicodeEscToken) {
		t.Errorf("Alacritty snippet = %q, missing the TOML-valid Unicode escape for ESC", got)
	}
	for _, want := range []string{`[[keyboard.bindings]]`, `key = "Return"`, `mods = "Shift"`} {
		if !strings.Contains(got, want) {
			t.Errorf("Alacritty snippet = %q, missing %q", got, want)
		}
	}
}

// VS Code's keybindings.json is JSON, which has the same \x-less rule as
// TOML - the manual step must use the same Unicode escape too.
func TestVSCodeManualStepUsesTheJSONUnicodeEscape(t *testing.T) {
	steps := InfoFor(VSCode).ManualSteps
	joined := strings.Join(steps, "\n")
	if strings.Contains(joined, `\x1b`) {
		t.Errorf("VS Code manual steps contain the invalid JSON \\x escape: %q", joined)
	}
	if !strings.Contains(joined, tomlUnicodeEscToken) {
		t.Errorf("VS Code manual steps are missing the JSON Unicode escape for ESC: %q", joined)
	}
}

// A raw ESC byte in any of this package's text is exactly the bug this
// package exists to avoid writing into somebody's config - every one of
// these strings must be the literal escape *text*, never the control byte
// itself. Scanned across the whole knowledge base rather than spot-checked,
// so a new terminal added later is covered by construction.
func TestNoStringInTheKnowledgeBaseContainsARawControlByte(t *testing.T) {
	for e := range knownInfo {
		info := InfoFor(e)
		check := func(what, s string) {
			for i := 0; i < len(s); i++ {
				b := s[i]
				if b < 0x20 && b != '\n' && b != '\t' {
					t.Errorf("%v %s contains a raw control byte 0x%02x at offset %d: %q", e, what, b, i, s)
				}
			}
		}
		check("Snippet", info.Snippet)
		check("ReloadHint", info.ReloadHint)
		for i, step := range info.ManualSteps {
			check("ManualSteps["+string(rune('0'+i))+"]", step)
		}
	}
}
