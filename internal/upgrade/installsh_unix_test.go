//go:build unix

package upgrade

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// installScript is the one-line installer, run the way `curl ... | sh` runs it.
var installScript = filepath.Join("..", "..", "scripts", "install.sh")

// scriptPrompt is how long a test waits for the installer to ask a question.
const scriptPrompt = 10 * time.Second

// installEnv is a machine with no claude, no wake and a zsh login, pointed at
// a fake release host.
func installEnv(t *testing.T, rel Releases) (env []string, home, dir string) {
	t.Helper()
	home = t.TempDir()
	dir = filepath.Join(home, ".local", "bin")
	return []string{
		"HOME=" + home, "SHELL=/bin/zsh", "PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"WAKE_RELEASES=" + rel.Base, "WAKE_INSTALL_DIR=" + dir,
	}, home, dir
}

// runDetached runs the installer with no controlling terminal, which is what
// a CI box or a piped, non-interactive shell gives it: nothing may prompt.
func runDetached(t *testing.T, env []string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", installScript)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestInstallScriptInstallsTheLatestBuildAndSaysWhatIsMissing(t *testing.T) {
	env, home, dir := installEnv(t, serve(t, release(t, []byte("new build"))))
	out, err := runDetached(t, env)
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	if got, err := os.ReadFile(filepath.Join(dir, binaryName)); err != nil || string(got) != "new build" {
		t.Errorf("installed %q, %v", got, err)
	}
	for _, want := range []string{"9.9.9", `export PATH="` + dir + `:$PATH"`, ".zshrc", "code.claude.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Errorf("a run with nobody to ask edited ~/.zshrc (stat: %v)", err)
	}
}

func TestInstallScriptRefusesAMismatchedArchive(t *testing.T) {
	r := release(t, []byte("new build"))
	r.archive = tarball(t, binaryName, []byte("tampered"))
	env, _, dir := installEnv(t, serve(t, r))
	out, err := runDetached(t, env)
	if err == nil || !strings.Contains(out, "checksum") {
		t.Fatalf("install.sh = %v, want a checksum refusal:\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, binaryName)); !os.IsNotExist(err) {
		t.Errorf("a refused archive was installed (stat: %v)", err)
	}
}

// Asked at a terminal, the installer adds the directory to the shell's rc
// file - once: a second install finds the line and does not ask again.
func TestInstallScriptAddsTheDirectoryToPathWhenAskedAtATerminal(t *testing.T) {
	env, home, dir := installEnv(t, serve(t, release(t, []byte("new build"))))
	rc := filepath.Join(home, ".zshrc")
	line := `export PATH="` + dir + `:$PATH"`

	out := runAtTerminal(t, env, "y\n")
	if got, _ := os.ReadFile(rc); strings.Count(string(got), line) != 1 {
		t.Fatalf("~/.zshrc = %q after answering yes:\n%s", got, out)
	}
	out = runAtTerminal(t, env, "")
	if got, _ := os.ReadFile(rc); strings.Count(string(got), line) != 1 {
		t.Errorf("a second install added the line again: %q\n%s", got, out)
	}
	if strings.Contains(out, "[Y/n]") {
		t.Errorf("a second install asked again:\n%s", out)
	}
}

func runAtTerminal(t *testing.T, env []string, answer string) string {
	t.Helper()
	cmd := exec.Command("sh", installScript)
	cmd.Env = env
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	out := &lockedBuffer{}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(out, ptmx); close(done) }()
	if answer != "" {
		deadline := time.Now().Add(scriptPrompt)
		for !strings.Contains(out.String(), "[Y/n]") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := ptmx.Write([]byte(answer)); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out.String())
	}
	_ = ptmx.Close()
	<-done
	return out.String()
}

// lockedBuffer is written by the pty copy and read by the test waiting on it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
