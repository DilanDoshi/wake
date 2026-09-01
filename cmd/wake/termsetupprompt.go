// The one-time first-run offer: when this operator's terminal is one `wake
// setup-terminal` could fix and has not, say so once, ever, on this machine.

package main

import (
	"os"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/termsetup"
)

// promptTerminalSetupOnceFormat points at the verb rather than applying
// anything itself - the same "offer, never act" line every other first-run
// notice in this package already draws (managerStarted, newFleetLine).
const promptTerminalSetupOnceFormat = "%s can send Shift+Enter as a newline: run `wake setup-terminal` " +
	"(Ctrl+J already works). One-time notice."

// promptTerminalSetupOnce offers the Shift+Enter fix through the room's own
// reserved notice row - `internal/notice`, the one-line surface every other
// transient message in this build already uses - so this adds no chrome, no
// key and no geometry change of its own.
//
// It runs once, ever, per machine: `termsetup.PromptSeen` gates it and
// `MarkPromptSeen` closes the gate before the notice is shown, not after -
// on the theory that a machine which cannot durably remember it made the
// offer should not make it, and should try again next launch instead. The
// reverse order would risk the opposite failure: a marker write that fails
// after the notice already fired makes this the notice that shows on every
// `wake` from then on, which is the one thing "once, ever" rules out. Every
// read failure (Status included) is the same "leave the gate open, retry
// later" shape - cheaper than a silent "this will never offer the fix"
// failure mode, and nothing here is worth a second line competing for
// notice's single slot with the offer itself.
//
// Called from converseModel, the one place a Bubble Tea program is
// constructed - so it fires exactly once whether the operator typed bare
// `wake`, `wake new`, `wake attach` or `wake fork`, and never for `wake
// manager`, which opens no TUI at all.
func promptTerminalSetupOnce() {
	env := termsetup.EnvMap(os.Environ())
	configHome := termsetup.ConfigHome(env)
	if termsetup.PromptSeen(configHome) {
		return
	}

	term := termsetup.Detect(env)
	info := termsetup.InfoFor(term)
	if !info.AutoWritable {
		return
	}
	configured, conflict, _, err := termsetup.Status(term, configHome)
	if err != nil || configured || conflict {
		// A conflict is silent rather than a different notice: the offer
		// names a verb that would then refuse and print manual steps
		// instead of the fix it promised, which is worse than saying
		// nothing until the operator asks directly.
		return
	}

	if err := termsetup.MarkPromptSeen(configHome); err != nil {
		return
	}
	notice.Report(promptTerminalSetupOnceFormat, term)
}
