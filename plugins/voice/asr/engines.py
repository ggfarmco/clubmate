DEPS = {
    "transcribe": ["transcribe-cpp==0.4.1", "huggingface-hub==2.4.0"],
}

MODELS = {
    "transcribe": (
        "handy-computer/nemotron-3.5-asr-streaming-0.6b-gguf/nemotron-3.5-asr-streaming-0.6b-Q8_0.gguf",
        "53ce288f69326b415aeef030cfcb02a08a8096dd",
        "b94545b313b3223fda7b2857a52681da813935c2127643d1e9ff0c23d988089c",
    ),
}


class Engine:
    streaming = True

    def start(self, id, lang, hint):
        raise NotImplementedError

    def feed(self, id, pcm):
        raise NotImplementedError

    def finish(self, id):
        raise NotImplementedError

    def cancel(self, id):
        raise NotImplementedError


class FakeEngine(Engine):
    def __init__(self):
        self.sizes = {}

    def start(self, id, lang, hint):
        self.sizes[id] = 0

    def feed(self, id, pcm):
        self.sizes[id] += len(pcm)
        return str(self.sizes[id])

    def finish(self, id):
        return f"fake {self.sizes.pop(id)}"

    def cancel(self, id):
        self.sizes.pop(id, None)


def load(name, model, revision, sha256, chunk_ms):
    if not model and name in MODELS:
        model, revision, sha256 = MODELS[name]
    if name == "transcribe":
        from transcribe_engine import TranscribeEngine

        return TranscribeEngine(model, revision, sha256, chunk_ms)
    return FakeEngine()
