package main

// The update notice: at most once a day, off the draw path, ask for the newest
// release and say so when it is newer than this wake. WAKE_NO_UPDATE_CHECK
// turns it off.

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

// updateCache is the last answer, beside the fleets under ~/.wake.
type updateCache struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

// checkForUpdate reports the notice when there is one. In the background, so
// a slow network never holds the room; an offline machine says nothing, since
// a failed courtesy check is not worth the notice row.
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
		tag, err := latestRelease(ctx, upgrade.GitHub, filepath.Join(root, updateCacheFile), time.Now())
		if err != nil {
			return
		}
		if text, ok := updateAvailable(tag, version.Version); ok {
			notice.Report("%s", text)
		}
	}()
}

// latestRelease is the newest tag: the kept answer while it is under a day
// old, otherwise asked for again and kept.
func latestRelease(ctx context.Context, rel releases, cachePath string, now time.Time) (string, error) {
	var kept updateCache
	if b, err := os.ReadFile(cachePath); err == nil && json.Unmarshal(b, &kept) == nil &&
		kept.Latest != "" && now.Sub(kept.Checked) < updateCheckEvery {
		return kept.Latest, nil
	}
	tag, err := rel.Latest(ctx)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(updateCache{Checked: now, Latest: tag})
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), updateCacheDirPerm); err != nil {
		return "", fmt.Errorf("keep the update check: %w", err)
	}
	if err := os.WriteFile(cachePath, b, updateCachePerm); err != nil {
		return "", fmt.Errorf("keep the update check: %w", err)
	}
	return tag, nil
}

// updateAvailable is the notice for a tag newer than current, and whether
// there is one.
func updateAvailable(tag, current string) (string, bool) {
	if !version.Newer(tag, current) {
		return "", false
	}
	return fmt.Sprintf(updateAvailableFormat, strings.TrimPrefix(tag, "v"), current), true
}
