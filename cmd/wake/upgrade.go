package main

// `wake upgrade`: replace this binary with the newest release, and name the
// running fleets, which keep the build they started from until they restart.

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/upgrade"
	"github.com/DilanDoshi/wake/internal/version"
)

// cmdUpgrade touches no fleet, like setup-terminal: it is handled before a
// fleet directory is resolved.
const cmdUpgrade = "upgrade"

const (
	alreadyLatestFormat = "wake %s is the latest release."
	upgradedFormat      = "Upgraded %s from wake %s to %s."
	restartFleetsFormat = "Running fleets keep the old build until they restart: %s. " +
		"⌃Q⌃Q each, then `wake --fleet <name>`."
)

// releases is the release host: upgrade.GitHub, or a test's fake.
type releases interface {
	Latest(ctx context.Context) (string, error)
	Install(ctx context.Context, tag, dest string) error
}

func runUpgrade(args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("%q takes no arguments\n\n%s", cmdUpgrade, usage)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate this wake: %w", err)
	}
	// Through any symlink, so the file replaced is the binary itself.
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return fmt.Errorf("locate this wake: %w", err)
	}
	return upgradeWake(context.Background(), upgrade.GitHub, exe, out)
}

func upgradeWake(ctx context.Context, rel releases, exe string, out io.Writer) error {
	tag, err := rel.Latest(ctx)
	if err != nil {
		return err
	}
	if !version.Newer(tag, version.Version) {
		return say(out, alreadyLatestFormat, version.Version)
	}
	if err := rel.Install(ctx, tag, exe); err != nil {
		return err
	}
	if err := say(out, upgradedFormat, exe, version.Version, strings.TrimPrefix(tag, "v")); err != nil {
		return err
	}
	names, err := daemon.Fleets()
	if err != nil {
		return err
	}
	running := slices.Sorted(maps.Keys(daemon.RunningBuilds(names)))
	if len(running) == 0 {
		return nil
	}
	return say(out, restartFleetsFormat, strings.Join(running, ", "))
}
