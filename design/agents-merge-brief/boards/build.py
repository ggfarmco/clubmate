import html
import json
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent

PAGES = [
    ("a-one-tree", "A · One tree (recommended)", [
        "AG-A-Run-1280-Mate", "AG-A-Gate-1280-Mate", "AG-A-Tree-640-MateLight", "AG-A-Gate-640-MateLight",
        "C-AG-A-Agents", "C-AG-A-Graphs-Light", "C-AG-A-Run", "C-AG-A-Gate-Light",
    ]),
    ("b-tabs", "B · Tabs", ["AG-B-Graphs-1280-Mate", "AG-B-Gates-640-MateLight", "C-AG-B-Agents-Light"]),
    ("c-group-by", "C · Group by", ["AG-C-Graph-1280-Mate", "AG-C-Gate-640-MateLight", "C-AG-C-Agents"]),
    ("rules", "Rules, keys, links, phone, handoff", ["AG-Rules"]),
]

SCRIPT = "<script>for(const b of document.querySelectorAll('.board')){b.attachShadow({mode:'open'}).appendChild(b.querySelector('template').content.cloneNode(true))}</script>"

PAGE_CSS = """
:root{color-scheme:light dark;--bg:#f4f4f2;--fg:#1d1d1b;--mut:#6b6b66;--bd:#dcdcd6}
@media (prefers-color-scheme:dark){:root{--bg:#161615;--fg:#ececea;--mut:#9a9a94;--bd:#2c2c2a}}
body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.5 -apple-system,system-ui,sans-serif}
header{padding:20px 24px 8px}
h1{font-size:20px;margin:0 0 4px}
nav a{color:var(--mut);margin-right:14px;text-decoration:none}
nav a.on{color:var(--fg);font-weight:600}
.boards{display:flex;flex-wrap:wrap;gap:28px;padding:16px 24px 40px;align-items:flex-start}
figure{margin:0}
figcaption{color:var(--mut);font-size:12px;margin-bottom:6px;max-width:100%}
.board{border:1px solid var(--bd);border-radius:8px;overflow:hidden;position:relative}
"""


def board(src, name, css):
    text = (src / f"{name}.dc.html").read_text()
    props = re.search(r"data-props='([^']*)'", text)
    props = json.loads(props.group(1)) if props else {}
    theme = props.get("theme", {}).get("default", "")
    size = props.get("$preview", {})
    title = re.search(r"<title>(.*?)</title>", text, re.S).group(1)
    body = re.search(r"<x-dc>(.*)</x-dc>", text, re.S).group(1)
    helmet = re.search(r"<helmet>(.*?)</helmet>", body, re.S)
    head = helmet.group(1) if helmet else ""
    body = body.replace(helmet.group(0), "") if helmet else body
    body = body.replace("{{theme}}", theme)
    links = re.findall(r'<link rel="stylesheet" href="/_blob/([0-9a-f]+)">', text)
    styles = "".join(f"<style>{css[i]}</style>" for i in links)
    doc = f'{styles}{head}{body}'
    width = size.get("width")
    height = size.get("height")
    if not width:
        m = re.search(r'class="cm[^"]*" style="width: (\d+)px(?:; height: (\d+)px)?', body)
        width = int(m.group(1)) if m else 1280
        height = int(m.group(2)) if m and m.group(2) else 0
    return width, height, title, doc


def main():
    src = pathlib.Path(sys.argv[1])
    css = {p.stem: p.read_text() for p in src.glob("*.css")}
    nav = lambda cur: "".join(f'<a class="{"on" if slug == cur else ""}" href="{slug}.html">{html.escape(t)}</a>' for slug, t, _ in PAGES)
    for slug, title, names in PAGES:
        figs = []
        for n in names:
            w, h, t, doc = board(src / "project", n, css)
            figs.append(f'<figure style="width:{w}px"><figcaption>{html.escape(n)} · {html.escape(t)}</figcaption>'
                        f'<div class="board" style="width:{w}px;{f"height:{h}px" if h else ""}"><template>{doc}</template></div></figure>')
        page = (f'<!doctype html><html><head><meta charset="utf-8"><title>{html.escape(title)}</title><style>{PAGE_CSS}</style></head>'
                f'<body><header><h1>Graphs and Gates in Agents · {html.escape(title)}</h1><nav>{nav(slug)}</nav></header>'
                f'<div class="boards">{"".join(figs)}</div>{SCRIPT}</body></html>')
        (HERE / f"{slug}.html").write_text(page)
        print(slug)


if __name__ == "__main__":
    main()
