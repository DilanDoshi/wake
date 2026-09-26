package main

// The update notice: off the draw path, ask for the newest release at most once
// a day, and say so - at most once a day - when it is newer than this wake.
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

// checkForUpdate reports the notice when one is due. In the background, so a
// slow network never holds the room; an offline machine says nothing, since a
// failed courtesy check is not worth the notice row.
func checkForUpdate() {
	if os.Getenv(noUpdateCheckEnv) != "" {
		return
	}
	root, err := daemon.StateRoot()
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		text, err := dueUpdateNotice(ctx, upgrade.GitHub, filepath.Join(root, updateCacheFile), time.Now(), version.Version)
		if err == nil && text != "" {
			notice.Report("%s", text)
		}
	}()
}

// dueUpdateNotice is the notice to give now, or nothing. The newest tag is
// asked for at most once per updateCheckEvery, and a newer one is announced at
// most once per updateCheckEvery - both kept in cachePath.
func dueUpdateNotice(ctx context.Context, rel releases, cachePath string, now time.Time, current string) (string, error) {
	var kept updateCache
	if b, err := os.ReadFile(cachePath); err == nil {
		// A torn or foreign file is an empty cache: ask again and rewrite it.
		_ = json.Unmarshal(b, &kept)
	}
	changed := false
	if kept.Latest == "" || now.Sub(kept.Checked) >= updateCheckEvery {
		tag, err := rel.Latest(ctx)
		if err != nil {
			return "", err
		}
		kept.Latest, kept.Checked, changed = tag, now, true
	}
	text, newer := updateAvailable(kept.Latest, current)
	due := newer && now.Sub(kept.Notified) >= updateCheckEvery
	if due {
		kept.Notified, changed = now, true
	}
	if changed {
		if err := keepUpdateCache(cachePath, kept); err != nil {
			return "", err
		}
	}
	if !due {
		return "", nil
	}
	return text, nil
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
