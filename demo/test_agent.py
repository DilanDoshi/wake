"""Tests for the scripted stand-in `claude` (agent/claude), run as Wake runs it.

    python3 -m unittest discover -s demo

The big fleet's blocked agents start work on their own and stop at an ask, so what
the operator does to that card has to work: a card that can be shown but never
answered is a prop that breaks the first clip that presses ⌃X.
"""

import json
import os
import queue
import subprocess
import sys
import tempfile
import threading
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
AGENT = os.path.join(HERE, "agent", "claude")
SESSION = "00000000-0000-4000-8000-000000000001"

SCENARIO = {
    "model": "claude-opus-5",
    "autostart": {
        "at": 0.1,
        "tokens": 100,
        "steps": [
            {"after": 0.05, "text": "BEFORE the ask."},
            {"after": 0.05, "ask": {"tool": "Bash", "input": {"command": "true"}}},
            {"after": 0.05, "text": "AFTER the answer."},
        ],
    },
}


class Agent:
    """The stand-in on pipes, its stdout read on a thread so a test can wait."""

    def __init__(self, scenario: dict, role: str) -> None:
        self.home = tempfile.TemporaryDirectory()
        self.scenarios = tempfile.TemporaryDirectory()
        with open(os.path.join(self.scenarios.name, role + ".json"), "w") as fh:
            json.dump(scenario, fh)
        env = dict(
            os.environ,
            HOME=self.home.name,
            WAKE_DEMO_SCENARIOS=self.scenarios.name,
        )
        self.proc = subprocess.Popen(
            [sys.executable, AGENT, "--name", role, "--session-id", SESSION],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            env=env,
        )
        self.frames: "queue.Queue[dict]" = queue.Queue()
        self.reader = threading.Thread(target=self.read, daemon=True)
        self.reader.start()

    def read(self) -> None:
        for line in self.proc.stdout:
            try:
                self.frames.put(json.loads(line))
            except ValueError:
                pass

    def send(self, frame: dict) -> None:
        self.proc.stdin.write(json.dumps(frame) + "\n")
        self.proc.stdin.flush()

    def until(self, wanted, seconds: float = 8.0):
        """The first frame `wanted` accepts, or None when the time runs out."""
        deadline = seconds
        while deadline > 0:
            try:
                frame = self.frames.get(timeout=0.2)
            except queue.Empty:
                deadline -= 0.2
                continue
            if wanted(frame):
                return frame
        return None

    def close(self) -> None:
        self.proc.kill()
        self.proc.wait()
        self.reader.join(timeout=2)
        self.proc.stdin.close()
        self.proc.stdout.close()
        self.home.cleanup()
        self.scenarios.cleanup()


def says(text: str):
    def match(frame: dict) -> bool:
        blocks = frame.get("message", {}).get("content", [])
        return frame.get("type") == "assistant" and any(
            isinstance(b, dict) and text in (b.get("text") or "") for b in blocks
        )

    return match


def asks(frame: dict) -> bool:
    return frame.get("type") == "control_request" and (
        frame.get("request", {}).get("subtype") == "can_use_tool"
    )


class AutostartAsk(unittest.TestCase):
    def test_the_operator_answering_an_autostarted_ask_lets_its_turn_finish(self):
        agent = Agent(SCENARIO, "probe")
        self.addCleanup(agent.close)

        ask = agent.until(asks)
        self.assertIsNotNone(ask, "the agent never raised its ask")
        agent.send(
            {
                "type": "control_response",
                "response": {
                    "subtype": "success",
                    "request_id": ask["request_id"],
                    "response": {"behavior": "allow", "updatedInput": {}},
                },
            }
        )

        self.assertIsNotNone(
            agent.until(says("AFTER the answer.")),
            "the answer never reached the turn that asked",
        )
        self.assertIsNotNone(
            agent.until(lambda f: f.get("type") == "result"),
            "the turn never ended after its answer",
        )


if __name__ == "__main__":
    unittest.main()
