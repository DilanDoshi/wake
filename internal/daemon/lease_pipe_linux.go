//go:build linux

package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func validateAnonymousTestPipe(fd int, _ *syscall.Stat_t) error {
	target, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
	if err != nil {
		return fmt.Errorf("cannot prove it is anonymous: %w", err)
	}
	if !strings.HasPrefix(target, "pipe:[") {
		return fmt.Errorf("it is a named pipe at %q", target)
	}
	return nil
}
