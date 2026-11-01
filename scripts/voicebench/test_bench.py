import json
import subprocess

from bench import run
from score import Phrase


PHRASES = [Phrase("a1", "answer", "да", []), Phrase("a2", "answer", "нет", [])]


def wavs_of(cmd):
    return [a for a in cmd if a.endswith(".wav")]


def fake_ok(cmd, **kw):
    out = "".join(json.dumps({"audio": w, "text": "да", "ms": 5}) + "\n" for w in wavs_of(cmd))
    return subprocess.CompletedProcess(cmd, 0, out, "")


def test_run_skips_missing_audio(tmp_path):
    (tmp_path / "mac").mkdir()
    (tmp_path / "mac" / "a1.wav").write_bytes(b"")
    rows, missing = run([("fake", "m")], PHRASES, str(tmp_path), None, runner=fake_ok)
    assert [r["id"] for r in rows] == ["a1"]
    assert missing == ["mac/a2", "iphone/a1", "iphone/a2"]


def test_run_records_backend_error(tmp_path):
    (tmp_path / "mac").mkdir()
    for i in ("a1", "a2"):
        (tmp_path / "mac" / f"{i}.wav").write_bytes(b"")

    def fake(cmd, **kw):
        wavs = wavs_of(cmd)
        out = json.dumps({"audio": wavs[0], "text": "да", "ms": 5}) + "\n" + json.dumps({"audio": wavs[1], "error": "boom"}) + "\n"
        return subprocess.CompletedProcess(cmd, 0, out, "")

    rows, _ = run([("fake", "m")], PHRASES, str(tmp_path), None, runner=fake)
    assert [r.get("error") for r in rows] == [None, "boom"]
