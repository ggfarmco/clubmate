import array
import hashlib
import os
import time

import transcribe_cpp as tc
from huggingface_hub import hf_hub_download

from engines import Engine


RATE = 16000
EVERY_S = 1.0
LOOKAHEAD = {80: 0, 320: 3, 560: 6, 1120: 13}
LOCALES = {"ru": "ru-RU", "en": "en-US"}


class Stream:
    def __init__(self, lang, hint):
        self.lang = lang
        self.hint = hint
        self.audio = array.array("f")
        self.decoded = 0
        self.at = 0.0
        self.text = ""
        self.live = None


class TranscribeEngine(Engine):
    def __init__(self, model, revision, sha256, chunk_ms):
        if chunk_ms not in LOOKAHEAD:
            raise ValueError(f"transcribe supports --chunk-ms {sorted(LOOKAHEAD)}")
        if not revision or not sha256:
            raise ValueError("transcribe needs --revision and --sha256 for the model")
        repo, filename = os.path.split(model)
        self.model = tc.Model(verified(hf_hub_download(repo, filename, revision=revision), sha256))
        self.session = self.model.session()
        self.streaming = self.model.capabilities.supports_streaming
        self.family = tc.ParakeetStreamOptions(att_context_right=LOOKAHEAD[chunk_ms]) if self.streaming else None
        self.streams = {}
        self.warm_up()

    def warm_up(self):
        self.start("warm-up", "ru", "")
        noise = array.array("h", (int(300 * ((i * 7919) % 200 - 100) / 100) for i in range(RATE)))
        self.feed("warm-up", noise.tobytes())
        self.finish("warm-up")

    def start(self, id, lang, hint):
        if self.streaming and any(s.live for s in self.streams.values()):
            raise RuntimeError("another utterance is being recognized")
        s = Stream(None if lang == "auto" else lang, hint or None)
        if self.streaming:
            s.live = self.session.stream(language=LOCALES.get(s.lang, s.lang), family=self.family)
        self.streams[id] = s

    def feed(self, id, pcm):
        s = self.streams[id]
        samples = array.array("h", pcm)
        audio = array.array("f", (v / 32768 for v in samples))
        if s.live:
            s.live.feed(audio)
            return s.live.text().display.strip()
        s.audio.extend(audio)
        if len(s.audio) - s.decoded >= RATE * EVERY_S and time.monotonic() - s.at >= EVERY_S:
            s.text = self.run(s)
            s.decoded, s.at = len(s.audio), time.monotonic()
        return s.text

    def finish(self, id):
        s = self.streams.pop(id)
        if not s.live:
            return self.run(s)
        try:
            s.live.finalize()
            return s.live.snapshot().text.strip()
        finally:
            s.live.reset()

    def cancel(self, id):
        s = self.streams.pop(id, None)
        if s and s.live:
            s.live.reset()

    def run(self, s):
        prompt = tc.WhisperRunOptions(initial_prompt=s.hint) if s.hint else None
        return self.session.run(s.audio, language=s.lang, family=prompt).text.strip()


def verified(path, sha256):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    if h.hexdigest() != sha256:
        raise ValueError(f"{os.path.basename(path)}: sha256 {h.hexdigest()} does not match the pinned {sha256}")
    return path
