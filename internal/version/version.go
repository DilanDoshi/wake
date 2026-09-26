// Package version is what this binary is: the release number a person reads
// and the build identity a daemon and a client compare. A leaf, so the daemon,
// the TUI and the verbs all read one answer.
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
)

// Version is the release number. A var, not a const, so a release build stamps
// it from the git tag with -ldflags -X (see .goreleaser.yaml); the default is
// what a plain `go build` reports, kept in step with the last tag by hand.
var Version = "0.1.5"

// shortRevision is how much of a commit hash a build names.
const shortRevision = 7

// Build names this exact binary. Two binaries with one Build run the same code,
// so it is what a client compares against its daemon's. Worked out once, so a
// daemon reports the binary it started as even after the file is replaced.
var Build = sync.OnceValue(func() string {
	info, _ := debug.ReadBuildInfo()
	return build(Version, info, executableDigest)
})

// build prefers the commit Go stamped from a checkout, then the module version
// `go install pkg@ref` records, then the bare release number. A build from an
// uncommitted tree also names a digest of its executable, since two of those
// from one commit can run different code.
func build(version string, info *debug.BuildInfo, digest func() string) string {
	if info == nil {
		return version
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev != "" {
		b := version + "+" + rev[:min(len(rev), shortRevision)]
		if dirty {
			b += "-dirty"
			if d := digest(); d != "" {
				b += "." + d
			}
		}
		return b
	}
	if m := info.Main.Version; m != "" && m != "(devel)" {
		return strings.TrimPrefix(m, "v")
	}
	return version
}

// executableDigest is a short sha256 of this process's executable, or nothing
// when it cannot be read - which leaves the build as "-dirty" alone.
func executableDigest() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:shortRevision]
}

// Newer reports whether release latest is after current. Both are plain
// MAJOR.MINOR.PATCH with an optional leading v; anything else - a pre-release
// included - is never newer, so a malformed tag cannot nag anybody.
func Newer(latest, current string) bool {
	l, ok := parse(latest)
	if !ok {
		return false
	}
	c, ok := parse(current)
	if !ok {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != len(out) {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
