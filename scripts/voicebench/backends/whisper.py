# /// script
# requires-python = "==3.12.*"
# dependencies = ["mlx-whisper==0.4.3"]
# ///
import argparse
import contextlib
import json
import sys
import time

import mlx.core as mx
from mlx_whisper.transcribe import ModelHolder, transcribe


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--model", required=True)
    p.add_argument("--lang", choices=["ru", "auto"], default="auto")
    p.add_argument("--hint")
    p.add_argument("audio", nargs="+")
    a = p.parse_args()
    out, sys.stdout = sys.stdout, sys.stderr
    ModelHolder.get_model(a.model, mx.float16)
    lang = "ru" if a.lang == "ru" else None

    def run(path):
        return transcribe(path, path_or_hf_repo=a.model, language=lang, initial_prompt=a.hint)["text"]

    with contextlib.suppress(Exception):
        run(a.audio[0])
    for path in a.audio:
        try:
            t = time.perf_counter()
            text = run(path)
            row = {"audio": path, "text": text.strip(), "ms": round((time.perf_counter() - t) * 1000)}
        except Exception as e:
            row = {"audio": path, "error": f"{type(e).__name__}: {e}"}
        print(json.dumps(row, ensure_ascii=False), file=out, flush=True)


main()
