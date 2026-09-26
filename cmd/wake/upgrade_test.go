package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/version"
)

// fakeReleases is a release host: the newest tag, and a record of installs.
type fakeReleases struct {
	latest    string
	err       error
	asked     int
	installed []string
}

func (f *fakeReleases) Latest(context.Context) (string, error) {
	f.asked++
	return f.latest, f.err
}

func (f *fakeReleases) Install(_ context.Context, tag, dest string) error {
	f.installed = append(f.installed, tag+" "+dest)
	return nil
}

func TestUpgradeInstallsANewerReleaseOverThisBinary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rel := &fakeReleases{latest: "v99.0.0"}
	var out strings.Builder
	if err := upgradeWake(context.Background(), rel, "/opt/bin/wake", &out); err != nil {
		t.Fatalf("upgradeWake: %v", err)
	}
	if len(rel.installed) != 1 || rel.installed[0] != "v99.0.0 /opt/bin/wake" {
		t.Errorf("installed %v", rel.installed)
	}
	if !strings.Contains(out.String(), "99.0.0") || !strings.Contains(out.String(), version.Version) {
		t.Errorf("upgrade said %q", out.String())
	}
}

func TestUpgradeLeavesTheLatestReleaseAlone(t *testing.T) {
	rel := &fakeReleases{latest: "v" + version.Version}
	var out strings.Builder
	if err := upgradeWake(context.Background(), rel, "/opt/bin/wake", &out); err != nil {
		t.Fatalf("upgradeWake: %v", err)
	}
	if len(rel.installed) != 0 || !strings.Contains(out.String(), "latest") {
		t.Errorf("installed %v, said %q", rel.installed, out.String())
	}
}

// Every fleet running when the binary is replaced was started from the old
// one, so the upgrade names them all and how to move each over.
func TestUpgradeNamesTheRunningFleetsThatKeepTheOldBuild(t *testing.T) {
	// Short, so the fleet's socket path under it can be bound.
	t.Setenv("HOME", filepath.Dir(tempSocket(t)))
	t.Setenv(daemon.SocketEnv, "")
	socket, err := daemon.FleetSocketPath("canyon")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- daemon.Serve(ctx, socket) }()
	t.Cleanup(func() { cancel(); <-served })
	// Held open for the test: a daemon with no agents quits when its last
	// client leaves, and a real fleet has agents or a room holding it.
	for deadline := time.Now().Add(testTimeout); ; time.Sleep(10 * time.Millisecond) {
		if conn, err := daemon.Dial(socket); err == nil {
			t.Cleanup(func() { _ = conn.Close() })
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fleet's daemon never listened")
		}
	}

	var out strings.Builder
	if err := upgradeWake(context.Background(), &fakeReleases{latest: "v99.0.0"}, "/opt/bin/wake", &out); err != nil {
		t.Fatalf("upgradeWake: %v", err)
	}
	if !strings.Contains(out.String(), "canyon") || !strings.Contains(out.String(), "⌃Q⌃Q") {
		t.Errorf("upgrade did not name the running fleet:\n%s", out.String())
	}
}

func TestUpgradeTakesNoArguments(t *testing.T) {
	if err := run([]string{cmdUpgrade, "now"}, &strings.Builder{}); err == nil {
		t.Error("wake upgrade now was accepted")
	}
}

// The check asks GitHub at most once a day: a fresh answer on disk is used as
// is, a stale or missing one is asked for again and kept.
func TestTheUpdateCheckAsksAtMostOnceADay(t *testing.T) {
	cache := filepath.Join(t.TempDir(), updateCacheFile)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	rel := &fakeReleases{latest: "v0.2.0"}

	for i, at := range []time.Time{now, now.Add(time.Hour), now.Add(updateCheckEvery + time.Minute)} {
		tag, err := latestRelease(context.Background(), rel, cache, at)
		if err != nil || tag != "v0.2.0" {
			t.Fatalf("check %d: %q, %v", i, tag, err)
		}
	}
	if rel.asked != 2 {
		t.Errorf("asked GitHub %d times over a day and a minute, want 2", rel.asked)
	}
	rel.err = errors.New("offline")
	if _, err := latestRelease(context.Background(), rel, cache, now.Add(3*updateCheckEvery)); err == nil {
		t.Error("an offline check reported an answer")
	}
	if _, err := os.Stat(cache); err != nil {
		t.Errorf("the answer was not kept: %v", err)
	}
}

func TestTheUpdateNoticeNamesOnlyANewerRelease(t *testing.T) {
	if text, ok := updateAvailable("v99.0.0", version.Version); !ok || !strings.Contains(text, "wake upgrade") {
		t.Errorf("a newer release: %q, %v", text, ok)
	}
	if _, ok := updateAvailable("v"+version.Version, version.Version); ok {
		t.Error("the current release was announced as an update")
	}
}
