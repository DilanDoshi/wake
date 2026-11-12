package core

import (
	"os"
	"path/filepath"
	"testing"
)

// BUG-70: HOME=h is the working directory's h, spelled as Getwd spells it.
func TestHomeDirResolvesARelativeHome(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", "h")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := HomeDir(); err != nil || got != filepath.Join(wd, "h") {
		t.Errorf("HomeDir under HOME=h = %q (%v), want %q", got, err, filepath.Join(wd, "h"))
	}
}
