import importlib.util
import json
import os
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("child_outcomes", os.path.join(os.path.dirname(os.path.abspath(__file__)), "child-outcomes.py"))
co = importlib.util.module_from_spec(spec)
spec.loader.exec_module(co)

WT = "/repo/.worktrees/"


def session(sid, created, worktree, parent="orch", **kw):
    return dict(id=sid, title=sid, parentSession=parent, createdAt=created, worktree=worktree, **kw)


def metrics(cost, kind=""):
    return {"exp": {"kind": kind} if kind else {}, "calls": 1, "reported": None, "finished": None, "claude": {"cost": cost, "advisor": 0, "model": "claude-opus-5-5"}}


class BuildRowsTest(unittest.TestCase):
    def test_cost_to_landing_joins_fixes_and_reviewers_by_branch(self):
        sessions = [
            dict(id="orch", title="o", createdAt="2026-11-18T00:00:00Z", worktree="/repo"),
            session("build", "2026-11-18T01:00:00Z", WT + "feat/x"),
            session("fix", "2026-11-18T03:00:00Z", WT + "feat-x", closedBranch="feat/x"),
            session("shared", "2026-11-18T04:00:00Z", "/repo"),
            session("helper", "2026-11-18T01:30:00Z", WT + "feat/x", parent="build"),
            dict(id="rev", title="r", createdAt="2026-11-18T02:00:00Z", worktree=WT + "feat/x", parentSession="",
                 gateFor={"source": "feat/x", "tree": "t2"}),
        ]
        m = {"build": metrics(5.0), "fix": metrics(2.0), "helper": metrics(0.5), "shared": metrics(1.0), "rev": metrics(1.5)}
        verdicts = [
            {"by": "reviewer", "source": "feat/x", "reviewer": "rev", "round": 1, "outcome": "changes"},
            {"by": "reviewer", "source": "feat/x", "reviewer": "rev", "round": 2, "outcome": "approved"},
            {"by": "reviewer", "source": "feat/x", "reviewer": "rev", "round": 2, "outcome": "approved"},
            {"by": "user", "source": "feat/x", "outcome": "approved"},
        ]
        rows = {r["session"]: r for r in co.build_rows(sessions, m, verdicts, {"t2": "abc"})}
        self.assertEqual(set(rows), {"build", "fix", "helper", "shared"})
        b = rows["build"]
        self.assertEqual((b["gate_rounds"], b["changes"], b["squash"], b["parts"]), (2, 1, "abc", "fix helper"))
        self.assertEqual(b["cost_to_landing"], 9.0)
        self.assertEqual((rows["helper"]["part_of"], rows["helper"]["cost_to_landing"]), ("build", ""))
        self.assertEqual((rows["shared"]["branch"], rows["shared"]["part_of"]), ("", ""))


class ParseTest(unittest.TestCase):
    def test_exp_tag_after_clubmate_preamble(self):
        text = '[From Clubmate agent session "x" (agt_1), not from the user]\n\n[exp kind=fix size=S arm=sonnet/high]\n\nGoal'
        self.assertEqual(co.parse_exp(text), {"kind": "fix", "size": "S", "arm": "sonnet/high"})

    def test_claude_log_dedups_messages_and_splits_advisor(self):
        usage = {"iterations": [
            {"type": "message", "input_tokens": 1_000_000, "output_tokens": 0},
            {"type": "advisor_message", "model": "claude-fable-5-2", "input_tokens": 0, "output_tokens": 100_000},
        ]}
        line = json.dumps({"type": "assistant", "message": {"id": "m1", "model": "claude-opus-5-5", "usage": usage}}, separators=(",", ":"))
        with tempfile.NamedTemporaryFile("w", suffix=".jsonl", delete=False) as f:
            f.write(line + "\n" + line + "\n")
        try:
            got = co.read_claude_logs([f.name])
        finally:
            os.unlink(f.name)
        self.assertEqual((round(got["cost"], 6), round(got["advisor"], 6), got["model"]), (8.0, 4.0, "claude-opus-5-5"))


class CacheTest(unittest.TestCase):
    def test_cached_metrics_survive_a_deleted_log(self):
        with tempfile.TemporaryDirectory() as d:
            log = os.path.join(d, "a.jsonl")
            with open(log, "w") as f:
                f.write("{}\n")
            s = {"id": "agt_x", "agentSessionId": "a"}
            cached = {"exp": {"kind": "fix"}, "calls": 7, "claude": {"cost": 3.0, "advisor": 1.0, "model": "claude-opus-5-5"}}
            cache = {"agt_x": {"key": [[], co.stamp([log])], "m": cached}}
            os.unlink(log)
            self.assertEqual(co.session_metrics([s], cache, {})["agt_x"], cached)


if __name__ == "__main__":
    unittest.main()
