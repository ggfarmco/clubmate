#!/usr/bin/env python3
import argparse
import csv
import glob
import json
import os
import re
import shutil
import statistics
import subprocess
import sys
from datetime import datetime, timezone

HOME = os.path.expanduser("~")
OUT = os.path.join(HOME, ".clubmate", "outcomes")
AGENTS = os.path.join(HOME, "Library", "Application Support", "clubmate", "plugin-data", "agents")
CLAUDE = os.environ.get("CLAUDE_CONFIG_DIR") or os.path.join(HOME, ".claude")
REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CACHE_VERSION = 2

PRICES = {
    "claude-fable-5-2": (8, 40, 0.2),
    "claude-mythos-5-2": (8, 40, 0.2),
    "claude-fable-5-1": (10, 50, 1.0),
    "claude-fable-5": (10, 50, 1.0),
    "claude-opus-5-5": (4, 20, 0.2),
    "claude-opus-5": (5, 25, 0.5),
    "claude-opus-4-8": (5, 25, 0.5),
    "claude-opus-4-7": (5, 25, 0.5),
    "claude-opus-4-6": (5, 25, 0.5),
    "claude-sonnet-5-5": (2, 10, 0.1),
    "claude-sonnet-5": (2, 10, 0.2),
    "claude-sonnet-4-6": (3, 15, 0.3),
    "claude-haiku-5-5": (0.1, 0.5, 0.01),
    "claude-haiku-4-5": (1, 5, 0.1),
}
LONG_PROMPT = {"claude-haiku-5-5": (100_000, (0.5, 2.5, 0.05))}
FREE_MODELS = {"<synthetic>", None, ""}

EXP_RE = re.compile(r"^\[exp ([^\]\n]*)\]", re.M)
EMAIL_RE = re.compile(r"[\w.+-]+@[\w-]+\.[\w.-]+")
TOOL_CALL_RE = re.compile(r'"sessionUpdate":"tool_call"[,}]')

COLUMNS = [
    "session", "title", "parent", "kind", "size", "arm", "model", "effort",
    "created", "finished", "wall_min", "calls", "cost", "advisor_cost", "cost_source",
    "branch", "part_of", "parts", "reviewers", "gate_rounds", "changes", "squash", "cost_to_landing",
]
SUBTREE_COLUMNS = [
    "root", "title", "created", "children", "builders", "landed",
    "root_cost", "children_cost", "reviewers_cost", "subtree_cost", "cost_per_landing",
    "busy_min", "union_min", "parallel",
]


def cm(*args):
    exe = shutil.which("cm") or shutil.which("clubmate") or "/usr/local/bin/cm"
    out = subprocess.run([exe, "command", "run", *args, "--json"], capture_output=True, text=True, check=True)
    return json.loads(out.stdout)


def parse_exp(text):
    m = EXP_RE.search(text or "")
    if not m:
        return {}
    return dict(kv.split("=", 1) for kv in m.group(1).split() if "=" in kv)


def token_cost(usage, model):
    if model in FREE_MODELS:
        return 0.0
    price = PRICES.get(model)
    if price is None:
        return None
    cc = usage.get("cache_creation") or {}
    w5 = cc.get("ephemeral_5m_input_tokens", 0) if cc else usage.get("cache_creation_input_tokens", 0)
    w1 = cc.get("ephemeral_1h_input_tokens", 0)
    prompt = usage.get("input_tokens", 0) + w5 + w1 + usage.get("cache_read_input_tokens", 0)
    limit, long_price = LONG_PROMPT.get(model, (None, None))
    inp, outp, read = long_price if limit and prompt > limit else price
    return (
        usage.get("input_tokens", 0) * inp
        + w5 * inp * 1.25
        + w1 * inp * 2
        + usage.get("cache_read_input_tokens", 0) * read
        + usage.get("output_tokens", 0) * outp
    ) / 1e6


def reported_cost(amounts):
    if not amounts:
        return None
    total = 0.0
    for a, b in zip(amounts, amounts[1:]):
        if b < a:
            total += a
    return total + amounts[-1]


def read_clubmate_log(path):
    first_prompt = None
    calls = set()
    amounts = []
    finished = None
    model = effort = None
    with open(path, encoding="utf-8", errors="replace") as f:
        for line in f:
            if first_prompt is None and '"kind":"session/prompt"' in line:
                r = json.loads(line)
                first_prompt = "\n".join(b.get("text", "") for b in r["payload"].get("prompt", []) if b.get("type") == "text")
            elif TOOL_CALL_RE.search(line):
                calls.add(json.loads(line)["payload"].get("toolCallId"))
            elif '"cost"' in line and "usage_update" in line:
                a = ((json.loads(line)["payload"].get("cost")) or {}).get("amount")
                if a is not None:
                    amounts.append(a)
            elif '"kind":"turn_end"' in line:
                finished = json.loads(line)["ts"]
            elif '"kind":"session/set_config_option"' in line:
                p = json.loads(line)["payload"]
                if p.get("configId") == "model":
                    model = p.get("value")
                elif p.get("configId") == "effort":
                    effort = p.get("value")
    return {
        "exp": parse_exp(first_prompt),
        "calls": len(calls),
        "reported": reported_cost(amounts),
        "finished": finished,
        "set_model": model,
        "set_effort": effort,
    }


def read_claude_logs(paths):
    messages = {}
    for p in paths:
        with open(p, encoding="utf-8", errors="replace") as f:
            for line in f:
                if '"type":"assistant"' not in line or '"usage"' not in line:
                    continue
                r = json.loads(line)
                if r.get("type") != "assistant":
                    continue
                m = r.get("message") or {}
                messages[(p, m.get("id") or r.get("uuid"))] = m
    total = advisor = 0.0
    by_model = {}
    unpriced = False
    for m in messages.values():
        usage = m.get("usage") or {}
        for it in usage.get("iterations") or [dict(usage, type="message")]:
            model = it.get("model") or m.get("model")
            c = token_cost(it, model)
            if c is None:
                unpriced = True
                continue
            total += c
            if it.get("type") == "advisor_message":
                advisor += c
            elif model not in FREE_MODELS:
                by_model[model] = by_model.get(model, 0) + c
    if not messages:
        return None
    return {
        "cost": None if unpriced else total,
        "advisor": advisor,
        "model": max(by_model, key=by_model.get) if by_model else None,
    }


def claude_index():
    idx = {}
    for p in glob.glob(os.path.join(CLAUDE, "projects", "*", "*.jsonl")):
        idx.setdefault(os.path.basename(p)[:-6], []).append(p)
    for p in glob.glob(os.path.join(CLAUDE, "projects", "*", "*", "subagents", "*.jsonl")):
        idx.setdefault(os.path.basename(os.path.dirname(os.path.dirname(p))), []).append(p)
    return idx


def stamp(paths):
    out = []
    for p in sorted(paths):
        try:
            st = os.stat(p)
        except FileNotFoundError:
            continue
        out.append([p, st.st_size, int(st.st_mtime)])
    return out


def lost_files(old, new):
    have = {p for stamps in new for p, _, _ in stamps}
    return any(p not in have for stamps in old for p, _, _ in stamps)


def session_metrics(sessions, cache, idx):
    out = {}
    for s in sessions:
        sid = s["id"]
        clog = os.path.join(AGENTS, "sessions", sid + ".jsonl")
        key = [stamp([clog]), stamp(idx.get(s.get("agentSessionId") or "", []))]
        hit = cache.get(sid)
        unpriced = bool(hit) and hit["m"].get("claude") is not None and hit["m"]["claude"].get("cost") is None
        if hit and ((hit["key"] == key and not unpriced) or lost_files(hit["key"], key)):
            out[sid] = hit["m"]
            continue
        m = read_clubmate_log(clog) if os.path.exists(clog) else {"exp": {}, "calls": 0, "reported": None, "finished": None, "set_model": None, "set_effort": None}
        cl = read_claude_logs([p for p, _, _ in key[1]])
        m["claude"] = cl
        cache[sid] = {"key": key, "m": m}
        out[sid] = m
    return out


def branch_of(s):
    if s.get("closedBranch"):
        return s["closedBranch"]
    wt = s.get("worktree") or ""
    if "/.worktrees/" in wt:
        return wt.split("/.worktrees/", 1)[1]
    return ""


def norm(branch):
    return branch.replace("/", "-")


def parse_ts(ts):
    base = datetime.strptime(ts[:19], "%Y-%m-%dT%H:%M:%S")
    frac = re.match(r"\.(\d+)", ts[19:])
    return base.replace(tzinfo=timezone.utc).timestamp() + (float("0." + frac.group(1)) if frac else 0)


def minutes(a, b):
    if not a or not b:
        return ""
    return round((parse_ts(b) - parse_ts(a)) / 60, 1)


def money(x):
    return "" if x is None else round(x, 2)


def cost_of(m):
    cl = m.get("claude") or {}
    if cl.get("cost") is not None:
        return cl["cost"], "tokens"
    return m.get("reported"), "reported" if m.get("reported") is not None else ""


def own_branch(s, by_id):
    parent = by_id.get(s.get("parentSession") or "")
    if parent and parent.get("worktree") == s.get("worktree"):
        return ""
    return branch_of(s)


def build_rows(sessions, metrics, verdicts, trees):
    by_id = {s["id"]: s for s in sessions}
    reviewers = {}
    for s in sessions:
        gf = s.get("gateFor")
        if gf and gf.get("source"):
            reviewers.setdefault(norm(gf["source"]), []).append(s)
    children = sorted(
        (s for s in sessions if s.get("parentSession") and not s.get("gateFor")),
        key=lambda s: s.get("createdAt", ""),
    )
    builder_of_branch = {}
    fix_of = {}
    for s in children:
        b = norm(own_branch(s, by_id))
        if not b:
            continue
        if b in builder_of_branch:
            fix_of[s["id"]] = builder_of_branch[b]
        else:
            builder_of_branch[b] = s["id"]
    for s in children:
        if s["id"] in fix_of or own_branch(s, by_id):
            continue
        p = by_id.get(s.get("parentSession") or "")
        while p and p.get("parentSession"):
            owner = builder_of_branch.get(norm(own_branch(p, by_id)))
            if owner:
                fix_of[s["id"]] = owner
                break
            p = by_id.get(p["parentSession"])
    for s in children:
        if s["id"] in fix_of or (s.get("exp") or metrics.get(s["id"], {}).get("exp") or {}).get("kind") != "fix":
            continue
        own = norm(own_branch(s, by_id))
        text = s.get("title", "")
        for b, bid in builder_of_branch.items():
            if b != own and bid != s["id"] and by_id[bid]["createdAt"] < s["createdAt"] and b in norm(text):
                fix_of[s["id"]] = bid
                break
    verdicts_by_branch = {}
    for v in verdicts:
        if v.get("by") == "reviewer" and v.get("source"):
            verdicts_by_branch.setdefault(norm(v["source"]), []).append(v)

    rows = []
    for s in children:
        sid = s["id"]
        m = metrics.get(sid, {})
        exp = s.get("exp") or m.get("exp") or {}
        cl = m.get("claude") or {}
        cost, source = cost_of(m)
        branch = own_branch(s, by_id)
        b = norm(branch)
        fixes = [c for c, owner in fix_of.items() if owner == sid]
        revs = reviewers.get(b, []) if b and sid not in fix_of else []
        vs = verdicts_by_branch.get(b, []) if b and sid not in fix_of else []
        decided = {(v.get("reviewer"), v.get("round") or v.get("outcome")): v.get("outcome") for v in vs}
        rounds = max(len(decided), len(revs))
        squash = ""
        for v in vs:
            if v.get("outcome") == "approved" and v.get("commit"):
                squash = v["commit"]
        if not squash:
            for r in revs:
                squash = trees.get(r["gateFor"].get("tree"), "") or squash
        to_land = None
        if sid not in fix_of and cost is not None:
            parts = [cost] + [cost_of(metrics.get(x, {}))[0] for x in fixes] + [cost_of(metrics.get(r["id"], {}))[0] for r in revs]
            to_land = sum(p for p in parts if p is not None)
        rows.append({
            "session": sid,
            "title": EMAIL_RE.sub("<email>", s.get("title", "")),
            "parent": s.get("parentSession", ""),
            "kind": exp.get("kind", ""),
            "size": exp.get("size", ""),
            "arm": exp.get("arm", ""),
            "model": cl.get("model") or s.get("startModel") or m.get("set_model") or "",
            "effort": s.get("startEffort") or m.get("set_effort") or "default",
            "created": s.get("createdAt", ""),
            "finished": m.get("finished") or "",
            "wall_min": minutes(s.get("createdAt"), m.get("finished")),
            "calls": m.get("calls", 0),
            "cost": money(cost),
            "advisor_cost": money(cl.get("advisor")),
            "cost_source": source,
            "branch": branch,
            "part_of": fix_of.get(sid, ""),
            "parts": " ".join(fixes),
            "reviewers": " ".join(r["id"] for r in revs),
            "gate_rounds": rounds,
            "changes": sum(1 for o in decided.values() if o == "changes"),
            "squash": squash,
            "cost_to_landing": money(to_land),
        })
    return rows


def root_of(sid, by_id):
    seen = set()
    s = by_id.get(sid)
    while s and s.get("parentSession") in by_id and s["id"] not in seen:
        seen.add(s["id"])
        s = by_id[s["parentSession"]]
    return s["id"] if s else sid


def union_seconds(intervals):
    total = 0.0
    end = None
    for a, b in sorted(intervals):
        if end is None or a > end:
            total += b - a
            end = b
        elif b > end:
            total += b - end
            end = b
    return total


def build_subtrees(sessions, metrics, rows, trees):
    by_id = {s["id"]: s for s in sessions}
    subs = {}
    for s in sessions:
        gf = s.get("gateFor")
        if gf:
            if gf.get("lander"):
                subs.setdefault(root_of(gf["lander"], by_id), {"members": [], "reviewers": []})["reviewers"].append(s)
        elif s.get("parentSession"):
            subs.setdefault(root_of(s["id"], by_id), {"members": [], "reviewers": []})["members"].append(s["id"])
    builders = {}
    intervals = {}
    for r in rows:
        if r["branch"] and not r["part_of"]:
            root = root_of(r["session"], by_id)
            builders[root] = builders.get(root, 0) + 1
            if r["created"] and r["finished"]:
                intervals.setdefault(root, []).append((parse_ts(r["created"]), parse_ts(r["finished"])))

    def total(ids):
        return sum(c for c in (cost_of(metrics.get(i, {}))[0] for i in ids) if c is not None)

    out = []
    for root, t in subs.items():
        s = by_id.get(root)
        if not s or s.get("gateFor"):
            continue
        landed = len({trees[g["gateFor"]["tree"]] for g in t["reviewers"] if g["gateFor"].get("tree") in trees})
        if not t["members"] and not landed:
            continue
        root_cost = cost_of(metrics.get(root, {}))[0]
        children_cost = total(t["members"])
        reviewers_cost = total(g["id"] for g in t["reviewers"])
        subtree = (root_cost or 0) + children_cost + reviewers_cost
        iv = intervals.get(root, [])
        busy = sum(b - a for a, b in iv)
        union = union_seconds(iv)
        out.append({
            "root": root,
            "title": EMAIL_RE.sub("<email>", s.get("title", "")),
            "created": s.get("createdAt", ""),
            "children": len(t["members"]),
            "builders": builders.get(root, 0),
            "landed": landed,
            "root_cost": money(root_cost),
            "children_cost": money(children_cost),
            "reviewers_cost": money(reviewers_cost),
            "subtree_cost": money(subtree),
            "cost_per_landing": money(subtree / landed) if landed else "",
            "busy_min": round(busy / 60, 1),
            "union_min": round(union / 60, 1),
            "parallel": round(busy / union, 2) if union > 0 else "",
        })
    return sorted(out, key=lambda r: r["created"])


def collect_verdicts(path):
    seen = set()
    if os.path.exists(path):
        with open(path) as f:
            for line in f:
                v = json.loads(line)
                seen.add((v.get("gate"), v.get("at"), v.get("reviewer")))
    added = 0
    with open(path, "a") as f:
        for gate in cm("agents.gates").get("gates", []):
            res = cm("agents.gate-verdicts", "id=" + gate["id"], "limit=100")
            for v in sorted(res.get("verdicts", []), key=lambda v: v.get("at", "")):
                k = (v.get("gate"), v.get("at"), v.get("reviewer"))
                if k in seen:
                    continue
                seen.add(k)
                f.write(json.dumps(v, ensure_ascii=False) + "\n")
                added += 1
    return added


def load_verdicts(path, sessions):
    out = []
    keys = set()
    if os.path.exists(path):
        with open(path) as f:
            for line in f:
                v = json.loads(line)
                out.append(v)
                keys.add((v.get("reviewer"), v.get("outcome")))
    source_of = {s["id"]: s["gateFor"].get("source") for s in sessions if s.get("gateFor")}
    for p in glob.glob(os.path.join(AGENTS, "land-verdicts", "*.json")):
        with open(p) as f:
            v = json.load(f)
        src = source_of.get(v.get("reviewer"))
        if not src:
            continue
        if (v.get("reviewer"), v.get("outcome")) in keys:
            for o in out:
                if o.get("reviewer") == v.get("reviewer") and o.get("outcome") == v.get("outcome") and v.get("commit"):
                    o.setdefault("commit", v["commit"])
            continue
        keys.add((v.get("reviewer"), v.get("outcome")))
        out.append(dict(v, source=src))
    return out


def main_trees():
    res = subprocess.run(["git", "-C", REPO, "log", "main", "--format=%H %T", "-n", "5000"], capture_output=True, text=True)
    trees = {}
    for line in res.stdout.splitlines():
        h, t = line.split()
        trees.setdefault(t, h)
    return trees


def median(xs):
    xs = [x for x in xs if x not in ("", None)]
    return round(statistics.median(xs), 2) if xs else ""


def subtree_lines(subtrees):
    buckets = [("0", 0, 0), ("1", 1, 1), ("2–3", 2, 3), ("4+", 4, 10**9)]
    lines = [
        "",
        "## Subtrees",
        "",
        "A subtree is a top-level session, all its descendants and the reviewers of their landings. "
        "This table counts only subtrees that landed into main through the land gate; subtrees.csv also lists those with children and no landing. "
        "builders = children with a branch of their own; parallel = summed builder wall time / time at least one builder ran (1 = one at a time), from the builders that ended a turn. "
        "Costs are pooled: the sum over the bucket divided by its landings. Not randomised: a bigger task gets more builders.",
        "",
        "| builders | roots | landings | $ per landing | root share | reviewers share | parallel |",
        "|---|---|---|---|---|---|---|",
    ]
    for name, lo, hi in buckets:
        rs = [r for r in subtrees if r["landed"] and lo <= r["builders"] <= hi]
        if not rs:
            continue
        landed = sum(r["landed"] for r in rs)
        cost = sum(r["subtree_cost"] for r in rs)
        share = lambda col: f"{round(100 * sum(r[col] or 0 for r in rs) / cost)}%" if cost else ""
        lines.append(
            f"| {name} | {len(rs)} | {landed} | {round(cost / landed, 2)} | {share('root_cost')} | "
            f"{share('reviewers_cost')} | {median([r['parallel'] for r in rs])} |"
        )
    return lines


def write_report(path, rows, verdicts, subtrees):
    groups = {}
    for r in rows:
        if r["part_of"] or not r["branch"]:
            continue
        arm = r["arm"] or "(" + r["model"].replace("claude-", "") + "/" + r["effort"] + ")"
        groups.setdefault((r["kind"] or "-", r["size"] or "-", arm), []).append(r)
    lines = [
        "# Child outcomes",
        "",
        f"Generated {datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M UTC')}. "
        "Builders only: children with a branch of their own; later sessions on that branch and branchless sessions they started roll into them (part_of). "
        f"{sum(1 for r in rows if not r['branch'] and not r['part_of'])} children without a branch (they worked in their caller's worktree) are left out. "
        "An arm in parentheses is the model an untagged child ran on. "
        "`landed` counts only landings through the land gate (since 16 Nov) into this repo's main; a landing into an integration branch or before the gate is not counted.",
        "",
        "| kind | size | arm | n | landed | calls | wall min | cost $ | cost to landing $ (landed) | rounds | changes |",
        "|---|---|---|---|---|---|---|---|---|---|---|",
    ]
    for (k, sz, arm), rs in sorted(groups.items(), key=lambda kv: (-len(kv[1]), kv[0])):
        landed = [r for r in rs if r["squash"]]
        lines.append(
            f"| {k} | {sz} | {arm} | {len(rs)} | {len(landed)} | {median([r['calls'] for r in rs])} | "
            f"{median([r['wall_min'] for r in rs])} | {median([r['cost'] for r in rs])} | "
            f"{median([r['cost_to_landing'] for r in landed])} | {median([r['gate_rounds'] for r in landed])} | "
            f"{median([r['changes'] for r in landed])} |"
        )
    lines += subtree_lines(subtrees)
    ats = sorted(v.get("at", "") for v in verdicts if v.get("at"))
    if ats:
        lines += ["", f"Verdicts kept: {len(verdicts)}, {ats[0][:16]} – {ats[-1][:16]}."]
    with open(path, "w") as f:
        f.write("\n".join(lines) + "\n")


def run(out_dir, skip_fetch):
    os.makedirs(out_dir, exist_ok=True)
    vpath = os.path.join(out_dir, "verdicts.jsonl")
    added = 0 if skip_fetch else collect_verdicts(vpath)
    sessions = cm("agents.sessions")
    cache_path = os.path.join(out_dir, "cache.json")
    cache = {}
    if os.path.exists(cache_path):
        with open(cache_path) as f:
            c = json.load(f)
        if c.get("version") == CACHE_VERSION:
            cache = c["sessions"]
    known = {s["id"] for s in sessions}
    sessions += [c["session"] for sid, c in cache.items() if sid not in known and "session" in c]
    metrics = session_metrics(sessions, cache, claude_index())
    for s in sessions:
        cache[s["id"]]["session"] = {k: v for k, v in s.items() if k in (
            "id", "title", "parentSession", "createdAt", "closedBranch", "worktree", "agentSessionId",
            "gateFor", "gateRound", "startModel", "startEffort", "exp")}
    tmp = cache_path + ".tmp"
    with open(tmp, "w") as f:
        json.dump({"version": CACHE_VERSION, "sessions": cache}, f)
    os.replace(tmp, cache_path)
    verdicts = load_verdicts(vpath, sessions)
    trees = main_trees()
    rows = build_rows(sessions, metrics, verdicts, trees)
    subtrees = build_subtrees(sessions, metrics, rows, trees)
    for name, cols, data in (("children.csv", COLUMNS, rows), ("subtrees.csv", SUBTREE_COLUMNS, subtrees)):
        with open(os.path.join(out_dir, name), "w", newline="") as f:
            w = csv.DictWriter(f, fieldnames=cols)
            w.writeheader()
            w.writerows(data)
    write_report(os.path.join(out_dir, "report.md"), rows, verdicts, subtrees)
    print(f"verdicts +{added}, children {len(rows)}, subtrees {len(subtrees)}, out {out_dir}")


def main():
    ap = argparse.ArgumentParser(description="Collect child-session outcomes for model/effort experiments.")
    ap.add_argument("--out", default=OUT)
    ap.add_argument("--skip-fetch", action="store_true", help="don't fetch gate verdicts")
    args = ap.parse_args()
    run(args.out, args.skip_fetch)


if __name__ == "__main__":
    sys.exit(main())
