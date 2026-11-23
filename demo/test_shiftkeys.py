"""Tests for the take-only key and mouse relay (shiftkeys.py).

    python3 -m unittest discover -s demo

The relay sits between VHS and `wake` in a take, so a wrong byte here films a
wrong gesture without an error anywhere: these hold the bytes, the timing and
the refusals, and one real pty pins that they arrive.
"""

import ast
import json
import os
import pty
import select
import sys
import tempfile
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import shiftkeys  # noqa: E402


class SGR(unittest.TestCase):
    def test_a_left_press_is_one_based(self):
        self.assertEqual(shiftkeys.sgr(0, 140, 2), b"\x1b[<0;141;3M")

    def test_a_release_is_a_lowercase_m(self):
        self.assertEqual(shiftkeys.sgr(0, 140, 2, release=True), b"\x1b[<0;141;3m")

    def test_motion_with_the_button_held(self):
        self.assertEqual(shiftkeys.sgr(shiftkeys.MOTION, 5, 6), b"\x1b[<32;6;7M")

    def test_a_shift_press_and_the_wheel(self):
        self.assertEqual(shiftkeys.sgr(shiftkeys.SHIFT, 0, 0), b"\x1b[<4;1;1M")
        self.assertEqual(shiftkeys.sgr(shiftkeys.WHEEL_UP, 0, 0), b"\x1b[<64;1;1M")
        self.assertEqual(shiftkeys.sgr(shiftkeys.WHEEL_DOWN, 0, 0), b"\x1b[<65;1;1M")


class Glide(unittest.TestCase):
    def test_it_ends_exactly_on_the_target_in_n_steps(self):
        path = shiftkeys.glide((0, 0), (10, 4), 5)
        self.assertEqual(len(path), 5)
        self.assertEqual(path[-1], (10, 4))

    def test_it_moves_off_the_start_and_never_backs_away(self):
        path = shiftkeys.glide((140, 3), (60, 20), 12)
        self.assertNotEqual(path[0], (140, 3))
        xs = [p[0] for p in path]
        ys = [p[1] for p in path]
        self.assertEqual(xs, sorted(xs, reverse=True))
        self.assertEqual(ys, sorted(ys))

    def test_one_step_is_just_the_target(self):
        self.assertEqual(shiftkeys.glide((1, 1), (9, 9), 1), [(9, 9)])


class Steps(unittest.TestCase):
    def test_a_click_is_a_press_then_a_release_a_beat_later(self):
        events = shiftkeys.expand([{"click": [3, 4]}])
        self.assertEqual(
            [e[1] for e in events],
            [shiftkeys.sgr(0, 3, 4), shiftkeys.sgr(0, 3, 4, release=True)],
        )
        self.assertEqual(events[0][0], 0)
        self.assertGreater(events[1][0], 0)

    def test_a_shift_click_carries_shift_on_both_halves(self):
        events = shiftkeys.expand([{"shift_click": [3, 4]}])
        self.assertEqual(
            [e[1] for e in events],
            [
                shiftkeys.sgr(shiftkeys.SHIFT, 3, 4),
                shiftkeys.sgr(shiftkeys.SHIFT, 3, 4, release=True),
            ],
        )

    def test_press_move_release_are_single_reports(self):
        events = shiftkeys.expand(
            [{"press": [1, 2]}, {"move": [3, 4]}, {"release": [5, 6]}]
        )
        self.assertEqual(
            [e[1] for e in events],
            [
                shiftkeys.sgr(0, 1, 2),
                shiftkeys.sgr(shiftkeys.MOTION, 3, 4),
                shiftkeys.sgr(0, 5, 6, release=True),
            ],
        )

    def test_a_glide_spreads_its_motion_over_its_time(self):
        events = shiftkeys.expand(
            [{"glide": {"from": [0, 0], "to": [8, 0], "steps": 4, "over": 0.4}}]
        )
        self.assertEqual(len(events), 4)
        self.assertAlmostEqual(sum(e[0] for e in events), 0.4)
        self.assertEqual(events[-1][1], shiftkeys.sgr(shiftkeys.MOTION, 8, 0))

    def test_a_wheel_notch_and_a_wait(self):
        events = shiftkeys.expand(
            [{"wheel_down": [7, 8]}, {"wait": 0.5}, {"wheel_up": [7, 8]}]
        )
        self.assertEqual(events[0][1], shiftkeys.sgr(shiftkeys.WHEEL_DOWN, 7, 8))
        self.assertEqual(events[1][1], shiftkeys.sgr(shiftkeys.WHEEL_UP, 7, 8))
        self.assertAlmostEqual(events[1][0], 0.5)

    def test_a_wait_delays_only_the_next_report(self):
        events = shiftkeys.expand([{"wait": 0.5}, {"click": [1, 1]}])
        self.assertAlmostEqual(events[0][0], 0.5)
        self.assertAlmostEqual(events[1][0], shiftkeys.CLICK_HOLD)

    def test_a_wait_is_a_finite_time_of_zero_or_more(self):
        for bad in (float("nan"), float("inf"), -1, "1", True, None):
            with self.subTest(bad=bad), self.assertRaisesRegex(ValueError, "wait"):
                shiftkeys.expand([{"wait": bad}, {"click": [1, 1]}])

    def test_a_wait_with_nothing_after_it_is_refused(self):
        with self.assertRaisesRegex(ValueError, "wait"):
            shiftkeys.expand([{"click": [1, 1]}, {"wait": 3}])

    def test_a_glide_time_is_a_number(self):
        for bad in ("1", -0.5, float("nan"), True):
            spec = {"from": [0, 0], "to": [1, 1], "steps": 2, "over": bad}
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                shiftkeys.expand([{"glide": spec}])

    def test_a_cell_is_not_a_bool(self):
        with self.assertRaises(ValueError):
            shiftkeys.expand([{"click": [True, 1]}])

    def test_an_unknown_step_is_refused_by_name(self):
        with self.assertRaisesRegex(ValueError, "teleport"):
            shiftkeys.expand([{"teleport": [1, 1]}])

    def test_a_negative_cell_and_a_zero_step_glide_are_refused(self):
        with self.assertRaises(ValueError):
            shiftkeys.expand([{"click": [-1, 4]}])
        with self.assertRaises(ValueError):
            shiftkeys.expand(
                [{"glide": {"from": [0, 0], "to": [1, 1], "steps": 0, "over": 0.1}}]
            )


class Gestures(unittest.TestCase):
    def write(self, spec):
        fh = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        self.addCleanup(os.unlink, fh.name)
        json.dump(spec, fh)
        fh.close()
        return fh.name

    def test_a_key_name_binds_its_control_byte(self):
        got = shiftkeys.load_gestures(self.write({"ctrl+l": [{"click": [1, 1]}]}))
        self.assertEqual(list(got), [b"\x0c"])

    def test_a_note_is_not_a_key(self):
        got = shiftkeys.load_gestures(
            self.write({"_note": "x", "ctrl+l": [{"click": [1, 1]}]})
        )
        self.assertEqual(list(got), [b"\x0c"])

    def test_the_keys_the_relay_or_the_tape_needs_are_refused(self):
        for key in (
            "ctrl+g",
            "ctrl+k",
            "ctrl+m",
            "ctrl+i",
            "ctrl+j",
            "ctrl+h",
            "ctrl+c",
            "ctrl+[",
            "ctrl+w",
            "ctrl+q",
        ):
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "reserved"):
                shiftkeys.load_gestures(self.write({key: [{"click": [1, 1]}]}))

    def test_a_key_that_is_not_a_control_key_is_refused(self):
        with self.assertRaisesRegex(ValueError, "ctrl"):
            shiftkeys.load_gestures(self.write({"alt+l": [{"click": [1, 1]}]}))

    def test_a_bad_step_stops_the_load_not_the_take(self):
        with self.assertRaisesRegex(ValueError, "teleport"):
            shiftkeys.load_gestures(self.write({"ctrl+l": [{"teleport": [1, 1]}]}))

    def write_text(self, text):
        fh = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        self.addCleanup(os.unlink, fh.name)
        fh.write(text)
        fh.close()
        return fh.name

    def test_a_key_that_uppercases_to_two_letters_is_refused(self):
        with self.assertRaisesRegex(ValueError, "ctrl"):
            shiftkeys.load_gestures(self.write({"ctrl+\u00df": [{"click": [1, 1]}]}))

    def test_a_file_that_is_not_a_table_of_step_lists_is_refused(self):
        for bad in ([], {"ctrl+l": 5}, {"ctrl+l": [5]}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                shiftkeys.load_gestures(self.write(bad))

    def test_a_nan_in_the_file_is_refused_not_spun_on(self):
        with self.assertRaisesRegex(ValueError, "wait"):
            shiftkeys.load_gestures(
                self.write_text('{"ctrl+l": [{"wait": NaN}, {"click": [1, 1]}]}')
            )

    def test_no_path_means_no_gestures(self):
        self.assertEqual(shiftkeys.load_gestures(""), {})


class Portability(unittest.TestCase):
    def test_no_union_syntax_in_an_annotation_so_python_39_can_import_it(self):
        # `float | None` is evaluated when the def runs and is a TypeError before 3.10;
        # env.sh's wake() runs this file for every take, not only the mouse ones.
        with open(os.path.join(HERE, "shiftkeys.py"), encoding="utf-8") as fh:
            tree = ast.parse(fh.read())
        annotations = []
        for node in ast.walk(tree):
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                annotations.append(node.returns)
                annotations += [
                    a.annotation for a in node.args.args + node.args.kwonlyargs
                ]
            elif isinstance(node, ast.AnnAssign):
                annotations.append(node.annotation)
        for annotation in filter(None, annotations):
            for part in ast.walk(annotation):
                if isinstance(part, ast.BinOp) and isinstance(part.op, ast.BitOr):
                    self.fail(f"a | union in an annotation at line {part.lineno}")


class GesturePath(unittest.TestCase):
    def test_a_bare_name_is_a_file_in_the_gestures_directory(self):
        self.assertEqual(
            shiftkeys.gesture_path("34-drag", "/d/gestures"), "/d/gestures/34-drag.json"
        )

    def test_a_path_is_kept_as_it_is(self):
        self.assertEqual(
            shiftkeys.gesture_path("/tmp/x.json", "/d/gestures"), "/tmp/x.json"
        )

    def test_no_name_is_no_file(self):
        self.assertEqual(shiftkeys.gesture_path("", "/d/gestures"), "")

    def test_a_name_with_no_directory_to_find_it_in_is_refused(self):
        with self.assertRaisesRegex(ValueError, "WAKE_GESTURES_DIR"):
            shiftkeys.gesture_path("34-drag", "")


class Route(unittest.TestCase):
    def test_the_shifted_arrows_are_unchanged(self):
        self.assertEqual(shiftkeys.route(b"\x07", {}), [(0, b"\x1b[1;2B")])
        self.assertEqual(shiftkeys.route(b"x\x0by", {}), [(0, b"x\x1b[1;2Ay")])

    def test_plain_bytes_pass_in_one_write(self):
        self.assertEqual(shiftkeys.route(b"hello", {}), [(0, b"hello")])

    def test_a_gesture_key_splits_the_bytes_around_it_in_order(self):
        gestures = {b"\x0c": [{"click": [3, 4]}]}
        events = shiftkeys.route(b"ab\x0ccd", gestures)
        self.assertEqual(
            [e[1] for e in events],
            [
                b"ab",
                shiftkeys.sgr(0, 3, 4),
                shiftkeys.sgr(0, 3, 4, release=True),
                b"cd",
            ],
        )


class Clock:
    def __init__(self):
        self.now = 0.0

    def __call__(self):
        return self.now


class Outbox(unittest.TestCase):
    def setUp(self):
        self.clock = Clock()
        self.box = shiftkeys.Outbox(self.clock)

    def test_nothing_waits_when_nothing_is_queued(self):
        self.assertIsNone(self.box.wait())
        self.assertEqual(self.box.ready(), b"")

    def test_an_event_is_released_only_when_due(self):
        self.box.put([(0, b"a"), (0.1, b"b")])
        self.assertEqual(self.box.ready(), b"a")
        self.clock.now = 0.05
        self.assertEqual(self.box.ready(), b"")
        self.assertAlmostEqual(self.box.wait(), 0.05)
        self.clock.now = 0.1
        self.assertEqual(self.box.ready(), b"b")
        self.assertIsNone(self.box.wait())

    def test_bytes_typed_during_a_gesture_wait_behind_it(self):
        self.box.put([(0, b"press"), (0.2, b"release")])
        self.clock.now = 0.02
        self.box.put([(0, b"typed")])
        self.assertEqual(self.box.ready(), b"press")
        self.clock.now = 0.2
        self.assertEqual(self.box.ready(), b"release")
        self.clock.now = 0.2 + shiftkeys.GAP
        self.assertEqual(self.box.ready(), b"typed")

    def test_a_second_gesture_keeps_its_own_pacing_behind_the_first(self):
        self.box.put([(0, b"a1"), (0.2, b"a2")])
        self.clock.now = 0.02
        self.box.put([(0, b"b1"), (0.1, b"b2")])
        self.clock.now = 0.2
        self.assertEqual(self.box.ready(), b"a1a2")
        self.assertAlmostEqual(self.box.wait(), shiftkeys.GAP)
        self.clock.now = 0.2 + shiftkeys.GAP
        self.assertEqual(self.box.ready(), b"b1")
        self.assertAlmostEqual(self.box.wait(), 0.1)


class OnARealPty(unittest.TestCase):
    """The relay, run as a take runs it: a pty in front of it, `cat -v` behind."""

    def relay(self, env_extra, command):
        env = dict(os.environ, **env_extra)
        pid, fd = pty.fork()
        if pid == 0:
            os.execvpe(
                sys.executable,
                [sys.executable, os.path.join(HERE, "shiftkeys.py"), *command],
                env,
            )
        self.addCleanup(self.reap, pid, fd)
        return pid, fd

    def read_until(self, fd, marker, seconds=5):
        seen = b""
        deadline = time.time() + seconds
        while time.time() < deadline and marker not in seen:
            if select.select([fd], [], [], 0.2)[0]:
                try:
                    seen += os.read(fd, 4096)
                except OSError:
                    break
        return seen

    def test_a_gesture_key_arrives_as_a_click_and_the_shifted_arrow_as_one_key(self):
        gestures = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        self.addCleanup(os.unlink, gestures.name)
        json.dump({"ctrl+l": [{"click": [140, 2]}]}, gestures)
        gestures.close()

        _, fd = self.relay(
            {"WAKE_GESTURES": gestures.name},
            ["sh", "-c", "stty raw -echo; echo READY; exec cat -v"],
        )
        self.assertIn(b"READY", self.read_until(fd, b"READY"))
        os.write(fd, b"\x0c\x07")
        seen = self.read_until(fd, b"^[[1;2B")
        self.assertIn(b"^[[<0;141;3M", seen)
        self.assertIn(b"^[[<0;141;3m", seen)
        self.assertIn(b"^[[1;2B", seen)
        self.assertLess(seen.index(b"^[[<0;141;3m"), seen.index(b"^[[1;2B"))

    def test_a_bad_gesture_file_stops_the_take_before_the_child_starts(self):
        bad = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        self.addCleanup(os.unlink, bad.name)
        bad.write('{"ctrl+l": [{"wait": NaN}, {"click": [1, 1]}]}')
        bad.close()

        pid, fd = self.relay(
            {"WAKE_GESTURES": bad.name}, ["sh", "-c", "echo CHILD-RAN"]
        )
        seen = self.read_until(fd, b"never appears", seconds=2)
        _, status = os.waitpid(pid, 0)
        self.assertIn(b"shiftkeys:", seen)
        self.assertNotIn(b"CHILD-RAN", seen)
        self.assertNotEqual(os.waitstatus_to_exitcode(status), 0)

    def reap(self, pid, fd):
        try:
            os.kill(pid, 15)
        except ProcessLookupError:
            pass
        try:
            os.waitpid(pid, 0)
        except ChildProcessError:
            pass
        os.close(fd)


if __name__ == "__main__":
    unittest.main()
