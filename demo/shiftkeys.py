#!/usr/bin/env python3
"""Runs a command in a pty and turns ⌃G into ⇧↓ and ⌃K into ⇧↑ on the way in.

    python3 shiftkeys.py wake new maya

VHS cannot send ⇧↑↓: a raw escape it types reaches Wake as a lone ⎋ and then
text. The roster walks on ⇧↑↓, and a channel is picked there, so a tape that
needs one runs Wake under this and types Ctrl+G / Ctrl+K, neither of which Wake
binds. Each shifted arrow is written whole, in one write, so it arrives as one
key. Everything else passes through untouched, both ways.
"""

import fcntl
import os
import pty
import select
import signal
import sys
import termios
import tty

KEYS = {b"\x07": b"\x1b[1;2B", b"\x0b": b"\x1b[1;2A"}


def resize(fd: int) -> None:
    """Gives the child the terminal's size, as a real one would."""
    size = fcntl.ioctl(sys.stdin.fileno(), termios.TIOCGWINSZ, b"\0" * 8)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, size)


def translate(data: bytes) -> bytes:
    for key, seq in KEYS.items():
        data = data.replace(key, seq)
    return data


def relay(fd: int) -> None:
    stdin = sys.stdin.fileno()
    while True:
        try:
            ready, _, _ = select.select([stdin, fd], [], [])
        except InterruptedError:
            continue
        if stdin in ready:
            data = os.read(stdin, 4096)
            if not data:
                return
            os.write(fd, translate(data))
        if fd in ready:
            try:
                data = os.read(fd, 65536)
            except OSError:  # the child closed its side
                return
            if not data:
                return
            os.write(sys.stdout.fileno(), data)


def main() -> int:
    if len(sys.argv) < 2:
        sys.exit("usage: shiftkeys.py <command> [args…]")
    if not os.isatty(sys.stdin.fileno()):  # no keys to turn: just run it
        os.execvp(sys.argv[1], sys.argv[1:])
    pid, fd = pty.fork()
    if pid == 0:
        os.execvp(sys.argv[1], sys.argv[1:])
    resize(fd)
    signal.signal(signal.SIGWINCH, lambda *_: resize(fd))
    saved = termios.tcgetattr(sys.stdin.fileno())
    tty.setraw(sys.stdin.fileno())
    try:
        relay(fd)
    finally:
        termios.tcsetattr(sys.stdin.fileno(), termios.TCSADRAIN, saved)
    _, status = os.waitpid(pid, 0)
    return os.waitstatus_to_exitcode(status)


if __name__ == "__main__":
    sys.exit(main())
