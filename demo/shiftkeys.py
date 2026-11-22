#!/usr/bin/env python3
"""Runs a command in a pty and turns a few typed keys into what VHS cannot send.

    python3 shiftkeys.py wake new maya

VHS cannot send ⇧↑↓: a raw escape it types reaches Wake as a lone ⎋ and then
text. The roster walks on ⇧↑↓, and a channel is picked there, so a tape that
needs one runs Wake under this and types Ctrl+G / Ctrl+K, which never reach
Wake as themselves. Each shifted arrow is written whole, so it arrives as one
key. Everything else passes through untouched, both ways.

VHS cannot send the mouse either. $WAKE_GESTURES names a JSON file (a bare name is one in
$WAKE_GESTURES_DIR) that binds
ctrl+<key> names to steps (click, press, move, release, glide, wheel_up,
wheel_down, shift_click, wait), cells counted from 0 on the screen; each bound
key is written as the SGR mouse reports a terminal sends for it, with the pacing
the steps ask for. demo/gestures/ holds one file per clip.
"""

import collections
import fcntl
import json
import os
import pty
import select
import signal
import sys
import termios
import time
import tty
from typing import Callable

KEYS = {b"\x07": b"\x1b[1;2B", b"\x0b": b"\x1b[1;2A"}

# SGR mouse buttons: bit 2 is shift, bit 5 motion with a button held, 64 the wheel.
MOTION = 32
SHIFT = 4
WHEEL_UP = 64
WHEEL_DOWN = 65
CLICK_HOLD = 0.06  # seconds a click's button stays down

# What a gesture may not take: the keys the relay turns, and the ones a tape
# types as themselves (enter, tab, backspace, esc, ⌃C).
RESERVED = {b"\x03", b"\x07", b"\x08", b"\x09", b"\x0a", b"\x0b", b"\x0d", b"\x1b"}

Event = tuple[float, bytes]


def resize(fd: int) -> None:
    """Gives the child the terminal's size, as a real one would."""
    size = fcntl.ioctl(sys.stdin.fileno(), termios.TIOCGWINSZ, b"\0" * 8)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, size)


def translate(data: bytes) -> bytes:
    for key, seq in KEYS.items():
        data = data.replace(key, seq)
    return data


def sgr(button: int, x: int, y: int, release: bool = False) -> bytes:
    """One mouse report at a screen cell counted from 0, as a terminal writes it."""
    return b"\x1b[<%d;%d;%d%s" % (button, x + 1, y + 1, b"m" if release else b"M")


def glide(
    src: tuple[int, int], dst: tuple[int, int], steps: int
) -> list[tuple[int, int]]:
    """The cells a pointer crosses from src to dst in steps moves, ending on dst."""
    return [
        (
            round(src[0] + (dst[0] - src[0]) * i / steps),
            round(src[1] + (dst[1] - src[1]) * i / steps),
        )
        for i in range(1, steps + 1)
    ]


def cell(value: object) -> tuple[int, int]:
    if (
        not isinstance(value, list)
        or len(value) != 2
        or not all(isinstance(n, int) and n >= 0 for n in value)
    ):
        raise ValueError(f"a cell is [column, row] counted from 0, not {value!r}")
    return value[0], value[1]


def glide_events(spec: object) -> list[Event]:
    if not isinstance(spec, dict) or set(spec) != {"from", "to", "steps", "over"}:
        raise ValueError(f"glide takes from, to, steps and over, not {spec!r}")
    steps, over = spec["steps"], spec["over"]
    if not isinstance(steps, int) or steps < 1 or over < 0:
        raise ValueError(
            f"glide needs at least one step and a time of 0 or more, not {spec!r}"
        )
    path = glide(cell(spec["from"]), cell(spec["to"]), steps)
    return [(over / steps, sgr(MOTION, x, y)) for x, y in path]


def click_events(button: int, at: object) -> list[Event]:
    x, y = cell(at)
    return [(0, sgr(button, x, y)), (CLICK_HOLD, sgr(button, x, y, release=True))]


def one_report(button: int, release: bool = False) -> Callable[[object], list[Event]]:
    return lambda at: [(0, sgr(button, *cell(at), release=release))]


STEPS: dict[str, Callable[[object], list[Event]]] = {
    "press": one_report(0),
    "release": one_report(0, release=True),
    "move": one_report(MOTION),
    "wheel_up": one_report(WHEEL_UP),
    "wheel_down": one_report(WHEEL_DOWN),
    "click": lambda at: click_events(0, at),
    "shift_click": lambda at: click_events(SHIFT, at),
    "glide": glide_events,
}


def expand(steps: list[dict]) -> list[Event]:
    """A gesture's steps as (seconds after the one before, bytes), in order."""
    events: list[Event] = []
    waited = 0.0
    for step in steps:
        if not isinstance(step, dict) or len(step) != 1:
            raise ValueError(f"a step is one name and its argument, not {step!r}")
        ((name, arg),) = step.items()
        if name == "wait":
            waited += float(arg)
            continue
        if name not in STEPS:
            raise ValueError(
                f"unknown step {name!r}; the steps are wait and {', '.join(STEPS)}"
            )
        for delay, data in STEPS[name](arg):
            events.append((waited + delay, data))
            waited = 0.0
    return events


def gesture_path(name: str, directory: str) -> str:
    """$WAKE_GESTURES as a file: a path stays one, a bare clip name is a file in
    $WAKE_GESTURES_DIR. A tape exports the name before it sources the stage, which
    is where `wake` first starts, and only the stage's own env.sh knows the directory.
    """
    if not name or "/" in name:
        return name
    if not directory:
        raise ValueError(
            f"{name!r} is a name, and WAKE_GESTURES_DIR says where its file is"
        )
    return os.path.join(directory, name + ".json")


def load_gestures(path: str) -> dict[bytes, list[dict]]:
    """The file's ctrl+<key> names as the bytes they type, each gesture checked now
    so a bad file stops the take before the first key rather than mid-film."""
    if not path:
        return {}
    with open(path, encoding="utf-8") as fh:
        spec = json.load(fh)
    gestures: dict[bytes, list[dict]] = {}
    for name, steps in spec.items():
        if name.startswith("_"):  # a note
            continue
        prefix, _, key = name.lower().partition("+")
        if prefix != "ctrl" or len(key) != 1 or not 0x40 <= ord(key.upper()) <= 0x5F:
            raise ValueError(f"{name!r}: a gesture binds ctrl+<letter> only")
        byte = bytes([ord(key.upper()) & 0x1F])
        if byte in RESERVED:
            raise ValueError(
                f"{name!r} is reserved: the relay or the tape needs that key"
            )
        expand(steps)
        gestures[byte] = steps
    return gestures


def route(data: bytes, gestures: dict[bytes, list[dict]]) -> list[Event]:
    """What typed bytes become: a bound key its gesture, the rest as translate
    makes it, in the order typed and one write per run so a key stays whole."""
    events: list[Event] = []
    run = bytearray()
    for byte in data:
        key = bytes([byte])
        if key not in gestures:
            run.append(byte)
            continue
        if run:
            events.append((0, translate(bytes(run))))
            run.clear()
        events.extend(expand(gestures[key]))
    if run:
        events.append((0, translate(bytes(run))))
    return events


class Outbox:
    """What is waiting to be written to the child and when it is due. Bytes typed
    while a gesture plays queue behind it, so a key never lands mid-drag."""

    def __init__(self, clock: Callable[[], float] = time.monotonic) -> None:
        self._clock = clock
        self._queue: collections.deque[tuple[float, bytes]] = collections.deque()
        self._tail = 0.0

    def put(self, events: list[Event]) -> None:
        due = max(self._clock(), self._tail)
        for delay, data in events:
            due += delay
            self._queue.append((due, data))
        self._tail = due

    def ready(self) -> bytes:
        now = self._clock()
        out = b""
        while self._queue and self._queue[0][0] <= now:
            out += self._queue.popleft()[1]
        return out

    def wait(self) -> float | None:
        """Seconds until the next event is due, or None with nothing queued."""
        if not self._queue:
            return None
        return max(0.0, self._queue[0][0] - self._clock())


def write_all(fd: int, data: bytes) -> None:
    while data:
        data = data[os.write(fd, data) :]


def relay(fd: int, gestures: dict[bytes, list[dict]]) -> None:
    """Until either side closes: the child exiting reads as EIO on its side."""
    stdin = sys.stdin.fileno()
    outbox = Outbox()
    try:
        while True:
            ready, _, _ = select.select([stdin, fd], [], [], outbox.wait())
            if stdin in ready:
                data = os.read(stdin, 4096)
                if not data:
                    return
                outbox.put(route(data, gestures))
            if fd in ready:
                data = os.read(fd, 65536)
                if not data:
                    return
                write_all(sys.stdout.fileno(), data)
            due = outbox.ready()
            if due:
                write_all(fd, due)
    except OSError:
        return


def main() -> int:
    if len(sys.argv) < 2:
        sys.exit("usage: shiftkeys.py <command> [args…]")
    if not os.isatty(sys.stdin.fileno()):  # no keys to turn: just run it
        os.execvp(sys.argv[1], sys.argv[1:])
    try:
        gestures = load_gestures(
            gesture_path(
                os.environ.get("WAKE_GESTURES", ""),
                os.environ.get("WAKE_GESTURES_DIR", ""),
            )
        )
    except (OSError, ValueError) as err:
        sys.exit(f"shiftkeys: {err}")
    pid, fd = pty.fork()
    if pid == 0:
        os.execvp(sys.argv[1], sys.argv[1:])
    resize(fd)
    signal.signal(signal.SIGWINCH, lambda *_: resize(fd))
    # Ended from outside, it still puts the terminal back on the way out.
    for sig in (signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, lambda signum, _: sys.exit(128 + signum))
    saved = termios.tcgetattr(sys.stdin.fileno())
    tty.setraw(sys.stdin.fileno())
    try:
        relay(fd, gestures)
    finally:
        termios.tcsetattr(sys.stdin.fileno(), termios.TCSADRAIN, saved)
    _, status = os.waitpid(pid, 0)
    code = os.waitstatus_to_exitcode(status)
    return code if code >= 0 else 128 - code  # a signal, as a shell reports it


if __name__ == "__main__":
    sys.exit(main())
