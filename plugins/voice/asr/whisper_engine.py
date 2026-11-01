import time

import mlx.core as mx
import numpy as np
from mlx_whisper.transcribe import ModelHolder, transcribe

from engines import Engine


RATE = 16000
EVERY_S = 1.0


class Stream:
    def __init__(self, lang, hint):
        self.lang = None if lang == "auto" else lang
        self.hint = hint or None
        self.audio = np.zeros(0, np.float32)
        self.decoded = 0
        self.at = 0.0
        self.text = ""


class WhisperEngine(Engine):
    streaming = False

    def __init__(self, model):
        self.model = model
        ModelHolder.get_model(model, mx.float16)
        self.streams = {}
        self.run(np.zeros(RATE, np.float32), Stream("ru", ""))

    def run(self, audio, s, **opts):
        out = transcribe(audio, path_or_hf_repo=self.model, language=s.lang, initial_prompt=s.hint, verbose=None, **opts)
        return out["text"].strip()

    def start(self, id, lang, hint):
        self.streams[id] = Stream(lang, hint)

    def feed(self, id, pcm):
        s = self.streams[id]
        s.audio = np.concatenate([s.audio, np.frombuffer(pcm, np.int16).astype(np.float32) / 32768])
        if len(s.audio) - s.decoded >= RATE * EVERY_S and time.monotonic() - s.at >= EVERY_S:
            s.text = self.run(s.audio, s, temperature=0.0)
            s.decoded, s.at = len(s.audio), time.monotonic()
        return s.text

    def finish(self, id):
        s = self.streams.pop(id)
        return self.run(s.audio, s)

    def cancel(self, id):
        self.streams.pop(id, None)
