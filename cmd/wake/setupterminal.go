// `wake setup-terminal`: make Shift+Enter send a newline, by configuring the
// host terminal to send ESC CR for it - the sequence bubbletea already reads
// as alt+enter and internal/ui/composer.go already binds to a newline. See
// internal/termsetup for the detection, the per-terminal knowledge and the
// file I/O; this file is the CLI shape around it.
//
// The one verb in this package that dials no socket: it never touches a
// fleet, a session or the wire, only a config file on this machine.

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DilanDoshi/wake/internal/termsetup"
)

// setupTerminalOpts is what `wake setup-terminal` was asked to do.
type setupTerminalOpts struct {
	Yes  bool
	Undo bool
}

// setupTerminalFlags parses this verb's own two flags. Separate from
// spawnFlags because these mean something on no other verb, the same
// reasoning fleetFlag's header gives for staying its own function.
func setupTerminalFlags(args []string) (setupTerminalOpts, error) {
	var opts setupTerminalOpts
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			opts.Yes = true
		case "--undo":
			opts.Undo = true
		default:
			return setupTerminalOpts{}, fmt.Errorf(
				"wake setup-terminal does not take %q; it takes --yes/-y and --undo\n\n%s", a, usage)
		}
	}
	return opts, nil
}

// runSetupTerminal detects the host terminal, shows what would change, and
// applies it or explains why it cannot.
//
// Detection reads the real process environment - the one place in this
// package that is not a pure function of a map - because this is the verb
// that answers "what terminal am I actually running in, right now".
func runSetupTerminal(args []string, in io.Reader, out io.Writer) error {
	opts, err := setupTerminalFlags(args)
	if err != nil {
		return err
	}

	env := termsetup.EnvMap(os.Environ())
	term := termsetup.Detect(env)
	configHome := termsetup.ConfigHome(env)

	mux, inMux := termsetup.DetectMultiplexer(env)
	if err := say(out, "detected: %s", term); err != nil {
		return err
	}
	if inMux {
		if err := say(out, "%s", termsetup.MultiplexerWarning(mux)); err != nil {
			return err
		}
	}

	if opts.Undo {
		// A manual-only terminal has nothing wake wrote, so Undo returns the
		// same add-steps ManualOnly result Apply does - which reads as
		// instructions to add the binding, the opposite of undo. Say the
		// undo-shaped thing here instead.
		if !termsetup.InfoFor(term).AutoWritable {
			return say(out, "nothing to undo: wake setup-terminal never writes %s's config. If you added the binding by hand, remove it where you added it.", term)
		}
		result, err := termsetup.Undo(term, configHome)
		if err != nil {
			return err
		}
		return printSetupTerminalResult(result, out)
	}
	return runSetupTerminalApply(term, configHome, opts.Yes, mux == termsetup.Cmux, in, out)
}

// runSetupTerminalApply is the non-undo half: preview, confirm, write. cmux
// says the write happened inside cmux, whose reload advice is the multiplexer
// warning above rather than Ghostty's own - so the written result's reload
// hint is dropped rather than contradicting it.
func runSetupTerminalApply(term termsetup.Emulator, configHome string, yes, cmux bool, in io.Reader, out io.Writer) error {
	info := termsetup.InfoFor(term)
	if !info.AutoWritable {
		result, err := termsetup.Apply(term, configHome)
		if err != nil {
			return err
		}
		return printSetupTerminalResult(result, out)
	}

	configured, conflict, path, err := termsetup.Status(term, configHome)
	if err != nil {
		return err
	}
	if configured {
		return say(out, "already configured at %s - nothing to do.", path)
	}
	if conflict {
		// Same result Apply would refuse with, checked first so the operator
		// is never shown a change and asked to confirm one that is about to
		// be declined anyway.
		result, err := termsetup.Apply(term, configHome)
		if err != nil {
			return err
		}
		return printSetupTerminalResult(result, out)
	}

	if err := say(out, "this will add to %s:\n\n%s\n", path, info.Snippet); err != nil {
		return err
	}
	if !yes {
		ok, err := confirmYesNo(in, out, "apply this change? [y/N] ")
		if err != nil {
			return err
		}
		if !ok {
			return say(out, "not applied.")
		}
	}

	result, err := termsetup.Apply(term, configHome)
	if err != nil {
		return err
	}
	if cmux {
		// Ghostty's "reloads automatically" is false inside cmux; the cmux
		// warning above already said to run `cmux reload-config`, so drop the
		// contradicting line rather than print both.
		result.ReloadHint = ""
	}
	return printSetupTerminalResult(result, out)
}

// printSetupTerminalResult is the one formatter for everything Apply and
// Undo can report, so the two paths cannot describe the same Outcome two
// different ways.
func printSetupTerminalResult(r termsetup.Result, out io.Writer) error {
	switch r.Outcome {
	case termsetup.ManualOnly:
		if err := say(out, "%s isn't a config wake setup-terminal writes for you:", r.Emulator); err != nil {
			return err
		}
		for _, step := range r.ManualSteps {
			if err := say(out, "%s", step); err != nil {
				return err
			}
		}
		if r.ReloadHint != "" {
			return say(out, "%s", r.ReloadHint)
		}
		return nil
	case termsetup.AlreadyConfigured:
		return say(out, "already configured at %s - nothing to do.", r.ConfigPath)
	case termsetup.Conflict:
		if err := say(out, "%s at %s already has something that makes an automatic change risky:", r.Emulator, r.ConfigPath); err != nil {
			return err
		}
		for _, step := range r.ManualSteps {
			if err := say(out, "%s", step); err != nil {
				return err
			}
		}
		return nil
	case termsetup.Wrote:
		if err := say(out, "wrote %s.", r.ConfigPath); err != nil {
			return err
		}
		if r.BackupPath != "" {
			if err := say(out, "the previous file is backed up at %s.", r.BackupPath); err != nil {
				return err
			}
		}
		if r.ReloadHint != "" {
			return say(out, "%s", r.ReloadHint)
		}
		return nil
	case termsetup.Removed:
		return say(out, "removed from %s.", r.ConfigPath)
	case termsetup.NothingToUndo:
		return say(out, "nothing to undo at %s: wake setup-terminal's own marker isn't there.", r.ConfigPath)
	case termsetup.MarkerNotRemovable:
		return say(out, "found wake setup-terminal's marker in %s, but lines were added after it, so removing it automatically would risk taking one of yours. Delete the marker comment and the line below it by hand.", r.ConfigPath)
	default:
		// Unreachable: termsetup.Outcome's seven values are all above. Stated
		// rather than silent, on main.go's own "a default that runs nothing
		// is worse than a build failure" reasoning - an eighth Outcome added
		// later and forgotten here would otherwise print nothing at all.
		return fmt.Errorf("wake: unhandled termsetup outcome %v", r.Outcome)
	}
}

// confirmYesNo asks a yes/no question on out and reads one line from in.
// Anything but exactly "y" or "yes", case-insensitively - including a typo
// like "yeah", a bare newline, or EOF - is no, which is the right default for
// a prompt guarding a file write: a prompt that took any leading "y" would
// also take "yy" pasted twice by reflex.
func confirmYesNo(in io.Reader, out io.Writer, prompt string) (bool, error) {
	if _, err := io.WriteString(out, prompt); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}
