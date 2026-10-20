//go:build unix

package core

// demo/record.sh films a beat and counts the agents the take staged. The fleet's
// daemon is detached, so it outlives the recording and the script is the only
// thing that can end it. It is run over a stub `wake` and `vhs`, so nothing here
// reaches a real fleet, and without WAKE_SOCKET, which the recorder inherits.

import (
	"bytes"
	"cmp"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	stagedSocket = "/tmp/wake-demo/wake.sock"
	takeSocket   = "/tmp/wake-take.recordshtest/wake.sock"
)

type recordOpts struct {
	vhs       string // "ok", "fail", or mint:/mintfail:/minthang: and a socket the tape writes
	stages    bool   // the tape sources stage.tape
	stopExit  string // what `wake stop` exits with
	stopSleep string // how long `wake stop` takes
	stale     string // a take-socket record an earlier, killed run left
	comment   string // a line of tape that is not a Source directive
	owner     string // a record.pid an earlier run left
	noSocket  bool   // env.sh names no staged socket
}

type recordProc struct {
	cmd    *exec.Cmd
	work   string
	log    string
	stderr *bytes.Buffer
}

type recordRun struct {
	log    []string // "<socket> <verb...>", one per wake call
	stderr string
	code   int
}

func startRecord(t *testing.T, o recordOpts) *recordProc {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "demo", ".work")
	stubs := filepath.Join(root, "stubs")
	for _, dir := range []string{filepath.Join(work, "bin"), filepath.Join(root, "demo", "tapes"), stubs} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script, err := os.ReadFile("../../demo/record.sh")
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tape := "Output .work/out/beat.mp4\n"
	if o.comment != "" {
		tape += o.comment + "\n"
	}
	if o.stages {
		tape += "Source .work/stage.tape\n"
	}
	write(filepath.Join(root, "demo", "record.sh"), string(script))
	write(filepath.Join(root, "demo", "tapes", "beat.tape"), tape)
	env := "export WAKE_SOCKET=" + stagedSocket + "\n"
	if o.noSocket {
		env = "true\n"
	}
	write(filepath.Join(work, "env.sh"), env)
	if o.stale != "" {
		write(filepath.Join(work, "take-socket"), o.stale+"\n")
	}
	if o.owner != "" {
		write(filepath.Join(work, "record.pid"), o.owner+"\n")
	}
	write(filepath.Join(work, "bin", "wake"), `#!/bin/bash
echo "$WAKE_SOCKET $*" >> "$RECORD_LOG"
case "$1" in
  status) for i in 1 2 3 4 5 6 7; do echo "  agent$i <> main  idle"; done ;;
  stop) [ -z "$STUB_STOP_SLEEP" ] || sleep "$STUB_STOP_SLEEP"; echo "$WAKE_SOCKET stop-done" >> "$RECORD_LOG"; exit "$STUB_STOP_EXIT" ;;
esac
`)
	write(filepath.Join(stubs, "vhs"), `#!/bin/bash
case "$STUB_VHS" in
  ok) ;;
  fail) exit 1 ;;
  mint:*) echo "${STUB_VHS#mint:}" > "$RECORD_WORK/take-socket" ;;
  mintfail:*) echo "${STUB_VHS#mintfail:}" > "$RECORD_WORK/take-socket"; exit 1 ;;
  minthang:*) echo "${STUB_VHS#minthang:}" > "$RECORD_WORK/take-socket"; touch "$RECORD_WORK/vhs-started"; exec sleep 30 ;;
esac
`)

	p := &recordProc{work: work, log: filepath.Join(root, "wake.log"), stderr: &bytes.Buffer{}}
	p.cmd = exec.Command("bash", "record.sh", "tapes/beat.tape")
	p.cmd.Dir = filepath.Join(root, "demo")
	p.cmd.Env = []string{
		"PATH=" + stubs + ":/usr/bin:/bin",
		"RECORD_LOG=" + p.log, "RECORD_WORK=" + work,
		"STUB_VHS=" + o.vhs, "STUB_STOP_EXIT=" + cmp.Or(o.stopExit, "0"), "STUB_STOP_SLEEP=" + o.stopSleep,
		"TRIES=1",
	}
	p.cmd.Stderr = p.stderr
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL) })
	return p
}

func (p *recordProc) wait(t *testing.T) recordRun {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	var run recordRun
	select {
	case err := <-done:
		var exit *exec.ExitError
		if err != nil {
			if !errors.As(err, &exit) {
				t.Fatalf("record.sh did not run: %v", err)
			}
			run.code = exit.ExitCode()
		}
	case <-time.After(20 * time.Second):
		t.Fatal("record.sh did not finish")
	}
	run.stderr = p.stderr.String()
	run.log = p.lines()
	return run
}

func (p *recordProc) lines() []string {
	raw, err := os.ReadFile(p.log)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func record(t *testing.T, o recordOpts) recordRun {
	t.Helper()
	return startRecord(t, o).wait(t)
}

// stops is the sockets `wake stop` was run against, in order.
func (r recordRun) stops() []string {
	var out []string
	for _, line := range r.log {
		if sock, verb, ok := strings.Cut(line, " "); ok && verb == "stop" {
			out = append(out, sock)
		}
	}
	return out
}

// finished is the sockets whose `wake stop` ran to its end.
func (r recordRun) finished() []string {
	var out []string
	for _, line := range r.log {
		if sock, verb, ok := strings.Cut(line, " "); ok && verb == "stop-done" {
			out = append(out, sock)
		}
	}
	return out
}

func count(socks []string, want string) (n int) {
	for _, s := range socks {
		if s == want {
			n++
		}
	}
	return n
}

func TestARecordedTakeEndsTheFleetItStaged(t *testing.T) {
	run := record(t, recordOpts{vhs: "ok", stages: true})
	if run.code != 0 {
		t.Fatalf("record.sh exited %d: %s", run.code, run.stderr)
	}
	if got := run.stops(); len(got) != 1 || got[0] != stagedSocket {
		t.Errorf("wake stop ran against %v, want the staged fleet's %s once: its daemon is detached and outlives the recording", got, stagedSocket)
	}
	if len(run.log) < 2 || run.log[0] != stagedSocket+" status" {
		t.Errorf("the fleet was not counted before it was ended: %v", run.log)
	}
}

func TestAFailedRecordingStillEndsTheFleet(t *testing.T) {
	run := record(t, recordOpts{vhs: "fail", stages: true})
	if run.code == 0 {
		t.Error("record.sh reported success for a recording that failed")
	}
	if got := run.stops(); count(got, stagedSocket) != 1 {
		t.Errorf("a failed recording ran wake stop against %v, want %s once: the staging before the failure left a fleet", got, stagedSocket)
	}
}

// The staged socket is shared by every checkout, so a run whose tape stages
// nothing leaves it alone - another session may be filming on it.
func TestATapeThatStagesNothingLeavesTheStagedFleetAlone(t *testing.T) {
	for _, vhs := range []string{"ok", "fail", "mint:" + takeSocket} {
		run := record(t, recordOpts{vhs: vhs, comment: "# this does not source stage.tape"})
		if got := count(run.stops(), stagedSocket); got != 0 {
			t.Errorf("a tape that stages nothing (%s) stopped the shared staged fleet %d times", vhs, got)
		}
	}
}

// A tape that mints its own socket has that fleet counted and ended once, and
// ended whether vhs finished or failed.
func TestATapeThatMintedItsOwnSocketHasThatFleetEnded(t *testing.T) {
	for _, vhs := range []string{"mint:", "mintfail:"} {
		run := record(t, recordOpts{vhs: vhs + takeSocket})
		if got := count(run.stops(), takeSocket); got != 1 {
			t.Errorf("%s: wake stop ran against the take's socket %d times (%v), want once", vhs, got, run.stops())
		}
	}
}

// A run killed outright (SIGKILL cannot be trapped) leaves its record behind,
// and the next run ends that fleet before it forgets it.
func TestARecordLeftByAKilledRunIsEndedBeforeTheNextTake(t *testing.T) {
	run := record(t, recordOpts{vhs: "ok", stale: takeSocket})
	if got := count(run.stops(), takeSocket); got != 1 {
		t.Errorf("the fleet a killed run left was stopped %d times (%v), want once", got, run.stops())
	}
}

// The recorder inherits the operator's real fleet, so the script only ever
// counts or stops a socket that is scratch by what it resolves to, not by how
// it is spelled.
func TestRecordNeitherCountsNorEndsASocketThatIsNotScratch(t *testing.T) {
	link, err := os.MkdirTemp("/tmp", "wake-take.recordshlink")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })

	// A scratch directory holding a wake.sock that is itself a link.
	linked, err := os.MkdirTemp("/tmp", "wake-take.recordshfile")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(linked) })
	if err := os.Symlink("/Users/someone/.wake/fleets/default/daemon.sock", linked+"/wake.sock"); err != nil {
		t.Fatal(err)
	}

	for name, sock := range map[string]string{
		"a real fleet":          "/Users/someone/.wake/fleets/default/daemon.sock",
		"a real fleet's name":   "/tmp/wake-take.recordshtest/daemon.sock",
		"a traversal":           "/tmp/wake-demo/../../Users/someone/.wake/fleets/default/wake.sock",
		"a scratch name's link": link + "/wake.sock",
		"a linked socket file":  linked + "/wake.sock",
	} {
		run := record(t, recordOpts{vhs: "mint:" + sock})
		if run.code == 0 {
			t.Errorf("%s: record.sh accepted a take socket that is not scratch", name)
		}
		for _, line := range run.log {
			if strings.HasPrefix(line, sock+" ") {
				t.Errorf("%s: record.sh ran `wake %s` against %s", name, strings.TrimPrefix(line, sock+" "), sock)
			}
		}
	}
}

// A stop that fails is said, fails the run, and leaves the record for the exit
// hook to try again - dropping it is how a fleet gets left for good.
func TestAFleetThatWillNotEndIsSaidRetriedAndFailsTheRun(t *testing.T) {
	run := record(t, recordOpts{vhs: "ok", stages: true, stopExit: "1"})
	if !strings.Contains(run.stderr, "could not end the fleet on "+stagedSocket) || run.code == 0 {
		t.Errorf("a failed wake stop on the staged fleet: exit %d, stderr %q", run.code, run.stderr)
	}

	run = record(t, recordOpts{vhs: "mint:" + takeSocket, stopExit: "1"})
	if run.code == 0 {
		t.Error("a take's fleet that would not end still reported success")
	}
	if got := count(run.stops(), takeSocket); got < 2 {
		t.Errorf("the take's socket was stopped %d times, want a retry at exit: its record was dropped after one failure", got)
	}
}

// A second Ctrl-C while the exit hook is waiting on a graceful stop must neither
// cut the hook short nor reach the stop it is waiting on, or the fleets are left.
func TestASignalDuringCleanupDoesNotSkipTheRestOfIt(t *testing.T) {
	p := startRecord(t, recordOpts{vhs: "minthang:" + takeSocket, stages: true, stopSleep: "1"})
	waitFor(t, func() bool { _, err := os.Stat(filepath.Join(p.work, "vhs-started")); return err == nil })
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGINT) // ends the recording; cleanup begins
	waitFor(t, func() bool { return count(recordRun{log: p.lines()}.stops(), takeSocket) > 0 })
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGINT) // the impatient second one

	run := p.wait(t)
	for _, sock := range []string{takeSocket, stagedSocket} {
		if count(run.finished(), sock) != 1 {
			t.Errorf("a signal during cleanup left the fleet on %s: stops %v, finished %v", sock, run.stops(), run.finished())
		}
	}
}

// Two recorders in one checkout share the take record and the staged socket, so
// a second one stops nobody: it refuses, and runs no cleanup for a run it does
// not own. A record left by a recorder that is gone is recovered.
func TestARecorderRefusesWhileAnotherIsRunningAndRecoversADeadOnes(t *testing.T) {
	run := record(t, recordOpts{vhs: "ok", stages: true, stale: takeSocket, owner: strconv.Itoa(os.Getpid())})
	if run.code == 0 || len(run.log) != 0 || !strings.Contains(run.stderr, "another recording is running") {
		t.Errorf("with a live recorder: exit %d, wake calls %v, stderr %q; want a refusal that touches no fleet", run.code, run.log, run.stderr)
	}

	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	run = record(t, recordOpts{vhs: "ok", stages: true, owner: strconv.Itoa(gone.Process.Pid)})
	if run.code != 0 || count(run.stops(), stagedSocket) != 1 {
		t.Errorf("with a dead recorder's record: exit %d, stops %v: %s", run.code, run.stops(), run.stderr)
	}
}

// A record that names nothing - an empty take record, an env.sh with no socket -
// is a failure to end the fleet, never a fleet that was ended.
func TestARecordThatNamesNothingFailsTheRun(t *testing.T) {
	run := record(t, recordOpts{vhs: "mint:"})
	if run.code == 0 || !strings.Contains(run.stderr, "take-socket is empty") {
		t.Errorf("an empty take record: exit %d, stderr %q", run.code, run.stderr)
	}
	run = record(t, recordOpts{vhs: "ok", stages: true, noSocket: true})
	if run.code == 0 || !strings.Contains(run.stderr, "no socket to end a fleet on") {
		t.Errorf("an env.sh with no socket: exit %d, stderr %q", run.code, run.stderr)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out waiting for the script")
}
