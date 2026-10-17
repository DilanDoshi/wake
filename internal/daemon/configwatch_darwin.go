//go:build darwin

package daemon

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// fileEvents is every way the watched file itself changes: written in place, or
// replaced or removed, after which the new file is opened and watched instead.
const fileEvents = unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_ATTRIB | unix.NOTE_DELETE | unix.NOTE_RENAME

// watchConfig signals changes each time Claude's config file may have changed,
// until stop, which returns once the watch has. A wait in kqueue, never a poll:
// the directory, whose entries change when claude replaces the file by a rename,
// and the file, for a write in place. Nil when the kernel refuses the watch.
func watchConfig(path string) (<-chan struct{}, func()) {
	kq, err := unix.Kqueue()
	if err != nil {
		logf("wake: no watch on %s: %v", path, err)
		return nil, func() {}
	}
	dir, err := unix.Open(filepath.Dir(path), unix.O_EVTONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(kq)
		logf("wake: no watch on %s: %v", path, err)
		return nil, func() {}
	}
	w := &kqueueWatch{kq: kq, dir: dir, file: -1, path: path, changes: make(chan struct{}, 1), done: make(chan struct{})}
	if err := w.register(dir, unix.EVFILT_VNODE, unix.NOTE_WRITE); err != nil {
		w.close()
		logf("wake: no watch on %s: %v", path, err)
		return nil, func() {}
	}
	if err := w.register(0, unix.EVFILT_USER, 0); err != nil {
		w.close()
		logf("wake: no watch on %s: %v", path, err)
		return nil, func() {}
	}
	w.watchFile()
	go w.run()
	return w.changes, w.stop
}

type kqueueWatch struct {
	kq, dir, file int
	path          string
	changes       chan struct{}
	done          chan struct{}
}

func (w *kqueueWatch) register(ident int, filter int16, fflags uint32) error {
	var ev unix.Kevent_t
	unix.SetKevent(&ev, ident, int(filter), unix.EV_ADD|unix.EV_CLEAR)
	ev.Fflags = fflags
	_, err := unix.Kevent(w.kq, []unix.Kevent_t{ev}, nil, nil)
	return err
}

// watchFile follows the file to whatever is at the path now. Missing is fine:
// the directory's own event reopens it once something is written there.
func (w *kqueueWatch) watchFile() {
	if w.file >= 0 {
		_ = unix.Close(w.file) // closing it drops its event
		w.file = -1
	}
	fd, err := unix.Open(w.path, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	w.file = fd
	if err := w.register(fd, unix.EVFILT_VNODE, fileEvents); err != nil {
		logf("wake: watch %s: %v", w.path, err)
	}
}

func (w *kqueueWatch) run() {
	defer close(w.done)
	events := make([]unix.Kevent_t, 8)
	for {
		n, err := unix.Kevent(w.kq, nil, events, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			logf("wake: watch %s: %v", w.path, err)
			return
		}
		for _, ev := range events[:n] {
			if ev.Filter == unix.EVFILT_USER {
				return
			}
			if int(ev.Ident) == w.dir || ev.Fflags&(unix.NOTE_DELETE|unix.NOTE_RENAME) != 0 {
				w.watchFile()
			}
		}
		select {
		case w.changes <- struct{}{}:
		default: // one pending signal says enough
		}
	}
}

func (w *kqueueWatch) stop() {
	var ev unix.Kevent_t
	unix.SetKevent(&ev, 0, unix.EVFILT_USER, 0)
	ev.Fflags = unix.NOTE_TRIGGER
	if _, err := unix.Kevent(w.kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		logf("wake: stop watching %s: %v", w.path, err)
	}
	<-w.done
	w.close()
}

func (w *kqueueWatch) close() {
	if w.file >= 0 {
		_ = unix.Close(w.file)
	}
	_ = unix.Close(w.dir)
	_ = unix.Close(w.kq)
}
