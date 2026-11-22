"""Tests for the landing site's page layout (page.py) and its words (pagecopy.py).

    python3 -m unittest discover -s demo

They render the two pages from the copy, with no ffmpeg and no network, and hold
what the pages claim to what the repo can check: the order the front page leads
in, that every clip has a tape it was filmed from, and that the words do not still
describe a key the build has since moved.
"""

import os
import re
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import page  # noqa: E402
import pagecopy  # noqa: E402

CLIPS = [s.clip for s in pagecopy.SHOTS] + [f.clip for f in pagecopy.FRIENDS]


def sections(html: str) -> list[str]:
    """The clip ids of a page's sections, in the order the page draws them."""
    return re.findall(r'<section id="([^"]+)"', html)


def prose() -> str:
    """Every word of copy the site carries, for a claim to be searched for."""
    parts = [s.heading + " " + s.prose + " " + s.label for s in pagecopy.SHOTS]
    parts += [t + " " + p for t, p in pagecopy.ALSO]
    parts += [pagecopy.FRIENDS_PROSE] + [d for _, d in pagecopy.META.values()]
    return " ".join(parts)


class TheFrontPage(unittest.TestCase):
    def test_it_leads_with_the_finder_then_the_drag(self):
        self.assertEqual(sections(page.index_page())[:2], ["33-jump", "34-drag"])

    def test_it_carries_exactly_the_shots_marked_for_it_in_order(self):
        home = [s.clip for s in pagecopy.SHOTS if s.home]
        self.assertEqual(
            [c for c in sections(page.index_page()) if c != "friends"], home
        )

    def test_leaving_is_for_the_features_page_alone(self):
        self.assertNotIn("11-leaving", sections(page.index_page()))
        self.assertIn("11-leaving", sections(page.features_page()))

    def test_the_hero_names_the_finder_and_the_drag(self):
        hero = page.hero()
        self.assertIn("⌃F", hero)
        self.assertIn("drag", hero.lower())


class TheFeaturesPage(unittest.TestCase):
    def test_it_carries_every_shot_in_order_then_the_friends(self):
        got = sections(page.features_page())
        self.assertEqual(got, [s.clip for s in pagecopy.SHOTS] + ["friends"])

    def test_it_carries_every_also_note(self):
        html = page.features_page()
        for title, _ in pagecopy.ALSO:
            with self.subTest(title=title):
                self.assertIn(f"<h3>{title}</h3>", html)


class TheCopy(unittest.TestCase):
    def test_every_clip_is_filmed_from_a_tape_in_the_repo(self):
        for clip in CLIPS:
            with self.subTest(clip=clip):
                self.assertTrue(
                    os.path.exists(os.path.join(HERE, "tapes", clip + ".tape")),
                    f"{clip} has no demo/tapes/{clip}.tape",
                )

    def test_no_clip_is_listed_twice(self):
        self.assertEqual(len(CLIPS), len(set(CLIPS)))

    def test_no_words_still_say_ctrl_f_forks(self):
        # ⌃F is the agent finder; fork is /fork (the finder's own PR moved it).
        for claim in ("<code>⌃F</code> forks", "⌃F forks"):
            self.assertFalse(claim in prose(), f"the copy still says {claim!r}")

    def test_the_fork_section_names_the_command_that_forks(self):
        fork = next(s for s in pagecopy.SHOTS if s.clip == "08-fork")
        self.assertIn("<code>/fork</code>", fork.prose)

    def test_a_press_on_an_agents_row_is_not_said_to_select_text(self):
        # A press on a sidebar agent's own row starts a drag, so "every surface"
        # is not true of selection any more.
        claim = "on every surface it draws"
        self.assertFalse(claim in prose(), f"the copy still says {claim!r}")

    def test_every_search_description_fits_a_results_page(self):
        for page_name, (title, description) in pagecopy.META.items():
            with self.subTest(page=page_name):
                self.assertLessEqual(len(description), 170)
                self.assertLessEqual(len(title), 70)

    def test_the_share_image_is_the_first_clips_poster(self):
        head = page.page("index.html", "")
        self.assertIn(f"/media/{pagecopy.SHOTS[0].clip}.jpg", head)


if __name__ == "__main__":
    unittest.main()
