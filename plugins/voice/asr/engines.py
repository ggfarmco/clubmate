DEPS = {
    "transcribe": ["transcribe-cpp==0.4.1", "huggingface-hub==2.4.0"],
}

MODELS = {
    "transcribe": "handy-computer/nemotron-3.5-asr-streaming-0.6b-gguf/nemotron-3.5-asr-streaming-0.6b-Q8_0.gguf",
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


def load(name, model, chunk_ms):
    model = model or MODELS.get(name)
    if name == "transcribe":
        from transcribe_engine import TranscribeEngine

        return TranscribeEngine(model, chunk_ms)
    return FakeEngine()
