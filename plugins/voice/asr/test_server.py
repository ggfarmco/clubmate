import base64
import json
import pathlib
import subprocess
import sys

import pytest


SERVER = pathlib.Path(__file__).with_name("server.py")


@pytest.fixture
def server():
    proc = subprocess.Popen(
        [sys.executable, str(SERVER), "--engine", "fake"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        text=True,
    )
    yield proc
    proc.kill()
    proc.wait()


def send(proc, **msg):
    proc.stdin.write(json.dumps(msg) + "\n")
    proc.stdin.flush()


def read(proc):
    return json.loads(proc.stdout.readline())


def pcm(n):
    return base64.b64encode(bytes(n)).decode()


def test_ready_then_partial_and_final(server):
    assert read(server) == {"ev": "ready", "engine": "fake", "streaming": True}
    send(server, op="start", id="a", lang="ru", hint="")
    send(server, op="audio", id="a", pcm=pcm(320))
    assert read(server) == {"ev": "partial", "id": "a", "text": "320"}
    send(server, op="audio", id="a", pcm=pcm(320))
    assert read(server) == {"ev": "partial", "id": "a", "text": "640"}
    send(server, op="end", id="a")
    final = read(server)
    assert (final["ev"], final["id"], final["text"]) == ("final", "a", "fake 640")
    assert isinstance(final["ms"], int)


def test_two_streams_interleaved(server):
    read(server)
    send(server, op="start", id="a", lang="ru", hint="")
    send(server, op="start", id="b", lang="auto", hint="")
    send(server, op="audio", id="a", pcm=pcm(100))
    send(server, op="audio", id="b", pcm=pcm(7))
    send(server, op="audio", id="a", pcm=pcm(100))
    send(server, op="end", id="b")
    send(server, op="end", id="a")
    got = [read(server) for _ in range(5)]
    assert [(e["ev"], e["id"], e["text"]) for e in got] == [
        ("partial", "a", "100"),
        ("partial", "b", "7"),
        ("partial", "a", "200"),
        ("final", "b", "fake 7"),
        ("final", "a", "fake 200"),
    ]


def test_audio_for_unknown_id_is_error(server):
    read(server)
    send(server, op="audio", id="zz", pcm=pcm(10))
    err = read(server)
    assert (err["ev"], err["id"]) == ("error", "zz")
    send(server, op="start", id="a", lang="ru", hint="")
    send(server, op="audio", id="a", pcm=pcm(10))
    assert read(server) == {"ev": "partial", "id": "a", "text": "10"}
