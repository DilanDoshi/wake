#!/usr/bin/env python3
"""Runs a command in a pty and turns ⌃G into ⇧↓ and ⌃K into ⇧↑ on the way in.

    python3 shiftkeys.py wake new maya

VHS cannot send ⇧↑↓: a raw escape it types reaches Wake as a lone ⎋ and then
text. The roster walks on ⇧↑↓, and a channel is picked there, so a tape that
needs one runs Wake under this and types Ctrl+G / Ctrl+K, which never reach
Wake as themselves. Each shifted arrow is written whole, so it arrives as one
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


def write_all(fd: int, data: bytes) -> None:
    while data:
        data = data[os.write(fd, data) :]


def relay(fd: int) -> None:
    """Until either side closes: the child exiting reads as EIO on its side."""
    stdin = sys.stdin.fileno()
    try:
        while True:
            ready, _, _ = select.select([stdin, fd], [], [])
            if stdin in ready:
                data = os.read(stdin, 4096)
                if not data:
                    return
                write_all(fd, translate(data))
            if fd in ready:
                data = os.read(fd, 65536)
                if not data:
                    return
                write_all(sys.stdout.fileno(), data)
    except OSError:
        return


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
    code = os.waitstatus_to_exitcode(status)
    return code if code >= 0 else 128 - code  # a signal, as a shell reports it


if __name__ == "__main__":
    sys.exit(main())
