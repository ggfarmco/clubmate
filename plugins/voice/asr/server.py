# /// script
# requires-python = "==3.12.*"
# dependencies = []
# ///
import argparse
import base64
import contextlib
import json
import os
import shutil
import sys
import time

import engines


MARKER = "CLUBMATE_VOICE_ASR_ENV"


def find_uv():
    for path in (os.environ.get("UV"), shutil.which("uv"), os.path.expanduser("~/.local/bin/uv"), "/opt/homebrew/bin/uv"):
        if path and os.access(path, os.X_OK):
            return path
    sys.exit("voice needs uv (https://docs.astral.sh/uv/)")


def reexec(engine):
    uv = find_uv()
    withs = [arg for dep in engines.DEPS[engine] for arg in ("--with", dep)]
    os.environ[MARKER] = "1"
    os.execv(uv, [uv, "run", "--quiet", *withs, "--script", os.path.abspath(__file__), *sys.argv[1:]])


def protocol_out():
    out = os.fdopen(os.dup(1), "w", encoding="utf-8")
    os.dup2(2, 1)
    sys.stdout = sys.stderr

    def emit(**ev):
        out.write(json.dumps(ev, ensure_ascii=False) + "\n")
        out.flush()

    return emit


def serve(engine, emit, lines):
    active, last = set(), {}

    def drop(id):
        active.discard(id)
        last.pop(id, None)
        with contextlib.suppress(Exception):
            engine.cancel(id)

    for raw in lines:
        if not raw.strip():
            continue
        id = None
        try:
            msg = json.loads(raw)
            op, id = msg.get("op"), msg.get("id")
            if op == "start":
                drop(id)
                engine.start(id, msg.get("lang") or "auto", msg.get("hint") or "")
                active.add(id)
            elif op == "cancel":
                drop(id)
            elif id not in active:
                emit(ev="error", id=id, message="unknown utterance")
            elif op == "audio":
                text = engine.feed(id, base64.b64decode(msg["pcm"]))
                if text and text != last.get(id):
                    last[id] = text
                    emit(ev="partial", id=id, text=text)
            elif op == "end":
                t = time.perf_counter()
                text = engine.finish(id)
                active.discard(id)
                last.pop(id, None)
                emit(ev="final", id=id, text=text, ms=round((time.perf_counter() - t) * 1000))
            else:
                emit(ev="error", id=id, message=f"unknown op {op!r}")
        except Exception as e:
            drop(id)
            emit(ev="error", id=id, message=f"{type(e).__name__}: {e}")


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--engine", choices=["nemotron", "whisper", "fake"], required=True)
    p.add_argument("--model")
    p.add_argument("--chunk-ms", type=int, default=320)
    a = p.parse_args()
    if a.engine != "fake" and not os.environ.get(MARKER):
        reexec(a.engine)
    emit = protocol_out()
    engine = engines.load(a.engine, a.model, a.chunk_ms)
    emit(ev="ready", engine=a.engine, streaming=engine.streaming)
    serve(engine, emit, sys.stdin.buffer)


if __name__ == "__main__":
    main()
