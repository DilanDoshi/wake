"""Tests that the big fleet's table (setup.sh) and its scenarios agree.

    python3 -m unittest discover -s demo

stage-fleet.tape is generated from the `fleet` array in setup.sh, and each agent
that is blocked or busy on camera is scripted by demo/scenarios/<name>.json. The
two are written apart, so nothing else notices when one drifts: an agent renamed
in the table loses its script and falls back to a pool variant, and the finder's
first screen no longer shows three agents waiting on you.
"""

import json
import os
import re
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
HARBOR_DIRS = {"api", "web", "cli", "docs"}
BLOCKED = {"pay-audit", "infra-terraform", "web-billing"}
BUSY = {
    "pay-ledger",
    "infra-blue-green",
    "gw-limits",
    "gw-timeouts",
    "web-usage-panel",
    "docs-changelog",
}


def setup_text() -> str:
    with open(os.path.join(HERE, "setup.sh"), encoding="utf-8") as fh:
        return fh.read()


def fleet() -> list[tuple[str, str, str]]:
    """The rows of setup.sh's fleet array: name, directory, team (- for none)."""
    block = re.search(r"^fleet=\(\n(.*?)^\)", setup_text(), re.S | re.M)
    assert block, "setup.sh has no fleet array"
    return re.findall(r'^\s*"(\S+) (\S+) (\S+)"$', block.group(1), re.M)


def scenario(name: str) -> dict:
    with open(os.path.join(HERE, "scenarios", name + ".json"), encoding="utf-8") as fh:
        return json.load(fh)


class TheTable(unittest.TestCase):
    def test_it_is_twenty_one_agents_with_distinct_valid_names(self):
        names = [name for name, _, _ in fleet()]
        self.assertEqual(len(names), 21)
        self.assertEqual(len(names), len(set(names)))
        for name in names:
            with self.subTest(name=name):
                self.assertRegex(name, r"^[A-Za-z0-9_-]+$")
                self.assertFalse(name.startswith("manager-"))

    def test_every_directory_is_one_of_harbors(self):
        for name, directory, _ in fleet():
            with self.subTest(name=name):
                self.assertIn(directory, HARBOR_DIRS)
                self.assertTrue(os.path.isdir(os.path.join(HERE, "harbor", directory)))

    def test_there_are_five_teams_and_two_loose_agents(self):
        teams = {team for _, _, team in fleet() if team != "-"}
        self.assertEqual(len(teams), 5)
        self.assertEqual(len([1 for _, _, team in fleet() if team == "-"]), 2)

    def test_the_wanted_count_is_the_table_and_the_manager(self):
        self.assertIn("WANT_AGENTS=$(( ${#fleet[@]} + 1 ))", setup_text())


class TheScenarios(unittest.TestCase):
    def test_the_blocked_and_busy_agents_are_in_the_fleet_and_no_one_else_is_scripted(
        self,
    ):
        names = {name for name, _, _ in fleet()}
        scripted = {
            f[:-5]
            for f in os.listdir(os.path.join(HERE, "scenarios"))
            if f.endswith(".json") and not f.startswith("_")
        }
        self.assertEqual(scripted & names, BLOCKED | BUSY)

    def test_each_opens_in_the_directory_the_table_puts_it_in(self):
        for name, directory, _ in fleet():
            if name in BLOCKED | BUSY:
                with self.subTest(name=name):
                    text = scenario(name)["opening"]["steps"][0]["text"]
                    self.assertIn(f"`{directory}/`", text)

    def test_a_blocked_agent_starts_work_and_stops_at_an_ask(self):
        for name in BLOCKED:
            with self.subTest(name=name):
                steps = scenario(name)["autostart"]["steps"]
                self.assertIn("ask", steps[-1])
                self.assertIn("text", steps[0])

    def test_a_busy_agent_holds_a_tool_for_far_longer_than_a_take(self):
        for name in BUSY:
            with self.subTest(name=name):
                tool = scenario(name)["autostart"]["steps"][-1]["tool"]
                self.assertGreaterEqual(tool["takes"], 600)
                self.assertIn("result", tool)

    def test_the_rest_take_a_pool_variant(self):
        self.assertTrue(os.path.exists(os.path.join(HERE, "scenarios", "_pool.json")))


if __name__ == "__main__":
    unittest.main()
