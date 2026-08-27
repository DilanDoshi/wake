//go:build unix && !linux

package daemon

import (
	"fmt"
	"syscall"
)

func validateAnonymousTestPipe(_ int, stat *syscall.Stat_t) error {
	if stat.Nlink != 0 {
		return fmt.Errorf("it is a named pipe with %d filesystem links", stat.Nlink)
	}
	return nil
}
