package main

// The update check the room asks (internal/ui/updatecue.go): off the draw path,
// ask for the newest release at most once a day, say so - at most once a day -
// when it is newer than this wake, and name it for the strip's standing marker.
// WAKE_NO_UPDATE_CHECK turns it off.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/ui"
	"github.com/DilanDoshi/wake/internal/upgrade"
	"github.com/DilanDoshi/wake/internal/version"
)

const (
	noUpdateCheckEnv      = "WAKE_NO_UPDATE_CHECK"
	updateCheckEvery      = 24 * time.Hour
	updateCheckTimeout    = 10 * time.Second
	updateCacheFile       = "update-check.json"
	updateCacheDirPerm    = 0o700
	updateCachePerm       = 0o600
	updateAvailableFormat = "wake %s is out (this is %s): run `wake upgrade`"
)

// updateCache is the last answer and the last notice, beside the fleets under
// ~/.wake.
type updateCache struct {
	Checked  time.Time `json:"checked"`
	Latest   string    `json:"latest"`
	Notified time.Time `json:"notified"`
}

// updateCheck is the room's check, or nil when it is turned off. The room runs it
// as a tea.Cmd, so a slow network never holds it; an offline machine says
// nothing, since a failed courtesy check is not worth the notice row.
func updateCheck() ui.UpdateCheck {
	if os.Getenv(noUpdateCheckEnv) != "" {
		return nil
	}
	root, err := daemon.StateRoot()
	if err != nil {
		return nil
	}
	// What this process has learned, for a cache the disk will not keep. The room
	// runs one check at a time, so the calls never overlap.
	var known updateCache
	return func() string {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		// The error says nothing: a failed courtesy check is not worth the notice row.
		newer, text, _ := dueUpdateNotice(ctx, upgrade.GitHub, filepath.Join(root, updateCacheFile), &known, time.Now(), version.Version)
		if text != "" {
			notice.Report("%s", text)
		}
		return newer
	}
}

// dueUpdateNotice is the newer release, if there is one, and the notice to give
// now, or nothing. The newest tag is asked for at most once per
// updateCheckEvery, and a newer one is announced at most once per
// updateCheckEvery - both kept in cachePath, and in known for when it cannot be
// kept - but named on every call.
func dueUpdateNotice(ctx context.Context, rel releases, cachePath string, known *updateCache, now time.Time, current string) (newer, text string, err error) {
	kept := *known
	if b, err := os.ReadFile(cachePath); err == nil {
		// A torn or foreign file is an empty cache: ask again and rewrite it.
		_ = json.Unmarshal(b, &kept)
	}
	changed := false
	if kept.Latest == "" || now.Sub(kept.Checked) >= updateCheckEvery {
		tag, err := rel.Latest(ctx)
		if err != nil {
			return "", "", err
		}
		kept.Latest, kept.Checked, changed = tag, now, true
	}
	text, isNewer := updateAvailable(kept.Latest, current)
	if isNewer {
		newer = strings.TrimPrefix(kept.Latest, "v")
	}
	if isNewer && now.Sub(kept.Notified) >= updateCheckEvery {
		kept.Notified, changed = now, true
	} else {
		text = ""
	}
	*known = kept
	if changed {
		// What GitHub said still counts when it cannot be kept.
		err = keepUpdateCache(cachePath, kept)
	}
	return newer, text, err
}

func keepUpdateCache(path string, kept updateCache) error {
	b, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), updateCacheDirPerm); err != nil {
		return fmt.Errorf("keep the update check: %w", err)
	}
	if err := os.WriteFile(path, b, updateCachePerm); err != nil {
		return fmt.Errorf("keep the update check: %w", err)
	}
	return nil
}

// updateAvailable is the notice for a tag newer than current, and whether
// there is one.
func updateAvailable(tag, current string) (string, bool) {
	if !version.Newer(tag, current) {
		return "", false
	}
	return fmt.Sprintf(updateAvailableFormat, strings.TrimPrefix(tag, "v"), current), true
}
