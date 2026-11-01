# /// script
# requires-python = "==3.12.*"
# dependencies = ["transformers==5.22.1", "torch==2.15.0", "librosa==1.0.0"]
# ///
import argparse
import contextlib
import json
import sys
import time

import librosa
import torch
from transformers import AutoModelForRNNT, AutoProcessor

LOOKAHEAD = {80: 0, 320: 3, 560: 6, 1120: 13}


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--model", required=True)
    p.add_argument("--lang", choices=["ru", "auto"], default="auto")
    p.add_argument("--hint")
    p.add_argument("--chunk-ms", type=int, choices=sorted(LOOKAHEAD), default=1120)
    p.add_argument("audio", nargs="+")
    a = p.parse_args()
    out, sys.stdout = sys.stdout, sys.stderr
    device = "mps" if torch.backends.mps.is_available() else "cpu"
    processor = AutoProcessor.from_pretrained(a.model)
    processor.set_num_lookahead_tokens(LOOKAHEAD[a.chunk_ms])
    model = AutoModelForRNNT.from_pretrained(a.model).to(device).eval()
    rate = processor.feature_extractor.sampling_rate
    lang = "ru-RU" if a.lang == "ru" else "auto"

    def run(path):
        audio, _ = librosa.load(path, sr=rate, mono=True)
        inputs = processor(audio, sampling_rate=rate, language=lang, return_tensors="pt").to(device)
        with torch.inference_mode():
            output = model.generate(**inputs, return_dict_in_generate=True)
        text = processor.decode(output.sequences, skip_special_tokens=True)
        return text[0] if isinstance(text, list) else text

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
