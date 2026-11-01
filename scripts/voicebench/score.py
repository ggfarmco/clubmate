import re
import statistics
from dataclasses import dataclass


_SEPARATORS = re.compile(r"[/\-_.,!?:;«»\"'()]+")


@dataclass
class Term:
    text: str
    aliases: list[str]


@dataclass
class Phrase:
    id: str
    kind: str
    text: str
    terms: list[Term]


def _parse_terms(field: str) -> list[Term]:
    terms = []
    for raw in filter(None, field.split("|")):
        text, _, aliases = raw.partition("=")
        terms.append(Term(text.strip(), [a.strip() for a in aliases.split(",") if a.strip()]))
    return terms


def load_phrases(path: str) -> list[Phrase]:
    phrases = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.rstrip("\n")
            if not line.strip():
                continue
            cols = line.split("\t") + [""] * 3
            phrases.append(Phrase(cols[0], cols[1], cols[2], _parse_terms(cols[3])))
    return phrases


def normalize(s: str) -> list[str]:
    return _SEPARATORS.sub(" ", s.lower().replace("ё", "е")).split()


def _distance(a: list[str], b: list[str]) -> int:
    prev = list(range(len(b) + 1))
    for i, x in enumerate(a, 1):
        cur = [i]
        for j, y in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[j - 1] + 1, prev[j - 1] + (x != y)))
        prev = cur
    return prev[-1]


def wer(ref: str, hyp: str) -> float:
    r = normalize(ref)
    if not r:
        return 0.0
    return _distance(r, normalize(hyp)) / len(r)


def _contains(hay: list[str], needle: list[str]) -> bool:
    n = len(needle)
    return n > 0 and any(hay[i:i + n] == needle for i in range(len(hay) - n + 1))


def term_accuracy(terms: list[Term], hyp: str, aliases: bool = False) -> float:
    if not terms:
        return 1.0
    words = normalize(hyp)
    hits = 0
    for term in terms:
        spellings = [term.text] + (term.aliases if aliases else [])
        hits += any(_contains(words, normalize(s)) for s in spellings)
    return hits / len(terms)


def score_rows(phrases: list[Phrase], results: list[dict]) -> list[dict]:
    by_id = {p.id: p for p in phrases}
    rows = []
    for r in results:
        p = by_id.get(r["id"])
        if p is None:
            continue
        row = dict(r)
        if "error" not in r:
            hyp = r.get("text", "")
            row["wer"] = wer(p.text, hyp)
            row["terms"] = term_accuracy(p.terms, hyp)
            row["terms_alias"] = term_accuracy(p.terms, hyp, aliases=True)
        rows.append(row)
    return rows


def _p90(values: list[int]) -> int:
    ordered = sorted(values)
    return ordered[min(len(ordered) - 1, int(round(0.9 * (len(ordered) - 1))))]


def summary_table(rows: list[dict]) -> str:
    groups: dict[tuple, list[dict]] = {}
    for r in rows:
        key = (r["backend"], r["model"], r.get("lang", ""), r["device"], "yes" if r.get("hint") else "no")
        groups.setdefault(key, []).append(r)
    lines = []
    for key, rs in groups.items():
        ok = [r for r in rs if "error" not in r]
        if ok:
            ms = [r["ms"] for r in ok]
            stats = (
                statistics.mean(r["terms_alias"] for r in ok),
                statistics.mean(r["terms"] for r in ok),
                statistics.mean(r["wer"] for r in ok),
                int(statistics.median(ms)),
                _p90(ms),
            )
        else:
            stats = (0.0, 0.0, 1.0, 0, 0)
        lines.append((key, stats, len(rs) - len(ok)))
    lines.sort(key=lambda x: (-x[1][0], -x[1][1], x[1][2]))
    out = [
        "| backend | model | lang | device | hint (upper bound) | terms+aliases | terms | WER | median ms | p90 ms | errors |",
        "|---|---|---|---|---|---|---|---|---|---|---|",
    ]
    for key, (ta, t, w, med, p90), errors in lines:
        out.append(f"| {' | '.join(key)} | {ta:.2f} | {t:.2f} | {w:.3f} | {med} | {p90} | {errors} |")
    return "\n".join(out) + "\n"
