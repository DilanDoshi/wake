// Package upgrade replaces this wake binary with a published release: the
// newest tag off GitHub's /releases/latest redirect, the archive for this
// platform, checked against the release's checksums.txt, renamed into place.
package upgrade

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// gitHubReleases is the repository every published build lives under.
	gitHubReleases = "https://github.com/DilanDoshi/wake"

	// downloadTimeout bounds a whole archive download, which is tens of MB.
	downloadTimeout = 5 * time.Minute

	checksumsName = "checksums.txt"
	binaryName    = "wake"
	tagPrefix     = "v"

	// maxBinaryBytes bounds what one archive entry may write: far above a real
	// build, far below a disk.
	maxBinaryBytes = 512 << 20
)

// Releases is where builds are published: GitHub, or a test server.
type Releases struct {
	Base   string
	Client *http.Client
}

// GitHub is the published releases.
var GitHub = Releases{Base: gitHubReleases, Client: &http.Client{Timeout: downloadTimeout}}

// AssetName is a release archive's file name, as .goreleaser.yaml's
// name_template spells it.
func AssetName(tag, goos, goarch string) string {
	return fmt.Sprintf("wake_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, tagPrefix), goos, goarch)
}

// Latest is the newest release's tag, read off the redirect /releases/latest
// answers with - no API call, so no API rate limit.
func (r Releases) Latest(ctx context.Context) (string, error) {
	client := *r.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Base+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ask for the latest release: %w", err)
	}
	_ = resp.Body.Close()
	tag := path.Base(resp.Header.Get("Location"))
	if resp.StatusCode/100 != 3 || !strings.HasPrefix(tag, tagPrefix) {
		return "", fmt.Errorf("ask for the latest release: got %s with no release tag", resp.Status)
	}
	return tag, nil
}

// Install downloads tag's build for this platform, checks it against the
// release's checksums, and renames it over dest. dest is untouched on any
// failure.
func (r Releases) Install(ctx context.Context, tag, dest string) error {
	asset := AssetName(tag, runtime.GOOS, runtime.GOARCH)
	sums, err := r.get(ctx, tag, checksumsName)
	if err != nil {
		return err
	}
	want, ok := checksumFor(sums, asset)
	if !ok {
		return fmt.Errorf("release %s has no build for %s_%s", tag, runtime.GOOS, runtime.GOARCH)
	}
	archive, err := r.get(ctx, tag, asset)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s does not match the release's checksum; nothing was replaced", asset)
	}
	return replace(archive, dest)
}

func (r Releases) get(ctx context.Context, tag, name string) ([]byte, error) {
	url := r.Base + "/releases/download/" + tag + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", name, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	return body, nil
}

// checksumFor finds asset's line in a `sha256sum`-format listing.
func checksumFor(sums []byte, asset string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[1] == asset {
			return f[0], true
		}
	}
	return "", false
}

// replace writes the archive's wake beside dest and renames it over dest, so
// the running binary is swapped whole or not at all.
func replace(archive []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("read the release archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the release archive has no wake binary in it")
		}
		if err != nil {
			return fmt.Errorf("read the release archive: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && hdr.Name == binaryName {
			return writeOver(tr, dest)
		}
	}
}

func writeOver(src io.Reader, dest string) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".wake-upgrade-*")
	if err != nil {
		return fmt.Errorf("write beside %s (is its directory writable?): %w", dest, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := io.Copy(tmp, io.LimitReader(src, maxBinaryBytes)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write the new wake: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("make the new wake executable: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write the new wake: %w", err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("replace %s: %w", dest, err)
	}
	return nil
}
