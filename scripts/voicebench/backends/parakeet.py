# /// script
# requires-python = "==3.12.*"
# dependencies = ["parakeet-mlx==0.5.3"]
# ///
import argparse
import contextlib
import json
import sys
import time

from parakeet_mlx import from_pretrained


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--model", required=True)
    p.add_argument("--lang", choices=["ru", "auto"], default="auto")
    p.add_argument("--hint")
    p.add_argument("audio", nargs="+")
    a = p.parse_args()
    out, sys.stdout = sys.stdout, sys.stderr
    model = from_pretrained(a.model)

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
