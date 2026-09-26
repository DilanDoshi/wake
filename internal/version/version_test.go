package version

import (
	"runtime/debug"
	"testing"
)

func info(main string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Version: main}, Settings: settings}
}

func vcs(rev, modified string) []debug.BuildSetting {
	return []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: modified}}
}

// A build names the commit when Go recorded one, because two builds of one
// release number from different commits run different code - which is exactly
// what a daemon and a client comparing builds must be able to see.
func TestBuildNamesTheCommitAndWhetherTheTreeWasDirty(t *testing.T) {
	cases := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"checkout build", info("(devel)", vcs("44a0a92c0ffee", "false")...), "0.1.5+44a0a92"},
		{"dirty checkout", info("(devel)", vcs("44a0a92c0ffee", "true")...), "0.1.5+44a0a92-dirty"},
		{"go install @tag", info("v0.1.5"), "0.1.5"},
		{"go install @main", info("v0.1.6-0.20260925010203-abcdefabcdef"), "0.1.6-0.20260925010203-abcdefabcdef"},
		{"no build info", nil, "0.1.5"},
		{"devel without vcs", info("(devel)"), "0.1.5"},
	}
	for _, c := range cases {
		if got := build("0.1.5", c.info); got != c.want {
			t.Errorf("%s: build = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNewerComparesReleaseNumbersNotStrings(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.1.6", "0.1.5", true},
		{"v0.1.10", "0.1.9", true},
		{"v0.2.0", "0.1.12", true},
		{"v1.0.0", "0.9.9", true},
		{"v0.1.5", "0.1.5", false},
		{"v0.1.4", "0.1.5", false},
		{"0.1.6", "v0.1.5", true},
		{"garbage", "0.1.5", false},
		{"v0.1.6", "garbage", false},
		{"v0.1.6-rc1", "0.1.5", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
