#!/usr/bin/env python3
import argparse
import collections
import glob
import json
import os
import time

ROOT = os.path.join(os.environ.get("CLAUDE_CONFIG_DIR") or os.path.expanduser("~/.claude"), "projects")
W = {"input_tokens": 1.0, "cache_creation_input_tokens": 2.0, "cache_read_input_tokens": 0.1, "output_tokens": 5.0}
CM = "mcp__clubmate__"


def weighted(u):
    return sum(u.get(k, 0) * w for k, w in W.items())


def text_len(c):
    if isinstance(c, str):
        return len(c)
    if isinstance(c, list):
        return sum(len(x.get("text", "")) for x in c if isinstance(x, dict))
    return 0


def is_cm(name, inp):
    return name.startswith(CM) or name == "ToolSearch" and "clubmate" in json.dumps(inp)


def is_cli(name, inp):
    if name != "Bash":
        return False
    cmd = inp.get("command", "")
    return "cm command run" in cmd or "clubmate command run" in cmd or "cm agent " in cmd


def session(path):
    steps, order, results = {}, [], {}
    prompt = False
    for line in open(path, errors="replace"):
        try:
            d = json.loads(line)
        except ValueError:
            continue
        if not isinstance(d, dict) or d.get("isSidechain"):
            continue
        t = d.get("type")
        if t == "assistant":
            m = d.get("message", {})
            mid = m.get("id")
            if not mid:
                continue
            if mid not in steps:
                steps[mid] = {"tools": [], "usage": {}, "prompt_before": prompt}
                order.append(mid)
                prompt = False
            if m.get("usage"):
                steps[mid]["usage"] = m["usage"]
            for c in m.get("content", []):
                if isinstance(c, dict) and c.get("type") == "tool_use":
                    steps[mid]["tools"].append((c.get("name", ""), c.get("input") or {}, c.get("id")))
        elif t == "user":
            c = d.get("message", {}).get("content")
            if isinstance(c, str):
                prompt = True
            elif isinstance(c, list):
                kinds = {x.get("type") for x in c if isinstance(x, dict)}
                if "tool_result" in kinds:
                    for x in c:
                        if isinstance(x, dict) and x.get("type") == "tool_result":
                            results[x.get("tool_use_id")] = text_len(x.get("content"))
                elif "text" in kinds:
                    prompt = True
    return [steps[i] for i in order], results


def sessions(days, project_glob):
    cutoff = time.time() - days * 86400
    files = []
    for d in glob.glob(os.path.join(ROOT, project_glob)):
        if os.path.basename(d).startswith("-private-tmp"):
            continue
        files += [f for f in glob.glob(os.path.join(d, "*.jsonl")) if os.path.getmtime(f) >= cutoff]
    return files


def main():
    ap = argparse.ArgumentParser(description="Clubmate MCP usage in Claude logs: collapsible runs, carried results, reply sizes.")
    ap.add_argument("days", nargs="?", type=int, default=7)
    ap.add_argument("--project-glob", default="*ggfarmco-clubmate*")
    a = ap.parse_args()

    files = sessions(a.days, a.project_glob)
    tot = collections.Counter()
    by_role = collections.defaultdict(collections.Counter)
    bigrams = collections.Counter()
    run_len = collections.Counter()
    res_by_tool = collections.defaultdict(lambda: [0, 0])
    res_by_args = collections.defaultdict(lambda: [0, 0])
    for f in files:
        steps, results = session(f)
        if not steps:
            continue
        orch = any(n == CM + "agents_new" for s in steps for n, _, _ in s["tools"])
        role = "orchestrator" if orch else "other"
        costs = [weighted(s["usage"]) for s in steps]
        sess_cost = sum(costs)
        tot["sessions"] += 1
        tot["steps"] += len(steps)
        tot["cost"] += sess_cost
        by_role[role]["sessions"] += 1
        by_role[role]["cost"] += sess_cost
        cm_steps = [bool(s["tools"]) and all(is_cm(n, i) for n, i, _ in s["tools"]) for s in steps]

        def close(run):
            if len(run) < 2:
                return
            saved = sum(costs[j] for j in run[1:])
            tot["runs"] += 1
            tot["saved_steps"] += len(run) - 1
            tot["saved_cost"] += saved
            by_role[role]["saved_cost"] += saved
            by_role[role]["saved_steps"] += len(run) - 1
            run_len[min(len(run), 8)] += 1
            seq = [n.replace(CM, "") for j in run for n, _, _ in steps[j]["tools"]]
            for x, y in zip(seq, seq[1:]):
                bigrams[(x, y)] += 1

        run = []
        for i, s in enumerate(steps):
            if cm_steps[i] and not (s["prompt_before"] and run):
                run.append(i)
            else:
                close(run)
                run = [i] if cm_steps[i] else []
            for n, inp, tid in s["tools"]:
                if n.startswith(CM):
                    tool = n.replace(CM, "")
                    chars = results.get(tid, 0)
                    carried = chars / 4 * (len(steps) - i - 1) * W["cache_read_input_tokens"]
                    tot["cm_calls"] += 1
                    tot["cm_result_carried"] += carried
                    by_role[role]["cm_calls"] += 1
                    by_role[role]["cm_result_carried"] += carried
                    for key, table in ((tool, res_by_tool), ((tool, ",".join(sorted(k for k in inp if k != "workspace"))), res_by_args)):
                        table[key][0] += 1
                        table[key][1] += chars
                if is_cli(n, inp):
                    tot["cli_bash_calls"] += 1
            if cm_steps[i]:
                tot["cm_only_steps"] += 1
        close(run)

    def pct(x, y):
        return f"{100 * x / y:.1f}%" if y else "-"

    print(f"window {a.days}d, files {len(files)}, sessions {tot['sessions']}, steps {tot['steps']}")
    print(f"clubmate MCP calls {tot['cm_calls']}, clubmate-only steps {tot['cm_only_steps']} ({pct(tot['cm_only_steps'], tot['steps'])} of steps)")
    print(f"Bash calls already using cm/clubmate CLI: {tot['cli_bash_calls']}")
    print(f"collapsible runs (>=2 consecutive clubmate-only steps): {tot['runs']}, steps saved {tot['saved_steps']}")
    print(f"saved weighted cost: {pct(tot['saved_cost'], tot['cost'])} of all")
    print(f"clubmate results carried in context afterwards (cache-read): {pct(tot['cm_result_carried'], tot['cost'])} of all")
    for role, c in sorted(by_role.items()):
        print(f"  {role}: cost share {pct(c['cost'], tot['cost'])}, sessions {c['sessions']}, cm calls {c['cm_calls']}, saved steps {c['saved_steps']}, saved {pct(c['saved_cost'], c['cost'])}, results carried {pct(c['cm_result_carried'], c['cost'])}")
    print("run length histogram:", dict(sorted(run_len.items())))
    print("top bigrams in runs:")
    for (x, y), n in bigrams.most_common(15):
        print(f"  {n:5d} {x} -> {y}")
    print("top tools by total reply size (calls, avg chars):")
    for name, (n, chars) in sorted(res_by_tool.items(), key=lambda x: -x[1][1])[:12]:
        print(f"  {name}: {n} calls, avg {chars // max(n, 1)} chars, total {chars // 1000}k")
    print("reply size by tool and argument set:")
    for (name, args), (n, chars) in sorted(res_by_args.items(), key=lambda x: -x[1][1])[:25]:
        print(f"  {name:20s} {args:40s} {n:5d} calls avg {chars // max(n, 1):6d}")


if __name__ == "__main__":
    main()
