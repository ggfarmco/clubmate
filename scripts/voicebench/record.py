#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# ///
import argparse
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from score import load_phrases


HERE = os.path.dirname(os.path.abspath(__file__))
DEVICE_NAMES = {"mac": "MacBook", "iphone": "iPhone"}


def list_audio_devices() -> list[tuple[int, str]]:
    out = subprocess.run(
        ["ffmpeg", "-hide_banner", "-f", "avfoundation", "-list_devices", "true", "-i", ""],
        capture_output=True,
        text=True,
    ).stderr
    devices, audio = [], False
    for line in out.splitlines():
        if "audio devices" in line:
            audio = True
            continue
        m = re.search(r"\[(\d+)\] (.+)$", line)
        if audio and m:
            devices.append((int(m.group(1)), m.group(2).strip()))
    return devices


def find_device(device: str) -> int:
    devices = list_audio_devices()
    for index, name in devices:
        if DEVICE_NAMES[device] in name:
            return index
    listing = "\n".join(f"  [{i}] {n}" for i, n in devices) or "  (none)"
    sys.exit(f"no audio input matching {DEVICE_NAMES[device]!r}; available:\n{listing}")


def record(index: int, path: str) -> None:
    proc = subprocess.Popen(
        ["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "avfoundation", "-i", f":{index}",
         "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", path],
        stdin=subprocess.PIPE,
    )
    input("  ● recording — Enter to stop")
    proc.communicate(b"q")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--device", choices=sorted(DEVICE_NAMES), required=True)
    parser.add_argument("--only", nargs="*")
    args = parser.parse_args()

    index = find_device(args.device)
    out_dir = os.path.join(HERE, "data", args.device)
    os.makedirs(out_dir, exist_ok=True)
    phrases = [p for p in load_phrases(os.path.join(HERE, "phrases.tsv")) if not args.only or p.id in args.only]
    todo = [p for p in phrases if args.only or not os.path.exists(os.path.join(out_dir, f"{p.id}.wav"))]
    print(f"{len(todo)} phrases to record on {args.device}")
    i = 0
    while i < len(todo):
        p = todo[i]
        path = os.path.join(out_dir, f"{p.id}.wav")
        print(f"\n[{i + 1}/{len(todo)}] {p.id}\n\n    {p.text}\n")
        key = input("  Enter — record, s — skip, r — redo previous, q — quit: ").strip().lower()
        if key == "q":
            break
        if key == "s":
            i += 1
            continue
        if key == "r" and i > 0:
            i -= 1
            continue
        record(index, path)
        i += 1
    print("done")


if __name__ == "__main__":
    main()
