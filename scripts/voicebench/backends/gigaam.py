# /// script
# requires-python = "==3.12.*"
# dependencies = [
#   "gigaam @ git+https://github.com/salute-developers/GigaAM@7447938d791c4f3e643386ee22c33777004293a5",
#   "torch==2.10.*",
#   "torchaudio==2.10.*",
# ]
# ///
import argparse
import contextlib
import importlib
import json
import os
import sys
import time


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--model", required=True)
    p.add_argument("--lang", choices=["ru", "auto"], default="auto")
    p.add_argument("--hint")
    p.add_argument("audio", nargs="+")
    a = p.parse_args()
    out, sys.stdout = sys.stdout, sys.stderr
    here = os.path.dirname(os.path.abspath(__file__))
    sys.path[:] = [d for d in sys.path if os.path.abspath(d or ".") != here]
    gigaam = importlib.import_module("gigaam")
    model = gigaam.load_model(a.model, fp16_encoder=False, device="cpu")

    def run(path):
        return model.transcribe(path).text

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
