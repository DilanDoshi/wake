package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/upgrade"
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

// The check asks GitHub at most once a day, and the notice is given at most
// once a day however many rooms open - it shares one row with notices that
// matter more, like a stale daemon.
func TestTheUpdateNoticeIsCheckedAndGivenAtMostOnceADay(t *testing.T) {
	cache := filepath.Join(t.TempDir(), updateCacheFile)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	rel := &fakeReleases{latest: "v99.0.0"}

	for i, step := range []struct {
		at     time.Time
		notice bool
	}{{now, true}, {now.Add(time.Hour), false}, {now.Add(updateCheckEvery + time.Minute), true}} {
		newer, text, err := dueUpdateNotice(context.Background(), rel, cache, &updateCache{}, step.at, version.Version)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if (text != "") != step.notice {
			t.Errorf("open %d: notice %q, want one: %v", i, text, step.notice)
		}
		// The marker does not wait on the notice's day: a newer release is named
		// on every check, whether or not the line is due.
		if newer != "99.0.0" {
			t.Errorf("open %d: newer %q, want 99.0.0", i, newer)
		}
	}
	if rel.asked != 2 {
		t.Errorf("asked GitHub %d times over a day and a minute, want 2", rel.asked)
	}
	rel.err = errors.New("offline")
	if newer, text, err := dueUpdateNotice(context.Background(), rel, cache, &updateCache{}, now.Add(3*updateCheckEvery), version.Version); err == nil || text != "" || newer != "" {
		t.Errorf("an offline check: %q, %q, %v", newer, text, err)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Errorf("the answer was not kept: %v", err)
	}
}

// The current release names nothing, notice or marker.
func TestTheUpdateCheckNamesNothingForTheCurrentRelease(t *testing.T) {
	cache := filepath.Join(t.TempDir(), updateCacheFile)
	rel := &fakeReleases{latest: "v" + version.Version}
	newer, text, err := dueUpdateNotice(context.Background(), rel, cache, &updateCache{}, time.Now(), version.Version)
	if err != nil || newer != "" || text != "" {
		t.Errorf("the current release: newer %q, notice %q, %v", newer, text, err)
	}
}

// WAKE_NO_UPDATE_CHECK (set for this package by TestMain) hands the room no
// check at all, so it never asks and never draws the marker.
func TestTheRoomGetsNoUpdateCheckWhenItIsTurnedOff(t *testing.T) {
	if updateCheck() != nil {
		t.Errorf("%s is set and the room still got a check", noUpdateCheckEnv)
	}
	t.Setenv(noUpdateCheckEnv, "")
	if updateCheck() == nil {
		t.Errorf("with %s unset the room got no check", noUpdateCheckEnv)
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

// The check the room runs, end to end: the release host's redirect, the cache
// under this HOME, the once-a-day notice and the version the strip names.
func TestTheRoomsCheckNamesTheNewerReleaseAndGivesTheNotice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v99.0.0", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	host := upgrade.GitHub
	upgrade.GitHub = upgrade.Releases{Base: srv.URL, Client: srv.Client()}
	t.Cleanup(func() { upgrade.GitHub = host })
	t.Setenv("HOME", t.TempDir())
	t.Setenv(noUpdateCheckEnv, "")
	notice.Reset()
	t.Cleanup(notice.Reset)

	check := updateCheck()
	if check == nil {
		t.Fatal("no check with the off switch unset")
	}
	if got := check(); got != "99.0.0" {
		t.Errorf("the check named %q, want 99.0.0", got)
	}
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.Text, "wake 99.0.0 is out") {
		t.Errorf("no notice for the newer release: %+v", n)
	}
	// Asked again within the day: still named, not announced twice.
	if got := check(); got != "99.0.0" {
		t.Errorf("a second check named %q, want 99.0.0", got)
	}
	if n, _ := notice.Latest(); n.Count != 1 {
		t.Errorf("the notice was given %d times in a day, want once", n.Count)
	}
}

// What GitHub said still counts when the cache cannot be kept - an unwritable
// ~/.wake loses only the once-a-day bookkeeping, never the release.
func TestACacheThatCannotBeKeptStillNamesTheRelease(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	newer, text, err := dueUpdateNotice(context.Background(), &fakeReleases{latest: "v99.0.0"},
		filepath.Join(blocker, updateCacheFile), &updateCache{}, time.Now(), version.Version)
	if err == nil {
		t.Fatal("baseline: a cache under a file was kept")
	}
	if newer != "99.0.0" || text == "" {
		t.Errorf("an unkeepable cache lost the answer: newer %q, notice %q", newer, text)
	}
}

// A cache the disk will not keep is kept by the process: one room asks GitHub
// about once a day, not on every hour of typing, and gives the notice once.
func TestAnUnkeepableCacheStillAsksAboutOnceADay(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(blocker, updateCacheFile)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	for _, latest := range []string{"v" + version.Version, "v99.0.0"} {
		rel := &fakeReleases{latest: latest}
		mem := &updateCache{}
		notices := 0
		for _, at := range []time.Duration{0, time.Hour, 2 * time.Hour, 23 * time.Hour} {
			if _, text, _ := dueUpdateNotice(context.Background(), rel, cache, mem, now.Add(at), version.Version); text != "" {
				notices++
			}
		}
		if rel.asked != 1 {
			t.Errorf("latest %s: asked GitHub %d times in a day with an unkeepable cache, want 1", latest, rel.asked)
		}
		if want := map[bool]int{true: 1, false: 0}[latest == "v99.0.0"]; notices != want {
			t.Errorf("latest %s: %d notices in a day, want %d", latest, notices, want)
		}
		if _, _, err := dueUpdateNotice(context.Background(), rel, cache, mem, now.Add(updateCheckEvery+time.Minute), version.Version); err == nil {
			t.Errorf("latest %s: the unkeepable cache was kept", latest)
		}
		if rel.asked != 2 {
			t.Errorf("latest %s: a day on, asked %d times in all, want 2", latest, rel.asked)
		}
	}
}
