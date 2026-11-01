DEPS = {
    "nemotron": ["transformers==5.22.1", "torch==2.15.0", "librosa==1.0.0"],
    "whisper": ["mlx-whisper==0.4.3"],
}

MODELS = {
    "nemotron": "nvidia/nemotron-3.5-asr-streaming-0.6b",
    "whisper": "mlx-community/whisper-large-v3-turbo",
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
    if name == "nemotron":
        from nemotron_engine import NemotronEngine

        return NemotronEngine(model, chunk_ms)
    if name == "whisper":
        from whisper_engine import WhisperEngine

        return WhisperEngine(model)
    return FakeEngine()
