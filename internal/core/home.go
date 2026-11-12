package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// HomeDir is the home directory, absolute: a relative HOME (HOME=h, a literal ~)
// is resolved against the working directory, as any relative path is (BUG-70).
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return "", fmt.Errorf("resolve HOME %q against the working directory: %w", home, err)
	}
	return abs, nil
}
