package termsetup

import "path/filepath"

// What this package knows about each terminal: where its config lives, what
// to write there, and how to make the write take effect.
//
// Two bindings per terminal: Shift+Enter -> ESC CR (a newline in the composer)
// and Cmd+Left/Right -> CSI H / CSI F (Home/End, which move the composer cursor
// to line start/end). Ghostty's key names for the arrows (`super`, `arrow_left`,
// `arrow_right`) are the ground truth its own `+list-keybinds` output reports;
// like the Shift+Enter line, whether the ⌘ bindings land end-to-end is an
// owner check against each real terminal.
//
// Every escape sequence below is a *literal* piece of text meant to land in
// the config file byte for byte - it is the terminal's own config parser
// that turns it into ESC (0x1B) then CR (0x0D) when the file is read, the way
// a shell decodes $'\x1b\r'. Writing the raw control bytes instead of this
// text would make an unreadable config file and is not what any of these
// formats expect.
//
// Ghostty's `text:` syntax is confirmed valid as written here (the owner
// verified it directly). Kitty's `send_text` action decodes ANSI-C escapes
// per its own docs, which demonstrate `\e` for ESC. Alacritty's config is
// TOML, which has no `\x` escape at all - only `\uXXXX` - so `\x1b\r` there
// is a parse error; the same class of bug filed against Claude Code's own
// /terminal-setup for Alacritty (upstream issues #14793, #19288, #24714,
// #25411, which propose `\^[` - also not valid TOML). The Unicode-escape form
// (spelled out in unicodeEscEscape below) is the escape TOML and JSON actually
// accept, derived from their grammars rather than copied from those issues.
// VS Code's keybindings.json is JSON, which has the same `\uXXXX`-only rule.
const (
	ghosttyEscape = `\x1b\r`
	kittyEscape   = `\e\r`
	// unicodeEscEscape spells ESC CR the one way both TOML and JSON
	// accept: a Unicode escape for ESC (code point 0x1B), then the
	// backslash-r both formats share for CR (0x0D). Neither format has
	// an \x escape at all, unlike ghosttyEscape/kittyEscape above.
	unicodeEscEscape = `\u001b\r`
	alacrittyEscape  = unicodeEscEscape
	vscodeEscape     = unicodeEscEscape

	// ⌘← and ⌘→ line navigation: the byte sequences CSI H (Home) and CSI F
	// (End). bubbletea reads those as Home/End, and internal/ui/composer.go's
	// textarea moves the cursor to line start/end on them (verified: \x1b[H
	// parses as home and lands the cursor at column 0). Same escape-text rule
	// as the ESC-CR constants above - the terminal's own parser decodes these,
	// so they are text and never raw bytes. \x1b for the formats that take it;
	// the Unicode escape for TOML/JSON, built by concatenation so the six-char
	// token is never contiguous in a tool call that would decode it to a raw
	// ESC byte - the same split, for the same reason, as knowledge_test.go's
	// tomlUnicodeEscToken. (unicodeEscEscape above predates this and is left as
	// one literal; nothing edits it, so its contiguous token is not re-exposed.)
	ghosttyHome = `\x1b[H`
	ghosttyEnd  = `\x1b[F`
	kittyHome   = `\e[H`
	kittyEnd    = `\e[F`
	unicodeHome = `\u` + `001b[H`
	unicodeEnd  = `\u` + `001b[F`
)

// marker is the comment line every snippet this package writes carries, so a
// second run recognises its own edit and Undo knows exactly what to remove.
//
// Its text is part of the recognition, so changing it (as this did, to name the
// Cmd+Left/Right binding) means a config written by an earlier version is not
// recognised: a re-run appends a fresh block rather than upgrading in place,
// leaving the old block and a duplicate (no-op) Shift+Enter keybind behind, and
// Undo then removes only the newer block. Accepted deliberately over
// version-aware migration - the duplication is one-time and cosmetic, and only
// a user who both ran the shipped version and re-runs after upgrading sees it.
const marker = "# added by `wake setup-terminal`: Shift+Enter sends a newline, and Cmd+Left/Right jump to line start/end"

// Info is what this package knows about one terminal.
//
// It carries no Emulator field of its own: knownInfo is keyed on Emulator,
// and a caller that has an Info reached InfoFor(e) with e already in hand -
// Result is the type that travels without that context, and it is the one
// that names which terminal it is about.
type Info struct {
	// AutoWritable is whether Apply may write the file itself. False for
	// every structured format this package refuses to parse and rewrite -
	// iTerm2's plist, VS Code's JSONC, WezTerm's Lua - and for Terminal.app
	// and Unknown, which have no config file of this kind to write.
	AutoWritable bool

	// configDir and configFile, joined, are the file Apply writes and Undo
	// edits. configDir is nil for a terminal with no such file.
	configDir  func(configHome string) string
	configFile string

	// Snippet is the exact text Apply appends, shown before the confirmation
	// prompt. Empty when AutoWritable is false.
	Snippet string

	// ManualSteps are copy-paste instructions for a terminal this package
	// will not write to, in order. Empty when AutoWritable is true.
	ManualSteps []string

	// ReloadHint says how the change takes effect.
	ReloadHint string

	// conflicts reports whether existing content makes an automatic append
	// unsafe even though AutoWritable is true - Alacritty's own hazard, and
	// the reason it has one. nil for every terminal with no such format-level
	// risk in a plain append.
	conflicts func(content []byte) bool

	// ConflictSteps are shown instead of writing when conflicts is non-nil
	// and returns true. Empty whenever conflicts is nil.
	ConflictSteps []string
}

// ConfigPath is the file this Info's Apply/Undo reads and writes, or "" for
// a terminal with no config file of this kind.
func (i Info) ConfigPath(configHome string) string {
	if i.configDir == nil {
		return ""
	}
	return filepath.Join(i.configDir(configHome), i.configFile)
}

// ConfigHome is the directory these terminals' configs are rooted under:
// $XDG_CONFIG_HOME if set, else $HOME/.config - the rule Ghostty, Kitty and
// Alacritty each document for themselves.
func ConfigHome(env map[string]string) string {
	if v := env["XDG_CONFIG_HOME"]; v != "" {
		return v
	}
	return filepath.Join(env["HOME"], ".config")
}

var knownInfo = map[Emulator]Info{
	Ghostty: {
		AutoWritable: true,
		configDir:    func(home string) string { return filepath.Join(home, "ghostty") },
		configFile:   "config",
		Snippet: "keybind = shift+enter=text:" + ghosttyEscape + "\n" +
			"keybind = super+arrow_left=text:" + ghosttyHome + "\n" +
			"keybind = super+arrow_right=text:" + ghosttyEnd,
		ReloadHint: "Ghostty reloads its config automatically; if the new keys still behave as " +
			"before, reload it by hand or restart Ghostty.",
	},
	Kitty: {
		AutoWritable: true,
		configDir:    func(home string) string { return filepath.Join(home, "kitty") },
		configFile:   "kitty.conf",
		Snippet: "map shift+enter send_text all " + kittyEscape + "\n" +
			"map cmd+left send_text all " + kittyHome + "\n" +
			"map cmd+right send_text all " + kittyEnd,
		ReloadHint: "Kitty reloads kitty.conf automatically; if not, press ctrl+shift+f5 or run " +
			"`kill -SIGUSR1 $KITTY_PID`.",
	},
	Alacritty: {
		AutoWritable: true,
		configDir:    func(home string) string { return filepath.Join(home, "alacritty") },
		configFile:   "alacritty.toml",
		Snippet: "[[keyboard.bindings]]\n" +
			`key = "Return"` + "\n" +
			`mods = "Shift"` + "\n" +
			`chars = "` + alacrittyEscape + `"` + "\n\n" +
			"[[keyboard.bindings]]\n" +
			`key = "Left"` + "\n" +
			`mods = "Command"` + "\n" +
			`chars = "` + unicodeHome + `"` + "\n\n" +
			"[[keyboard.bindings]]\n" +
			`key = "Right"` + "\n" +
			`mods = "Command"` + "\n" +
			`chars = "` + unicodeEnd + `"`,
		ReloadHint: "Alacritty reloads alacritty.toml automatically (live_config_reload); if not, restart it.",
		conflicts:  alacrittyKeyboardTableConflict,
		ConflictSteps: []string{
			"alacritty.toml already has a [keyboard] table, and Alacritty's own docs show bindings " +
				"written two ways - appending array-of-tables entries risks a parse error if " +
				"yours already assigns `bindings` as an inline array under it. Add these by hand instead, " +
				"inside your existing [keyboard] table (or as their own [[keyboard.bindings]] entries if " +
				"yours already uses that form):",
			`{ key = "Return", mods = "Shift", chars = "` + alacrittyEscape + `" }`,
			`{ key = "Left", mods = "Command", chars = "` + unicodeHome + `" }`,
			`{ key = "Right", mods = "Command", chars = "` + unicodeEnd + `" }`,
		},
	},
	ITerm2: {
		ManualSteps: []string{
			"iTerm2's keymap lives in a binary plist, so wake setup-terminal won't edit it for you:",
			"1. Preferences (Cmd+,) -> Profiles -> your profile -> Keys -> Key Mappings.",
			`2. Click + and set: Keyboard Shortcut = Shift+Return (press it in the field); ` +
				`Action = "Send Text with \"vim\" Special Chars"; Text = \x1b\r`,
			`3. Click + again: Keyboard Shortcut = Cmd+Left Arrow; ` +
				`Action = "Send Text with \"vim\" Special Chars"; Text = \x1b[H`,
			`4. Click + again: Keyboard Shortcut = Cmd+Right Arrow; same Action; Text = \x1b[F`,
			"5. Click OK - no restart needed.",
			`If you use iTerm2's "Natural Text Editing" preset it already maps Cmd+Left/Right to ` +
				`Ctrl+A/Ctrl+E, which Wake reads as its own keys; these explicit mappings to \x1b[H ` +
				`and \x1b[F override it.`,
		},
		ReloadHint: "Applies as soon as you click OK.",
	},
	AppleTerminal: {
		ManualSteps: []string{
			"Terminal.app has no way to make a key combination send a custom byte sequence - its " +
				"keyboard preferences remap menu shortcuts, not what gets sent to the shell. Ctrl+J " +
				"already inserts a newline here with nothing to configure, or switch to iTerm2, " +
				"Ghostty, Kitty or Alacritty, which all support this.",
		},
	},
	VSCode: {
		ManualSteps: []string{
			`VS Code's integrated terminal takes these in keybindings.json (Cmd/Ctrl+Shift+P -> ` +
				`"Preferences: Open Keyboard Shortcuts (JSON)"). Add:`,
			`{"key": "shift+enter", "command": "workbench.action.terminal.sendSequence", ` +
				`"args": {"text": "` + vscodeEscape + `"}, "when": "terminalFocus"},`,
			`{"key": "cmd+left", "command": "workbench.action.terminal.sendSequence", ` +
				`"args": {"text": "` + unicodeHome + `"}, "when": "terminalFocus"},`,
			`{"key": "cmd+right", "command": "workbench.action.terminal.sendSequence", ` +
				`"args": {"text": "` + unicodeEnd + `"}, "when": "terminalFocus"}`,
		},
		ReloadHint: "Applies as soon as you save keybindings.json.",
	},
	WezTerm: {
		ManualSteps: []string{
			"WezTerm's config is Lua (~/.config/wezterm/wezterm.lua or ~/.wezterm.lua), so " +
				"wake setup-terminal won't edit it for you - a bad append to a .lua file breaks every " +
				"binding, not just this one. Add to your config:",
			`config.keys = config.keys or {}`,
			`table.insert(config.keys, { key = "Enter", mods = "SHIFT", action = wezterm.action.SendString("\x1b\r") })`,
			`table.insert(config.keys, { key = "LeftArrow", mods = "SUPER", action = wezterm.action.SendString("\x1b[H") })`,
			`table.insert(config.keys, { key = "RightArrow", mods = "SUPER", action = wezterm.action.SendString("\x1b[F") })`,
		},
		ReloadHint: "WezTerm reloads wezterm.lua automatically.",
	},
	Unknown: {
		ManualSteps: []string{
			"Wake didn't recognise this terminal. Ctrl+J already inserts a newline with nothing to " +
				"configure. If your terminal can map a key to send raw bytes, map Shift+Enter to send " +
				`ESC then CR (0x1B 0x0D) - most terminals write that as \x1b\r or \e\r.`,
		},
	},
}

// InfoFor is what this package knows about e.
func InfoFor(e Emulator) Info {
	if info, ok := knownInfo[e]; ok {
		return info
	}
	return knownInfo[Unknown]
}
