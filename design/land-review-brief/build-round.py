import base64
import html
import json
import re
import sys
from pathlib import Path

DESIGN = Path(__file__).resolve().parent.parent
ASSETS = DESIGN / "language/html/assets"


def fonts_css():
    out = []
    for line in (ASSETS / "fonts.css").read_text().splitlines():
        m = re.search(r"url\('assets/([^']+)'\)", line)
        if not m:
            continue
        data = base64.b64encode((ASSETS / m.group(1)).read_bytes()).decode()
        out.append(line.replace(m.group(0), f"url(data:font/woff2;base64,{data})"))
    return "\n".join(out)


def board(path):
    src = path.read_text()
    props = re.search(r"data-props='([^']*)'", src)
    defaults = {}
    if props:
        for k, v in json.loads(html.unescape(props.group(1))).items():
            if isinstance(v, dict) and "default" in v:
                defaults[k] = v["default"]
    body = src.split("<x-dc>", 1)[1].split("</x-dc>", 1)[0]
    helmet = re.search(r"<helmet>(.*?)</helmet>", body, re.S)
    styles = []
    if helmet:
        styles = [s for s in re.findall(r"<style>(.*?)</style>", helmet.group(1), re.S)]
        body = body.replace(helmet.group(0), "")
    body = re.sub(r"\{\{\s*(\w+)\s*\}\}", lambda m: str(defaults.get(m.group(1), "")), body)
    styles = [re.sub(r"body\{margin:0\}|a\{[^}]*\}a:hover\{[^}]*\}", "", s) for s in styles]
    return body, "\n".join(styles)


def main():
    spec = json.loads(Path(sys.argv[1]).read_text())
    proj = Path(spec["project"])
    css = [fonts_css(), (ASSETS / "cm.css").read_text(), (ASSETS / "c.css").read_text()]
    sections = []
    for d in spec["directions"]:
        cards = []
        for b in d["boards"]:
            body, style = board(proj / b["file"])
            css.append(style)
            cards.append(
                f'<figure class="bd"><div class="fr" style="--w:{b["w"]}px;--h:{b["h"]}px;--z:{b.get("zoom", 1)}"><div class="zm">{body}</div></div>'
                f'<figcaption>{html.escape(b["caption"])}</figcaption></figure>'
            )
        points = "".join(f"<li>{html.escape(p)}</li>" for p in d["points"])
        sections.append(
            f'<section><h2>{html.escape(d["title"])}</h2><p class="pitch">{html.escape(d["pitch"])}</p>'
            f'<ul class="pts">{points}</ul><div class="rrow">{"".join(cards)}</div></section>'
        )
    page = f"""<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{html.escape(spec["title"])}</title>
<style>
{chr(10).join(css)}
:root{{color-scheme:light dark}}
body.rd{{margin:0;padding:24px 16px 64px;background:#F4F3EE;color:#1B1D1C;font:15px/1.5 'IBM Plex Sans',system-ui,sans-serif}}
@media (prefers-color-scheme:dark){{body.rd{{background:#161817;color:#E8E6DF}} body.rd .pitch,body.rd figcaption,body.rd .pts{{color:#B9B6AC}}}}
.rd h1{{font-size:24px;margin:0 0 6px}} .rd .lead{{margin:0 0 24px;max-width:900px}}
.rd section{{margin:0 0 40px}} .rd h2{{font-size:19px;margin:0 0 4px}}
.rd .pitch{{margin:0 0 6px;color:#55534C;max-width:900px}} .rd .pts{{margin:0 0 14px;color:#55534C;max-width:900px;padding-left:20px}}
.rd .rrow{{display:flex;flex-wrap:wrap;gap:20px;align-items:flex-start}}
.rd figure.bd{{margin:0;display:flex;flex-direction:column;gap:8px;max-width:100%}}
.rd .fr{{width:calc(var(--w) * var(--z));height:calc(var(--h) * var(--z));max-width:100%;overflow:hidden;border-radius:10px;box-shadow:0 1px 4px rgba(0,0,0,.25)}}
.rd .zm{{width:var(--w);height:var(--h);zoom:var(--z)}}
.rd figcaption{{font-size:13px;color:#55534C;max-width:640px}}
</style></head>
<body class="rd">
<h1>{html.escape(spec["title"])}</h1>
<p class="lead">{html.escape(spec["lead"])}</p>
{"".join(sections)}
</body></html>
"""
    Path(spec["out"]).write_text(page)
    print(spec["out"], len(page.encode()) // 1024, "KiB")


main()
