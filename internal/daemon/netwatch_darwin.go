//go:build darwin

package daemon

import (
	"errors"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// routeBuffer holds one routing message - a header and a few socket addresses -
// with room to spare; the socket returns one message per read.
const routeBuffer = 2048

// watchRoutes signals each time the routing socket says the machine may be back
// on a network, until stop, which returns once the watch has. A wait in kqueue,
// never a poll. Nil when the kernel refuses the watch.
func watchRoutes() (<-chan time.Time, func()) {
	kq, err := unix.Kqueue() // not inherited across a fork
	if err != nil {
		logf("wake: no network watch: %v", err)
		return nil, func() {}
	}
	syscall.ForkLock.RLock() // so no child spawned meanwhile inherits the socket
	fd, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, unix.AF_UNSPEC)
	if err == nil {
		unix.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		_ = unix.Close(kq)
		logf("wake: no network watch: %v", err)
		return nil, func() {}
	}
	w := &routeWatch{kq: kq, fd: fd, changes: make(chan time.Time, 1), done: make(chan struct{})}
	for _, err := range []error{unix.SetNonblock(fd, true), kqueueAdd(kq, fd, unix.EVFILT_READ, 0), kqueueAdd(kq, 0, unix.EVFILT_USER, 0)} {
		if err != nil {
			w.close()
			logf("wake: no network watch: %v", err)
			return nil, func() {}
		}
	}
	go w.run()
	return w.changes, w.stop
}

type routeWatch struct {
	kq, fd  int
	changes chan time.Time
	done    chan struct{}
}

func (w *routeWatch) run() {
	defer close(w.done)
	events := make([]unix.Kevent_t, 2)
	buf := make([]byte, routeBuffer)
	for {
		n, err := unix.Kevent(w.kq, nil, events, nil)
		woke := time.Now() // when the kernel had it, before the reads that parse it
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			logf("wake: network watch: %v", err)
			return
		}
		for _, ev := range events[:n] {
			if ev.Filter == unix.EVFILT_USER {
				return
			}
		}
		if w.drain(buf) {
			latest(w.changes, woke)
		}
	}
}

// drain reads every message waiting on the socket, one per read, and reports
// whether any of them may mean the network came back.
func (w *routeWatch) drain(buf []byte) bool {
	changed := false
	for {
		n, err := unix.Read(w.fd, buf)
		if err != nil || n <= 0 {
			return changed // EAGAIN once it is empty
		}
		changed = changed || routeChanged(buf[:n])
	}
}

// routeChanged reports whether one routing message may mean the machine is back
// on a network: an interface gained an address or came up with a link, or a
// gateway route was added or changed. Host routes and lookup misses, which
// arrive all the time on a connected machine, say nothing.
func routeChanged(msg []byte) bool {
	if len(msg) < unix.SizeofIfaMsghdr || msg[2] != unix.RTM_VERSION {
		return false
	}
	switch msg[3] { // the type, where every routing message's header has it
	case unix.RTM_NEWADDR:
		return true
	case unix.RTM_IFINFO:
		flags, ok := field32(msg, unsafe.Offsetof(unix.IfMsghdr{}.Flags))
		return ok && flags&linkUp == linkUp && flags&unix.IFF_LOOPBACK == 0
	case unix.RTM_ADD, unix.RTM_CHANGE:
		flags, ok := field32(msg, unsafe.Offsetof(unix.RtMsghdr{}.Flags))
		errno, whole := field32(msg, unsafe.Offsetof(unix.RtMsghdr{}.Errno))
		return ok && whole && errno == 0 && flags&unix.RTF_GATEWAY != 0 && flags&unix.RTF_HOST == 0
	}
	return false
}

func (w *routeWatch) stop() {
	if err := kqueueTrigger(w.kq); err != nil {
		logf("wake: stop the network watch: %v", err)
	}
	<-w.done
	w.close()
}

func (w *routeWatch) close() {
	_ = unix.Close(w.fd)
	_ = unix.Close(w.kq)
}
