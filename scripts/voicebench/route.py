# /// script
# requires-python = ">=3.11"
# ///
import argparse
import json
import math
import os
import re
import statistics
import subprocess
import sys
import time

CONTEXT = {
    "device": "phone",
    "workspace": {"id": "ws_clubmate", "name": "clubmate"},
    "section": "agents",
    "selection": {
        "session": {
            "id": "agt_7c1e",
            "title": "Синхронизация бэклога с телефоном",
            "status": "waiting",
            "pendingQuestion": {
                "text": "Как телефон получает задачи бэклога?",
                "options": [
                    {"id": 1, "label": "Только онлайн, запрос к core при открытии"},
                    {"id": 2, "label": "Локальный кэш с подпиской на изменения"},
                ],
            },
            "pendingPermission": None,
        }
    },
}

UTTERANCE = "второй вариант, и добавь ещё офлайн режим"

ROUTE_PROMPT = f"""You are the Clubmate Voice assistant. The user spoke on a device; route the utterance.

Resolved context:
{json.dumps(CONTEXT, ensure_ascii=False, indent=2)}

Utterance: «{UTTERANCE}»

If the utterance answers the selected session's pending question, reply with only this JSON object and nothing else:
{{"action": "answer", "session": "<session id>", "option": <option id>, "text": "<the user's extra words, cleaned>"}}"""


def clean_env() -> dict[str, str]:
    return {k: v for k, v in os.environ.items() if not k.startswith("CLUBMATE_")}


def run_claude(model: str, prompt: str) -> tuple[float, float, str]:
    cmd = ["claude", "-p", "--model", model, "--output-format", "json", prompt]
    start = time.monotonic()
    proc = subprocess.run(cmd, capture_output=True, text=True, env=clean_env(), stdin=subprocess.DEVNULL)
    elapsed = time.monotonic() - start
    if proc.returncode != 0:
        raise RuntimeError(f"claude exited {proc.returncode}: {proc.stderr.strip()[:300]}")
    out = json.loads(proc.stdout)
    return elapsed, out.get("duration_api_ms", 0) / 1000, out["result"]


def parse_decision(reply: str) -> dict:
    match = re.search(r"\{.*\}", reply, re.S)
    if not match:
        raise ValueError(f"no JSON in reply: {reply[:200]!r}")
    return json.loads(match.group(0))


def p90(xs: list[float]) -> float:
    s = sorted(xs)
    return s[math.ceil(0.9 * len(s)) - 1]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="sonnet")
    ap.add_argument("--runs", type=int, default=5)
    args = ap.parse_args()

    route, api, base, failures = [], [], [], 0
    for i in range(1, args.runs + 1):
        elapsed, api_s, reply = run_claude(args.model, ROUTE_PROMPT)
        route.append(elapsed)
        api.append(api_s)
        try:
            decision = parse_decision(reply)
            ok = decision.get("action") == "answer" and decision.get("option") == 2
        except ValueError as e:
            decision, ok = str(e), False
        failures += not ok
        print(f"route {i}: {elapsed:.2f}s api {api_s:.2f}s {'ok' if ok else 'FAIL'} {json.dumps(decision, ensure_ascii=False)}")

        elapsed, _, _ = run_claude(args.model, "ok")
        base.append(elapsed)
        print(f"base  {i}: {elapsed:.2f}s")

    rm, bm = statistics.median(route), statistics.median(base)
    rp, bp = p90(route), p90(base)
    print(f"\nmodel {args.model}, {args.runs} runs")
    print(f"route    median {rm:.2f}s  p90 {rp:.2f}s")
    print(f"route api median {statistics.median(api):.2f}s  p90 {p90(api):.2f}s")
    print(f"baseline median {bm:.2f}s  p90 {bp:.2f}s")
    print(f"diff     median {rm - bm:.2f}s  p90 {rp - bp:.2f}s")
    print(f"decisions ok {args.runs - failures}/{args.runs}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
