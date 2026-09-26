package main

// A daemon keeps running the binary it started from, so an upgrade on disk
// leaves every running fleet on the old code. The room says so when it opens.

import (
	"fmt"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/version"
)

const (
	staleDaemonFormat = "this fleet's daemon runs wake %s and this is wake %s: ⌃Q⌃Q, then reopen the fleet, to run it on this build"
	// preBuildDaemon stands in for the build a daemon from before builds were
	// reported did not send.
	preBuildDaemon = "an older build"
)

// staleDaemonNotice is the line for a seed whose daemon runs another build
// than ours, and whether there is one.
func staleDaemonNotice(seed *rpc.Status, ours string) (string, bool) {
	if seed == nil || !seed.Running || seed.Build == ours {
		return "", false
	}
	return fmt.Sprintf(staleDaemonFormat, daemonBuild(seed.Build), ours), true
}

// daemonBuild is a reported build as a reader sees it.
func daemonBuild(build string) string {
	if build == "" {
		return preBuildDaemon
	}
	return build
}

// warnIfStaleDaemon reports staleDaemonNotice's line when there is one.
func warnIfStaleDaemon(seed *rpc.Status) {
	if text, stale := staleDaemonNotice(seed, version.Build()); stale {
		notice.Report("%s", text)
	}
}
