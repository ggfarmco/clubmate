import numpy as np
import torch
from transformers import AutoModelForRNNT, AutoProcessor
from transformers.models.nemotron3_5_asr.generation_nemotron3_5_asr import Nemotron3_5AsrRNNTDecoderCache

from engines import Engine


LOOKAHEAD = {80: 0, 320: 3, 560: 6, 1120: 13}
LANGS = {"ru": "ru-RU", "en": "en-US"}
RATE = 16000


class Stream:
    def __init__(self, prompt, blank):
        self.prompt = prompt
        self.audio = np.zeros(0, np.float32)
        self.base = 0
        self.frames = 0
        self.encoder = {}
        self.cache = None
        self.last = blank
        self.tokens = []


class NemotronEngine(Engine):
    streaming = True

    def __init__(self, model, chunk_ms):
        if chunk_ms not in LOOKAHEAD:
            raise ValueError(f"nemotron supports --chunk-ms {sorted(LOOKAHEAD)}")
        self.device = "mps" if torch.backends.mps.is_available() else "cpu"
        self.dtype = torch.float16 if self.device == "mps" else torch.float32
        self.lookahead = LOOKAHEAD[chunk_ms]
        self.processor = AutoProcessor.from_pretrained(model)
        self.processor.set_num_lookahead_tokens(self.lookahead)
        self.features = self.processor.feature_extractor
        self.model = AutoModelForRNNT.from_pretrained(model, dtype=self.dtype).to(self.device).eval()
        self.model.decoder.to("cpu", torch.float32)
        self.model.joint.to("cpu", torch.float32)
        self.blank = self.model.config.blank_token_id
        self.first_frames = self.processor.num_mel_frames_first_audio_chunk
        self.chunk_frames = self.processor.num_mel_frames_per_audio_chunk
        self.streams = {}
        self.warm_up()

    def warm_up(self):
        self.start("warm-up", "ru", "")
        noise = np.random.default_rng(0).normal(0, 300, RATE).astype(np.int16)
        self.feed("warm-up", noise.tobytes())
        self.finish("warm-up")

    def start(self, id, lang, hint):
        lang = LANGS.get(lang, lang)
        if lang not in self.processor.prompt_dictionary:
            raise ValueError(f"unknown language {lang!r}")
        prompt = torch.tensor([self.processor.prompt_dictionary[lang]], device=self.device)
        self.streams[id] = Stream(prompt, self.blank)

    def feed(self, id, pcm):
        s = self.streams[id]
        s.audio = np.concatenate([s.audio, np.frombuffer(pcm, np.int16).astype(np.float32) / 32768])
        while True:
            start, end = self.span(s)
            if s.base + len(s.audio) < end:
                break
            self.step(s, start, end)
        start, _ = self.span(s)
        s.audio, s.base = s.audio[start - s.base :], start
        return self.text(s)

    def finish(self, id):
        s = self.streams.pop(id)
        stop = s.base + len(s.audio) + self.chunk_frames * self.features.hop_length
        while s.frames * self.features.hop_length < stop:
            start, end = self.span(s)
            short = end - s.base - len(s.audio)
            if short > 0:
                s.audio = np.concatenate([s.audio, np.zeros(short, np.float32)])
            self.step(s, start, end)
        return self.text(s)

    def cancel(self, id):
        self.streams.pop(id, None)

    def span(self, s):
        if s.frames == 0:
            return 0, self.processor.num_samples_first_audio_chunk
        start = s.frames * self.features.hop_length - self.features.n_fft // 2
        return start, start + self.processor.num_samples_per_audio_chunk

    def step(self, s, start, end):
        first = s.frames == 0
        frames = self.first_frames if first else self.chunk_frames
        audio = s.audio[start - s.base : end - s.base]
        mel = self.features(audio, sampling_rate=RATE, center=first, return_tensors="pt")["input_features"][:, :frames]
        with torch.inference_mode():
            out = self.model.get_audio_features(
                input_features=mel.to(self.device, self.dtype),
                num_lookahead_tokens=self.lookahead,
                use_cache=True,
                output_attention_mask=False,
                prompt_ids=s.prompt,
                **s.encoder,
            )
            s.encoder = {"past_key_values": out.past_key_values, "padding_cache": out.padding_cache}
            self.decode(s, out.pooler_output.to("cpu", torch.float32))
        s.frames += frames

    def decode(self, s, hidden):
        if s.cache is None:
            s.cache = Nemotron3_5AsrRNNTDecoderCache(self.model.config)
        for t in range(hidden.shape[1]):
            frame = hidden[:, t : t + 1]
            for _ in range(self.model.max_symbols_per_step):
                decoded = self.model.decoder(torch.tensor([[s.last]]), cache=s.cache)
                token = int(self.model.joint(decoder_hidden_states=decoded, encoder_hidden_states=frame)[0, -1].argmax())
                if token == self.blank:
                    s.last = self.blank
                    break
                s.tokens.append(token)
                s.last = token

    def text(self, s):
        return self.processor.tokenizer.decode(s.tokens, skip_special_tokens=True).strip()
