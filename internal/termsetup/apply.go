package termsetup

// Applying the fix: writing the three plain-text configs, and refusing to
// write anything this package cannot safely round-trip. iTerm2 (a binary
// plist), VS Code (JSONC) and WezTerm (Lua) are structured formats a bad
// append can break in ways a text diff will not show; Apply never touches
// them, and neither does Undo.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// Outcome is what Apply or Undo actually did.
type Outcome int

const (
	// Wrote means the config file was created or appended to.
	Wrote Outcome = iota
	// AlreadyConfigured means the file already carries this mapping - ours,
	// or an equivalent one some other tool wrote - so nothing was written.
	AlreadyConfigured
	// ManualOnly means this terminal's config is not a format this package
	// writes; ManualSteps says what to do by hand instead.
	ManualOnly
	// Conflict means this terminal's config is normally auto-writable, but
	// this specific file's existing content makes an automatic append unsafe;
	// ManualSteps says what to do by hand instead. Alacritty's own hazard -
	// see alacrittyKeyboardTableConflict.
	Conflict
	// Removed means Undo found and removed exactly what Apply added.
	Removed
	// NothingToUndo means Undo found no marker to remove.
	NothingToUndo
	// MarkerNotRemovable means Undo found the marker but content was added
	// after the block, so it is no longer the exact suffix Apply left. Removing
	// it would risk taking a line the operator added, so Undo reports where it
	// is and lets them remove it by hand rather than guess.
	MarkerNotRemovable
)

// Result is what happened, and everything a caller needs to report it.
type Result struct {
	Emulator    Emulator
	Outcome     Outcome
	ConfigPath  string
	BackupPath  string
	ManualSteps []string
	ReloadHint  string
}

// configPerm and configDirPerm match what these terminals create for
// themselves: a readable config, nothing secret in it - unlike, say,
// parked.json, which holds session directories and is kept at 0600.
const (
	configPerm    = 0o644
	configDirPerm = 0o755
)

// Status reads whether e's config already has this mapping and whether
// Apply would refuse to write it, without writing anything - the preview
// step before a confirmation prompt. conflict is always false when
// AutoWritable is false, since Apply's own ManualOnly path never inspects
// content at all.
func Status(e Emulator, configHome string) (configured, conflict bool, path string, err error) {
	info := InfoFor(e)
	path = info.ConfigPath(configHome)
	if !info.AutoWritable {
		return false, false, path, nil
	}
	content, err := readIfExists(path)
	if err != nil {
		return false, false, path, err
	}
	configured = alreadyConfigured(content, info.Snippet)
	conflict = !configured && info.conflicts != nil && info.conflicts(content)
	return configured, conflict, path, nil
}

// Apply writes e's Shift+Enter mapping, or reports why it did not.
//
// Safe to call on a terminal this package does not write to - it returns
// ManualOnly rather than attempting anything - so a caller does not have to
// duplicate the AutoWritable check before every call.
func Apply(e Emulator, configHome string) (Result, error) {
	info := InfoFor(e)
	path := info.ConfigPath(configHome)
	if !info.AutoWritable {
		return manualResult(e, info, path), nil
	}

	content, err := readIfExists(path)
	if err != nil {
		return Result{}, err
	}
	if alreadyConfigured(content, info.Snippet) {
		return Result{Emulator: e, Outcome: AlreadyConfigured, ConfigPath: path, ReloadHint: info.ReloadHint}, nil
	}
	if info.conflicts != nil && info.conflicts(content) {
		return Result{Emulator: e, Outcome: Conflict, ConfigPath: path, ManualSteps: info.ConflictSteps}, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		return Result{}, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	var backupPath string
	if len(content) > 0 {
		backupPath, err = writeBackup(path, content)
		if err != nil {
			return Result{}, err
		}
	}

	if err := os.WriteFile(path, appendBlock(content, info.Snippet), configPerm); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", path, err)
	}
	return Result{Emulator: e, Outcome: Wrote, ConfigPath: path, BackupPath: backupPath, ReloadHint: info.ReloadHint}, nil
}

// Undo removes exactly what Apply added, leaving anything else in the file
// untouched. It refuses rather than guesses: if the marker is not there in
// the shape Apply would have left it - because Apply was never run, or
// because the file was hand-edited since - nothing is removed. It never
// restores from the .bak Apply may have left; that would silently discard
// any edit made to the file after Apply ran, which is the one thing an undo
// must not do.
func Undo(e Emulator, configHome string) (Result, error) {
	info := InfoFor(e)
	path := info.ConfigPath(configHome)
	if !info.AutoWritable {
		return manualResult(e, info, path), nil
	}

	content, err := readIfExists(path)
	if err != nil {
		return Result{}, err
	}
	trimmed, ok := removeBlock(content, info.Snippet)
	if !ok {
		if bytes.Contains(content, []byte(marker)) {
			return Result{Emulator: e, Outcome: MarkerNotRemovable, ConfigPath: path}, nil
		}
		return Result{Emulator: e, Outcome: NothingToUndo, ConfigPath: path}, nil
	}
	if err := os.WriteFile(path, trimmed, configPerm); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", path, err)
	}
	return Result{Emulator: e, Outcome: Removed, ConfigPath: path}, nil
}

func manualResult(e Emulator, info Info, path string) Result {
	return Result{Emulator: e, Outcome: ManualOnly, ConfigPath: path, ManualSteps: info.ManualSteps, ReloadHint: info.ReloadHint}
}

func readIfExists(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return content, nil
}

// writeBackup copies content to a sibling backup and never overwrites one that
// is already there. Claude Code's own /terminal-setup writes <config>.bak too,
// so a fixed name would clobber the one clean copy of a config it had just
// broken - the exact operator this verb is for. It steps .bak -> .bak.1 ->
// .bak.2 until it finds a name nothing holds.
func writeBackup(path string, content []byte) (string, error) {
	backup := path + ".bak"
	for i := 1; existsFile(backup); i++ {
		backup = fmt.Sprintf("%s.bak.%d", path, i)
	}
	if err := os.WriteFile(backup, content, configPerm); err != nil {
		return "", fmt.Errorf("back up %s: %w", path, err)
	}
	return backup, nil
}

// existsFile is whether a plain stat of path succeeds. A stat error other than
// not-exist reads as "not there", which is safe here: the write that follows
// surfaces the real error rather than this masking it.
func existsFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// alreadyConfigured is whether content already carries this mapping: our own
// marker, or the exact snippet some other tool (Claude Code's own
// /terminal-setup, or a hand edit) already wrote.
func alreadyConfigured(content []byte, snippet string) bool {
	return bytes.Contains(content, []byte(marker)) || bytes.Contains(content, []byte(snippet))
}

// alacrittyKeyboardTableConflict is whether content already defines a
// [keyboard] table. Alacritty's own docs show key bindings written two ways -
// an inline array assigned to `bindings` under [keyboard], or repeated
// [[keyboard.bindings]] array-of-table entries, which is the shape Snippet
// uses - and appending the second form after a file already using the first
// is a TOML redefinition error: `bindings` cannot be both an array value and
// an array of tables. Detecting that exactly needs a real TOML parser;
// refusing whenever the table exists at all is the conservative
// approximation. Some configs with a [keyboard] table that never touches
// `bindings` are refused unnecessarily, which costs a manual step; appending
// into one that does is a config that fails to parse, and CLAUDE.md's own
// ruling on this package is that corrupting a structured config is far worse
// than printing steps.
func alacrittyKeyboardTableConflict(content []byte) bool {
	for _, line := range bytes.Split(content, []byte("\n")) {
		if bytes.Equal(bytes.TrimSpace(line), []byte("[keyboard]")) {
			return true
		}
	}
	return false
}

// appendBlock is content with the marker and snippet added at the end, blank
// line separated from whatever was already there.
//
// Deterministic given content and snippet: removeBlock is its exact inverse
// for every content this ever produces (up to one trailing newline it may
// add to normalise a file that did not end in one - see removeBlock), which
// is what lets Undo remove precisely what was added instead of diffing.
func appendBlock(content []byte, snippet string) []byte {
	if len(content) == 0 {
		return []byte(marker + "\n" + snippet + "\n")
	}
	var b bytes.Buffer
	b.Write(content)
	if !bytes.HasSuffix(content, []byte("\n")) {
		b.WriteByte('\n')
	}
	b.WriteByte('\n') // blank line separating the added block from what was there
	b.WriteString(marker)
	b.WriteByte('\n')
	b.WriteString(snippet)
	b.WriteByte('\n')
	return b.Bytes()
}

// removeBlock is appendBlock's inverse: it recognises the exact suffix
// appendBlock would have added for this snippet and strips it, reporting
// false when content does not end that way - the file was never touched by
// Apply, or has been edited since in a way that no longer matches.
//
// One imprecision, accepted rather than hidden: content that did not end in
// "\n" comes back with one, because appendBlock normalises that before
// adding its own block and there is no record of which case it was. A
// config file is expected to end in a newline; this is not data loss.
func removeBlock(content []byte, snippet string) ([]byte, bool) {
	block := []byte(marker + "\n" + snippet + "\n")
	if bytes.Equal(content, block) {
		return []byte{}, true
	}
	withSeparator := append([]byte("\n"), block...)
	if bytes.HasSuffix(content, withSeparator) {
		return content[:len(content)-len(withSeparator)], true
	}
	return content, false
}
