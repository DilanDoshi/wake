"""Tests for the landing site's page layout (page.py) and its words (pagecopy.py).

    python3 -m unittest discover -s demo

They render the two pages from the copy, with no ffmpeg and no network, and hold
what the repo can check: the order the front page leads in, that every clip has a
well-formed tape it was filmed from, that a cut never runs past its recording, and
that the words do not still say what a review proved false. They cannot check a
sentence against the code it describes; that is a human's (or a reviewer's) job, and
RETIRED is where each sentence one caught goes so it cannot come back.
"""

import os
import re
import sys
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import page  # noqa: E402
import pagecopy  # noqa: E402
import shiftkeys  # noqa: E402

CLIPS = [s.clip for s in pagecopy.SHOTS] + [f.clip for f in pagecopy.FRIENDS]

# The owner's call (wake-orch, 2026-11-22): the front page leads with the new work
# and carries eleven sections; leaving is on the features page alone.
FRONT_PAGE = [
    "33-jump",
    "34-drag",
    "35-search",
    "24-teams",
    "29-channels",
    "03-broadcast",
    "09-manager",
    "07-grid",
    "05-blocked",
    "10-board",
    "08-fork",
]

# Sentences found false or misleading in review, each as a phrase that must not
# come back: what it claimed, and what the code does (the adversarial review of
# the v0.1.17 refresh, checked against #249-#257).
RETIRED = [
    # ↵ opens as a click does; ⌃B and ⌃D are placements of their own.
    "exactly as a click on its row does",
    # An agent with no /color draws its drag tag in Wake's accent.
    "in its own colour",
    # The board keeps its plain dividers; only the roster draws boxes.
    "the board shelves them the same way",
    # Esc clears the filter only while the bar holds the keys.
    "until <code>esc</code> clears it",
    # No agent in the film has a colour, and the film has 21 agents, not fifty.
    "Fifty agents. Type three letters.",
    # A search-bar press takes no selection either.
    "on every surface it draws",
    # Fork is /fork; ⌃F is the finder.
    "<code>⌃F</code> forks",
    "⌃F forks",
]


def sections(html: str) -> list[str]:
    """The clip ids of a page's sections, in the order the page draws them."""
    return re.findall(r'<section id="([^"]+)"', html)


def prose() -> str:
    """Every word of copy the site carries, for a claim to be searched for."""
    parts = [s.heading + " " + s.prose + " " + s.label for s in pagecopy.SHOTS]
    parts += [t + " " + p for t, p in pagecopy.ALSO]
    parts += [pagecopy.FRIENDS_PROSE] + [d for _, d in pagecopy.META.values()]
    return " ".join(parts)


def rendered() -> str:
    """Both pages as served, hero and head included."""
    return page.index_page() + page.features_page()


def tape(clip: str) -> list[str]:
    with open(os.path.join(HERE, "tapes", clip + ".tape"), encoding="utf-8") as fh:
        return [
            line.strip() for line in fh if line.strip() and not line.startswith("#")
        ]


class TheFrontPage(unittest.TestCase):
    def test_it_is_the_eleven_the_owner_chose_in_their_order(self):
        self.assertEqual(
            [c for c in sections(page.index_page()) if c != "friends"], FRONT_PAGE
        )

    def test_it_leads_with_the_finder_then_the_drag(self):
        self.assertEqual(sections(page.index_page())[:2], ["33-jump", "34-drag"])

    def test_it_carries_exactly_the_shots_marked_for_it(self):
        home = [s.clip for s in pagecopy.SHOTS if s.home]
        self.assertEqual(home, FRONT_PAGE)

    def test_leaving_is_for_the_features_page_alone(self):
        self.assertNotIn("11-leaving", sections(page.index_page()))
        self.assertIn("11-leaving", sections(page.features_page()))

    def test_the_hero_names_the_finder_and_the_drag_to_place_panes(self):
        hero = page.hero()
        self.assertIn("<code>⌃F</code>", hero)
        self.assertIn("drag-to-place panes", hero)

    def test_the_page_still_carries_what_a_visitor_needs_after_the_clips(self):
        index = page.index_page()
        for heading in ("what you need", "how it's built", "there's more"):
            self.assertIn(heading, index)


class TheFeaturesPage(unittest.TestCase):
    def test_it_carries_every_shot_in_order_then_the_friends(self):
        got = sections(page.features_page())
        self.assertEqual(got, [s.clip for s in pagecopy.SHOTS] + ["friends"])

    def test_it_carries_every_also_note_with_its_words(self):
        html = page.features_page()
        for title, words in pagecopy.ALSO:
            with self.subTest(title=title):
                self.assertIn(f"<h3>{title}</h3>", html)
                self.assertGreater(len(words), 30)

    def test_every_section_plays_its_clip_and_shows_its_poster(self):
        html = rendered()
        for clip in CLIPS:
            with self.subTest(clip=clip):
                self.assertIn(f'src="media/{clip}.mp4"', html)
                self.assertIn(f'poster="media/{clip}.jpg"', html)


class TheTapes(unittest.TestCase):
    def test_every_clip_has_a_tape_that_records_it_where_the_page_reads_it(self):
        for clip in CLIPS:
            with self.subTest(clip=clip):
                self.assertTrue(
                    os.path.exists(os.path.join(HERE, "tapes", clip + ".tape"))
                )
                self.assertIn(f"Output .work/out/{clip}.mp4", tape(clip))

    def test_every_tape_ends_after_a_show(self):
        # VHS runs a tape that never calls Show a second time: the second pass types
        # the whole staging into the room the first one left running.
        for clip in CLIPS:
            with self.subTest(clip=clip):
                lines = [line.split()[0] for line in tape(clip)]
                self.assertIn("Show", lines)
                if "Hide" in lines:
                    last_hide = len(lines) - 1 - lines[::-1].index("Hide")
                    self.assertIn("Show", lines[last_hide:])

    def test_no_clip_is_listed_twice(self):
        self.assertEqual(len(CLIPS), len(set(CLIPS)))


class TheGestures(unittest.TestCase):
    """A mouse tape and its gestures/<clip>.json say the same thing: the tape sets
    WAKE_GESTURES to the clip, types exactly the keys the file binds, and the file
    loads. A key typed but unbound reaches wake as itself; one bound but never typed
    is a gesture nobody films."""

    def files(self) -> list[str]:
        directory = os.path.join(HERE, "gestures")
        return sorted(f[:-5] for f in os.listdir(directory) if f.endswith(".json"))

    def test_there_is_a_gesture_file_for_each_tape_that_exports_one(self):
        exporting = [c for c in CLIPS if f'Type "export WAKE_GESTURES={c}"' in tape(c)]
        self.assertEqual(sorted(exporting), self.files())

    def test_a_tape_types_exactly_the_keys_its_file_binds(self):
        for clip in self.files():
            with self.subTest(clip=clip):
                path = shiftkeys.gesture_path(clip, os.path.join(HERE, "gestures"))
                bound = {b[0] + 0x60 for b in shiftkeys.load_gestures(path)}
                lines = tape(clip)
                body = lines[lines.index("Show") + 1 :]
                typed = {
                    ord(m.group(1).lower())
                    for line in body
                    if (m := re.fullmatch(r"Ctrl\+([A-Za-z])", line))
                }
                self.assertEqual(typed, bound)


class TheWords(unittest.TestCase):
    def test_no_sentence_a_review_proved_false_has_come_back(self):
        text = rendered() + prose()
        for phrase in RETIRED:
            with self.subTest(phrase=phrase):
                self.assertFalse(phrase in text, f"the copy says {phrase!r} again")

    def test_no_words_say_the_finders_key_forks(self):
        # ⌃F is the agent finder; fork is /fork (the finder's own PR moved it).
        pattern = re.compile(
            r"(?:⌃F|Ctrl\+F|\^F)(?:</code>)?\s+(?:forks?|will fork|to fork)", re.I
        )
        self.assertIsNone(pattern.search(rendered()))
        with open(os.path.join(HERE, "captions.py"), encoding="utf-8") as fh:
            self.assertNotIn(
                '"^F"', fh.read(), "the film's fork caption still names ^F"
            )

    def test_the_fork_section_names_the_commands_that_fork(self):
        fork = next(s for s in pagecopy.SHOTS if s.clip == "08-fork")
        self.assertIn("<code>/fork</code>", fork.prose)
        self.assertIn("<code>/fork @maya</code>", fork.prose)

    def test_the_selection_note_names_both_places_a_press_does_not_select(self):
        note = dict(pagecopy.ALSO)["Drag, double- and triple-click"]
        self.assertIn("agent's own row", note)
        self.assertIn("search bar", note)

    def test_the_search_note_says_where_esc_clears_the_filter(self):
        search = next(s for s in pagecopy.SHOTS if s.clip == "35-search")
        self.assertIn("click the bar again", search.prose)

    def test_every_search_description_fits_a_results_page(self):
        for page_name, (title, description) in pagecopy.META.items():
            with self.subTest(page=page_name):
                self.assertLessEqual(len(description), 170)
                self.assertLessEqual(len(title), 70)

    def test_the_share_image_is_the_first_clips_poster_and_is_described_as_the_finder(
        self,
    ):
        head = page.page("index.html", "")
        self.assertIn(f"/media/{pagecopy.SHOTS[0].clip}.jpg", head)
        self.assertIn("finder", head.split("og:image:alt")[1].split(">")[0])


class ThePosters(unittest.TestCase):
    def test_a_poster_is_three_fifths_in_unless_a_shot_names_its_frame(self):
        self.assertAlmostEqual(page.poster_at("no-such-clip", 30), 18.0)
        with mock.patch.dict(pagecopy.POSTERS, {"no-such-clip": 4.5}):
            self.assertAlmostEqual(page.poster_at("no-such-clip", 30), 4.5)

    def test_a_named_frame_is_inside_its_clip(self):
        cuts = {s.clip: s.dur for s in pagecopy.SHOTS}
        for clip, at in pagecopy.POSTERS.items():
            with self.subTest(clip=clip):
                self.assertIn(clip, cuts)
                self.assertLess(at, cuts[clip])

    def test_encode_takes_the_poster_where_poster_at_says(self):
        with (
            mock.patch.object(page, "run") as run,
            mock.patch.object(page.os.path, "getsize", return_value=1),
            mock.patch.dict(pagecopy.POSTERS, {"no-such-clip": 4.5}),
        ):
            page.encode("no-such-clip", 1.0, 30, "/tmp/media")
        poster = run.call_args_list[1].args[0]
        self.assertEqual(poster[poster.index("-ss") + 1], "4.5")


class TheCuts(unittest.TestCase):
    def test_a_recording_as_long_as_every_cut_asks_is_fine(self):
        self.assertEqual(page.short_cuts(lambda clip: 1000.0), [])

    def test_a_cut_that_runs_past_its_recording_is_named(self):
        shot = pagecopy.SHOTS[0]
        have = shot.start + shot.dur - 1.0
        problems = page.short_cuts(lambda clip: have if clip == shot.clip else 1000.0)
        self.assertEqual(len(problems), 1)
        self.assertIn(shot.clip, problems[0])

    def test_a_rounding_of_a_fraction_of_a_second_is_not_a_short_cut(self):
        shot = pagecopy.SHOTS[0]
        have = shot.start + shot.dur - 0.1
        self.assertEqual(
            page.short_cuts(lambda clip: have if clip == shot.clip else 1000.0), []
        )

    def test_a_missing_recording_is_a_problem_not_a_traceback(self):
        def missing(clip: str) -> float:
            raise FileNotFoundError(clip)

        problems = page.short_cuts(missing)
        self.assertEqual(len(problems), len(CLIPS))
        self.assertIn("no recording", problems[0])


if __name__ == "__main__":
    unittest.main()
