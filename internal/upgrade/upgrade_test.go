package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const testTag = "v9.9.9"

// fakeRelease is GitHub's release surface for one tag: /releases/latest
// redirects to the tag, and the tag's download directory holds this
// platform's archive and checksums.txt.
type fakeRelease struct {
	archive []byte
	sums    string
}

func tarball(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{{"LICENSE", []byte("license")}, {name, body}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func release(t *testing.T, binary []byte) fakeRelease {
	t.Helper()
	archive := tarball(t, binaryName, binary)
	sum := sha256.Sum256(archive)
	return fakeRelease{archive: archive, sums: fmt.Sprintf("%s  %s\n%s  other.tar.gz\n",
		hex.EncodeToString(sum[:]), AssetName(testTag, runtime.GOOS, runtime.GOARCH), strings.Repeat("0", 64))}
}

func serve(t *testing.T, r fakeRelease) Releases {
	t.Helper()
	dl := "/releases/download/" + testTag + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/releases/latest":
			http.Redirect(w, req, "/releases/tag/"+testTag, http.StatusFound)
		case "/releases/tag/" + testTag:
			_, _ = fmt.Fprint(w, "the release page")
		case dl + checksumsName:
			_, _ = fmt.Fprint(w, r.sums)
		case dl + AssetName(testTag, runtime.GOOS, runtime.GOARCH):
			_, _ = w.Write(r.archive)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return Releases{Base: srv.URL, Client: srv.Client()}
}

func TestLatestReadsTheTagOffGitHubsRedirect(t *testing.T) {
	rel := serve(t, release(t, []byte("new")))
	tag, err := rel.Latest(context.Background())
	if err != nil || tag != testTag {
		t.Errorf("Latest = %q, %v; want %q", tag, err, testTag)
	}
}

func TestInstallVerifiesAndReplacesTheBinaryInPlace(t *testing.T) {
	rel := serve(t, release(t, []byte("new build")))
	dest := filepath.Join(t.TempDir(), "wake")
	if err := os.WriteFile(dest, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := rel.Install(context.Background(), testTag, dest); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "new build" {
		t.Errorf("dest = %q, %v", got, err)
	}
	if info, err := os.Stat(dest); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("dest is not executable: %v, %v", info.Mode(), err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".wake-upgrade-*")); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// A download that does not match checksums.txt never touches the binary.
func TestInstallRefusesAnArchiveThatDoesNotMatchItsChecksum(t *testing.T) {
	r := release(t, []byte("new build"))
	r.archive = tarball(t, binaryName, []byte("tampered"))
	rel := serve(t, r)
	dest := filepath.Join(t.TempDir(), "wake")
	if err := os.WriteFile(dest, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := rel.Install(context.Background(), testTag, dest)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Install = %v, want a checksum refusal", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "old build" {
		t.Errorf("a refused install changed the binary to %q", got)
	}
}

func TestInstallSaysWhenThisPlatformHasNoBuild(t *testing.T) {
	r := release(t, []byte("new build"))
	r.sums = strings.Repeat("0", 64) + "  other.tar.gz\n"
	rel := serve(t, r)
	err := rel.Install(context.Background(), testTag, filepath.Join(t.TempDir(), "wake"))
	if err == nil || !strings.Contains(err.Error(), runtime.GOOS+"_"+runtime.GOARCH) {
		t.Errorf("Install = %v, want it to name this platform", err)
	}
}

// The asset name is goreleaser's name_template, spelled a second time here and
// a third in scripts/install.sh; this holds all three to the config.
func TestAssetNameMatchesTheReleaseConfig(t *testing.T) {
	config, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	const want = `name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"`
	if !strings.Contains(string(config), want) {
		t.Fatalf(".goreleaser.yaml no longer names archives %s", want)
	}
	if got := AssetName("v0.1.5", "darwin", "arm64"); got != "wake_0.1.5_darwin_arm64.tar.gz" {
		t.Errorf("AssetName = %q", got)
	}
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`wake_\$\{version\}_\$\{os\}_\$\{arch\}\.tar\.gz`).Match(script) {
		t.Errorf("scripts/install.sh does not build the asset name as wake_${version}_${os}_${arch}.tar.gz")
	}
}

// releaseOf is a release whose checksums vouch for exactly this archive, so a
// test can hand Install a well-signed archive that is wrong in some other way.
func releaseOf(archive []byte) fakeRelease {
	sum := sha256.Sum256(archive)
	return fakeRelease{archive: archive,
		sums: hex.EncodeToString(sum[:]) + "  " + AssetName(testTag, runtime.GOOS, runtime.GOARCH) + "\n"}
}

func existingBinary(t *testing.T) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "wake")
	if err := os.WriteFile(dest, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dest
}

func assertUntouched(t *testing.T, dest string) {
	t.Helper()
	if got, _ := os.ReadFile(dest); string(got) != "old build" {
		t.Errorf("a failed install changed the binary to %q", got)
	}
}

func TestLatestRefusesAnAnswerThatNamesNoRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "no redirect here")
	}))
	t.Cleanup(srv.Close)
	if tag, err := (Releases{Base: srv.URL, Client: srv.Client()}).Latest(context.Background()); err == nil {
		t.Errorf("Latest = %q with no redirect to a tag", tag)
	}
}

func TestInstallRefusesAWellSignedArchiveWithNoWakeInIt(t *testing.T) {
	rel := serve(t, releaseOf(tarball(t, "LICENSE.txt", []byte("just a license"))))
	dest := existingBinary(t)
	if err := rel.Install(context.Background(), testTag, dest); err == nil || !strings.Contains(err.Error(), "no wake") {
		t.Errorf("Install = %v, want a refusal naming the missing binary", err)
	}
	assertUntouched(t, dest)
}

func TestInstallRefusesAnArchiveThatIsNotOne(t *testing.T) {
	rel := serve(t, releaseOf([]byte("not gzip at all")))
	dest := existingBinary(t)
	if err := rel.Install(context.Background(), testTag, dest); err == nil {
		t.Error("Install accepted an archive that does not open")
	}
	assertUntouched(t, dest)
}

func TestInstallSaysWhichDownloadWasMissing(t *testing.T) {
	r := release(t, []byte("new build"))
	rel := serve(t, r)
	rel.Base += "/elsewhere"
	err := rel.Install(context.Background(), testTag, existingBinary(t))
	if err == nil || !strings.Contains(err.Error(), checksumsName) || !strings.Contains(err.Error(), "404") {
		t.Errorf("Install = %v, want it to name %s and the 404", err, checksumsName)
	}
}

func TestInstallIntoADirectoryItCannotWriteChangesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes through directory permissions")
	}
	rel := serve(t, release(t, []byte("new build")))
	dest := existingBinary(t)
	dir := filepath.Dir(dest)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := rel.Install(context.Background(), testTag, dest); err == nil || !strings.Contains(err.Error(), "writable") {
		t.Errorf("Install = %v, want it to say the directory is not writable", err)
	}
	assertUntouched(t, dest)
}

// An entry larger than the bound is refused, never copied up to the bound and
// installed truncated: the archive's checksum vouches for the archive, not for
// what a short copy of it would be.
func TestInstallRefusesABinaryLargerThanTheBound(t *testing.T) {
	bound := maxBinaryBytes
	maxBinaryBytes = 4
	t.Cleanup(func() { maxBinaryBytes = bound })
	rel := serve(t, release(t, []byte("far more than four bytes")))
	dest := existingBinary(t)
	if err := rel.Install(context.Background(), testTag, dest); err == nil {
		t.Error("Install accepted a binary larger than the bound")
	}
	assertUntouched(t, dest)
}
