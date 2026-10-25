//go:build linux

package daemon

import (
	"errors"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// routeGroups is what the watch hears: links, and addresses and routes in both
// families.
const routeGroups = unix.RTMGRP_LINK | unix.RTMGRP_IPV4_IFADDR | unix.RTMGRP_IPV6_IFADDR | unix.RTMGRP_IPV4_ROUTE | unix.RTMGRP_IPV6_ROUTE

// netlinkBuffer holds one read, which may carry many messages.
const netlinkBuffer = 64 * 1024

// watchRoutes signals each time netlink says the machine may be back on a
// network, until stop, which returns once the watch has. A wait in poll, never a
// poll loop. Nil when the kernel refuses the watch.
func watchRoutes() (<-chan time.Time, func()) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_ROUTE)
	if err != nil {
		logf("wake: no network watch: %v", err)
		return nil, func() {}
	}
	var wake [2]int
	if err := unix.Pipe2(wake[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		_ = unix.Close(fd)
		logf("wake: no network watch: %v", err)
		return nil, func() {}
	}
	w := &netlinkWatch{fd: fd, wake: wake, changes: make(chan time.Time, 1), done: make(chan struct{})}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: routeGroups}); err != nil {
		w.close()
		logf("wake: no network watch: %v", err)
		return nil, func() {}
	}
	go w.run()
	return w.changes, w.stop
}

type netlinkWatch struct {
	fd      int
	wake    [2]int
	changes chan time.Time
	done    chan struct{}
}

func (w *netlinkWatch) run() {
	defer close(w.done)
	buf := make([]byte, netlinkBuffer)
	for {
		fds := []unix.PollFd{{Fd: int32(w.fd), Events: unix.POLLIN}, {Fd: int32(w.wake[0]), Events: unix.POLLIN}}
		_, err := unix.Poll(fds, -1)
		woke := time.Now() // when the kernel had it, before the reads that parse it
		if err != nil && !errors.Is(err, unix.EINTR) {
			logf("wake: network watch: %v", err)
			return
		}
		if fds[1].Revents != 0 {
			return
		}
		if w.drain(buf) {
			latest(w.changes, woke)
		}
	}
}

// drain reads everything waiting on the socket and reports whether any of it
// may mean the network came back. An overflow lost messages that may have.
func (w *netlinkWatch) drain(buf []byte) bool {
	changed := false
	for {
		n, _, err := unix.Recvfrom(w.fd, buf, 0)
		switch {
		case errors.Is(err, unix.ENOBUFS):
			changed = true
		case err != nil || n <= 0:
			return changed // EAGAIN once it is empty
		default:
			changed = changed || routeChanged(buf[:n])
		}
	}
}

// routeChanged reports whether a read of netlink messages may mean the machine
// is back on a network: an interface came up with a link or gained an address,
// or a default route was added to the main table. Other routes - a neighbour's,
// a local one - say nothing.
func routeChanged(buf []byte) bool {
	msgs, err := syscall.ParseNetlinkMessage(buf)
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if messageChanged(m.Header.Type, m.Data) {
			return true
		}
	}
	return false
}

// messageChanged is routeChanged's verdict on one message.
func messageChanged(typ uint16, data []byte) bool {
	switch typ {
	case unix.RTM_NEWADDR:
		return true
	case unix.RTM_NEWLINK:
		flags, ok := field32(data, unsafe.Offsetof(unix.IfInfomsg{}.Flags))
		return ok && flags&linkUp == linkUp && flags&unix.IFF_LOOPBACK == 0
	case unix.RTM_NEWROUTE:
		return len(data) >= unix.SizeofRtMsg && data[unsafe.Offsetof(unix.RtMsg{}.Dst_len)] == 0 &&
			data[unsafe.Offsetof(unix.RtMsg{}.Table)] == unix.RT_TABLE_MAIN
	}
	return false
}

func (w *netlinkWatch) stop() {
	if _, err := unix.Write(w.wake[1], []byte{0}); err != nil {
		logf("wake: stop the network watch: %v", err)
	}
	<-w.done
	w.close()
}

func (w *netlinkWatch) close() {
	_ = unix.Close(w.fd)
	_ = unix.Close(w.wake[0])
	_ = unix.Close(w.wake[1])
}
