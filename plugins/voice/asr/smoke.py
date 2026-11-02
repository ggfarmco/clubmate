import argparse
import base64
import json
import pathlib
import statistics
import subprocess
import threading
import time


SERVER = pathlib.Path(__file__).with_name("server.py")
RATE = 16000


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--engine", required=True)
    p.add_argument("--chunk-ms", type=int, default=320)
    p.add_argument("--lang", default="ru")
    p.add_argument("--hint", default="")
    p.add_argument("--model")
    p.add_argument("pcm")
    a = p.parse_args()
    audio = pathlib.Path(a.pcm).read_bytes()
    size = RATE * 2 * a.chunk_ms // 1000
    t0 = time.perf_counter()
    proc = subprocess.Popen(
        ["uv", "run", "--quiet", "--script", str(SERVER), "--engine", a.engine, "--chunk-ms", str(a.chunk_ms), *(["--model", a.model] if a.model else [])],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        text=True,
    )
    ready = json.loads(proc.stdout.readline())
    load_s = round(time.perf_counter() - t0, 1)
    events, sent = [], []

    def reader():
        for line in proc.stdout:
            events.append((time.perf_counter(), json.loads(line)))
            if events[-1][1]["ev"] in ("final", "error"):
                return

    def send(**msg):
        proc.stdin.write(json.dumps(msg, ensure_ascii=False) + "\n")
        proc.stdin.flush()

    thread = threading.Thread(target=reader)
    thread.start()
    send(op="start", id="s", lang=a.lang, hint=a.hint)
    for i in range(0, len(audio), size):
        sent.append(time.perf_counter())
        send(op="audio", id="s", pcm=base64.b64encode(audio[i : i + size]).decode())
        time.sleep(max(0.0, sent[-1] + a.chunk_ms / 1000 - time.perf_counter()))
    ended = time.perf_counter()
    send(op="end", id="s")
    thread.join()
    proc.stdin.close()
    proc.wait()
    partials = [(t, e) for t, e in events if e["ev"] == "partial"]
    latency = [round((t - max(s for s in sent if s <= t)) * 1000) for t, _ in partials]
    final = events[-1][1]
    print(json.dumps({
        "ready": ready,
        "load_s": load_s,
        "audio_s": round(len(audio) / 2 / RATE, 2),
        "chunks": len(sent),
        "partials": [e["text"] for _, e in partials],
        "partial_latency_ms": {"median": statistics.median(latency), "max": max(latency)} if latency else None,
        "final": final,
        "final_wall_ms": round((events[-1][0] - ended) * 1000),
    }, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
