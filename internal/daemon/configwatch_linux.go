//go:build linux

package daemon

import (
	"errors"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/unix"
)

// configEvents is every way the file's directory entry changes: written and
// closed in place, renamed over (how claude writes it), or created.
const configEvents = unix.IN_CLOSE_WRITE | unix.IN_MOVED_TO | unix.IN_CREATE

// watchConfig signals changes each time Claude's config file may have changed,
// until stop, which returns once the watch has. A wait in inotify, never a poll:
// the directory, filtered to the file's name, so a replacement is seen too. Nil
// when the kernel refuses the watch.
func watchConfig(path string) (<-chan struct{}, func()) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		logf("wake: no watch on %s: %v", path, err)
		return nil, func() {}
	}
	var wake [2]int
	if err := unix.Pipe2(wake[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		_ = unix.Close(fd)
		logf("wake: no watch on %s: %v", path, err)
		return nil, func() {}
	}
	w := &inotifyWatch{fd: fd, wake: wake, base: filepath.Base(path), changes: make(chan struct{}, 1), done: make(chan struct{})}
	if _, err := unix.InotifyAddWatch(fd, filepath.Dir(path), configEvents); err != nil {
		w.close()
		logf("wake: no watch on %s: %v", path, err)
		return nil, func() {}
	}
	go w.run()
	return w.changes, w.stop
}

type inotifyWatch struct {
	fd      int
	wake    [2]int
	base    string
	changes chan struct{}
	done    chan struct{}
}

func (w *inotifyWatch) run() {
	defer close(w.done)
	buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
	for {
		fds := []unix.PollFd{{Fd: int32(w.fd), Events: unix.POLLIN}, {Fd: int32(w.wake[0]), Events: unix.POLLIN}}
		if _, err := unix.Poll(fds, -1); err != nil && !errors.Is(err, unix.EINTR) {
			logf("wake: watch %s: %v", w.base, err)
			return
		}
		if fds[1].Revents != 0 {
			return
		}
		n, err := unix.Read(w.fd, buf)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
			continue // a spurious wake
		}
		if err != nil {
			logf("wake: watch %s: %v", w.base, err)
			return
		}
		if namesFile(buf[:n], w.base) {
			select {
			case w.changes <- struct{}{}:
			default: // one pending signal says enough
			}
		}
	}
}

// namesFile reports whether a read of inotify events names the file, or lost
// some to an overflow, which may have named it.
func namesFile(buf []byte, name string) bool {
	for off := 0; off+unix.SizeofInotifyEvent <= len(buf); {
		ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
		if ev.Mask&unix.IN_Q_OVERFLOW != 0 {
			return true
		}
		start := off + unix.SizeofInotifyEvent
		end := start + int(ev.Len)
		if end > len(buf) {
			return false
		}
		if string(trimNUL(buf[start:end])) == name {
			return true
		}
		off = end
	}
	return false
}

// trimNUL cuts the NUL padding inotify puts after a name.
func trimNUL(b []byte) []byte {
	for i, c := range b {
		if c == 0 {
			return b[:i]
		}
	}
	return b
}

func (w *inotifyWatch) stop() {
	if _, err := unix.Write(w.wake[1], []byte{0}); err != nil {
		logf("wake: stop watching %s: %v", w.base, err)
	}
	<-w.done
	w.close()
}

func (w *inotifyWatch) close() {
	_ = unix.Close(w.fd)
	_ = unix.Close(w.wake[0])
	_ = unix.Close(w.wake[1])
}
