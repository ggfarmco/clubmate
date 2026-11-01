#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# ///
import argparse
import json
import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from score import Phrase, load_phrases, score_rows, summary_table


HERE = os.path.dirname(os.path.abspath(__file__))
DEVICES = ["mac", "iphone"]
FIXED_HINT = "MCP worktree merge squash commit backlog CI PR Clubmate Companion"
DEFAULT_BACKENDS = [
    ("whisper", "mlx-community/whisper-large-v3-turbo"),
    ("gigaam", "v3_e2e_rnnt"),
    ("qwen3", "Qwen/Qwen3-ASR-0.6B"),
    ("qwen3", "Qwen/Qwen3-ASR-1.7B"),
    ("parakeet", "mlx-community/parakeet-tdt-0.6b-v3"),
    ("nemotron", "nvidia/nemotron-3.5-asr-streaming-0.6b"),
]
LANG_BACKENDS = {"whisper", "qwen3", "nemotron"}
HINT_BACKENDS = {"whisper", "qwen3"}


def _adapter_cmd(backend: str, model: str, lang: str, hint: str | None, wavs: list[str]) -> list[str]:
    cmd = ["uv", "run", "--script", os.path.join(HERE, "backends", f"{backend}.py"), "--model", model, "--lang", lang]
    if hint:
        cmd += ["--hint", hint]
    return cmd + wavs


def run(
    backends: list[tuple[str, str]],
    phrases: list[Phrase],
    data_dir: str,
    hint: str | None,
    lang: str = "ru",
    runner=subprocess.run,
) -> tuple[list[dict], list[str]]:
    rows, missing = [], []
    present: dict[str, list[tuple[str, str]]] = {}
    for device in DEVICES:
        present[device] = []
        for p in phrases:
            path = os.path.join(data_dir, device, f"{p.id}.wav")
            if os.path.exists(path):
                present[device].append((p.id, path))
            else:
                missing.append(f"{device}/{p.id}")
    for backend, model in backends:
        for device in DEVICES:
            items = present[device]
            if not items:
                continue
            base = {"backend": backend, "model": model, "lang": lang, "device": device, "hint": hint or ""}
            proc = runner(_adapter_cmd(backend, model, lang, hint, [path for _, path in items]), capture_output=True, text=True)
            by_path = {}
            for line in proc.stdout.splitlines():
                try:
                    out = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(out, dict) and "audio" in out:
                    by_path[out["audio"]] = out
            failure = (proc.stderr or "").strip()[-300:] or f"exit {proc.returncode}"
            for pid, path in items:
                out = by_path.get(path)
                row = dict(base, id=pid)
                if out is None:
                    row["error"] = failure if proc.returncode else "no output"
                elif "error" in out:
                    row["error"] = out["error"]
                else:
                    row["text"] = out.get("text", "")
                    row["ms"] = int(out.get("ms", 0))
                rows.append(row)
    return rows, missing


def _parse_backend(s: str) -> tuple[str, str]:
    name, _, model = s.partition(":")
    return name, model


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--backend", nargs="*", type=_parse_backend)
    parser.add_argument("--hint", choices=["auto", "none", "both"], default="both")
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    phrases = load_phrases(os.path.join(HERE, "phrases.tsv"))
    backends = args.backend or DEFAULT_BACKENDS
    auto = " ".join(dict.fromkeys([t.text for p in phrases for t in p.terms] + FIXED_HINT.split()))
    hints = {"auto": [auto], "none": [None], "both": [None, auto]}[args.hint]
    data_dir = os.path.join(HERE, "data")

    rows, missing = [], []
    for backend, model in backends:
        langs = ["ru", "auto"] if backend in LANG_BACKENDS else ["ru"]
        for lang in langs:
            for hint in hints if backend in HINT_BACKENDS else [None]:
                print(f"{backend} {model} lang={lang} hint={'yes' if hint else 'no'}", file=sys.stderr)
                got, missing = run([(backend, model)], phrases, data_dir, hint, lang)
                if backend not in LANG_BACKENDS:
                    for r in got:
                        r["lang"] = "—"
                rows += got

    scored = score_rows(phrases, rows)
    by_id = {p.id: p for p in phrases}
    worst = sorted((r for r in scored if "error" not in r), key=lambda r: (r["terms_alias"], -r["wer"]))[:10]
    errors = [r for r in scored if "error" in r]
    lines = ["# voicebench results", "", summary_table(scored), "## Worst 10 by term accuracy", ""]
    for r in worst:
        lines.append(f"- {r['backend']} {r['model']} {r['lang']} {r['device']} hint={'yes' if r['hint'] else 'no'} {r['id']}: «{by_id[r['id']].text}» → «{r['text']}»")
    if errors:
        lines += ["", "## Errors", ""] + [f"- {r['backend']} {r['model']} {r['device']} {r['id']}: {r['error']}" for r in errors[:30]]
    if missing:
        lines += ["", "## Missing recordings", "", ", ".join(missing)]
    os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
    with open(args.out, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")
    print(args.out)


if __name__ == "__main__":
    main()
