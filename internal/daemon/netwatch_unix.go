//go:build darwin || linux

package daemon

import (
	"encoding/binary"
	"time"

	"golang.org/x/sys/unix"
)

// linkUp is an interface that came up and has a link: Wi-Fi rejoining, a cable,
// a wake from sleep that kept its address and its routes.
const linkUp = unix.IFF_UP | unix.IFF_RUNNING

// field32 reads a message header's 32-bit field at off, false past its end - a
// read at an offset, so a buffer's alignment never matters.
func field32(msg []byte, off uintptr) (uint32, bool) {
	if uintptr(len(msg)) < off+4 {
		return 0, false
	}
	return binary.NativeEndian.Uint32(msg[off:]), true
}

// latest hands the resume goroutine the newest change, as the kernel reported
// it, in place of one it has not taken yet; the watch is the one sender.
func latest(changes chan time.Time, at time.Time) {
	select {
	case <-changes:
	default:
	}
	select {
	case changes <- at:
	default:
	}
}
