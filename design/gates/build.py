import base64
import pathlib
import re

HERE = pathlib.Path(__file__).resolve().parent
ASSETS = HERE.parent / "language" / "html" / "assets"

FONTS = [
    ("IBM Plex Sans", 400, "ibm-plex-sans-latin-400-normal.woff2"),
    ("IBM Plex Sans", 500, "ibm-plex-sans-latin-500-normal.woff2"),
    ("IBM Plex Sans", 600, "ibm-plex-sans-latin-600-normal.woff2"),
    ("JetBrains Mono", 400, "jetbrains-mono-latin-400-normal.woff2"),
    ("JetBrains Mono", 500, "jetbrains-mono-latin-500-normal.woff2"),
    ("JetBrains Mono", 600, "jetbrains-mono-latin-600-normal.woff2"),
]

ICONS = {
    "shield": '<path d="M8 1.5 13 3.5v4.2c0 3-2.1 5.4-5 6.8-2.9-1.4-5-3.8-5-6.8V3.5Z"/>',
    "shieldok": '<path d="M8 1.5 13 3.5v4.2c0 3-2.1 5.4-5 6.8-2.9-1.4-5-3.8-5-6.8V3.5Z"/><path d="m5.8 8 1.6 1.6 2.9-3"/>',
    "check": '<path d="m3.5 8.5 3 3 6-7"/>',
    "x": '<path d="M4 4l8 8M12 4l-8 8"/>',
    "warn": '<path d="M8 2 14.5 13.5h-13Z"/><path d="M8 6.5v3M8 11.5v.01"/>',
    "pending": '<circle cx="8" cy="8" r="6" stroke-dasharray="2.2 2.2"/>',
    "doc": '<path d="M4 1.5h5l3 3v10H4Z"/><path d="M9 1.5v3h3"/>',
    "user": '<circle cx="8" cy="5.5" r="2.5"/><path d="M3 14c.6-2.8 2.6-4.2 5-4.2s4.4 1.4 5 4.2"/>',
    "folder": '<path d="M1.5 4.5V12a1 1 0 0 0 1 1h11a1 1 0 0 0 1-1V6a1 1 0 0 0-1-1H7.5L6 3.5H2.5a1 1 0 0 0-1 1Z"/>',
    "branch": '<circle cx="4.5" cy="3.5" r="1.5"/><circle cx="4.5" cy="12.5" r="1.5"/><circle cx="11.5" cy="5" r="1.5"/><path d="M4.5 5v6M11.5 6.5c0 3-4 2.5-7 4.5"/>',
    "merge": '<circle cx="4.5" cy="3.5" r="1.5"/><circle cx="4.5" cy="12.5" r="1.5"/><circle cx="11.5" cy="12.5" r="1.5"/><path d="M4.5 5v6M4.5 5c0 4 7 3 7 6"/>',
    "plus": '<path d="M8 3v10M3 8h10"/>',
    "down": '<path d="M3.5 6 8 10.5 12.5 6"/>',
    "right": '<path d="M6 3.5 10.5 8 6 12.5"/>',
    "left": '<path d="M10 3.5 5.5 8l4.5 4.5"/>',
    "pause": '<path d="M5.5 3.5v9M10.5 3.5v9"/>',
    "play": '<path d="M5 3.5v9l7.5-4.5Z"/>',
    "edit": '<path d="M3 13l.8-3.2 6.7-6.7 2.4 2.4-6.7 6.7Z"/>',
    "trash": '<path d="M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.7 9h5.6l.7-9"/>',
    "sparkle": '<path d="M8 1.5 9.3 6.7 14.5 8 9.3 9.3 8 14.5 6.7 9.3 1.5 8 6.7 6.7Z"/>',
    "bolt": '<path d="M9 1.5 3.5 9H8l-1 5.5L12.5 7H8Z"/>',
    "help": '<circle cx="8" cy="8" r="6.5"/><path d="M6.2 6.2a1.9 1.9 0 1 1 2.6 1.8c-.5.2-.8.6-.8 1.1v.4M8 11.5v.01"/>',
    "arrow": '<path d="M3 8h10M9 4l4 4-4 4"/>',
    "out": '<path d="M9.5 2.5h4v4M13.5 2.5 8 8M12 9.5V13a.5.5 0 0 1-.5.5h-8A.5.5 0 0 1 3 13V5a.5.5 0 0 1 .5-.5H7"/>',
    "gear": '<circle cx="8" cy="8" r="2.2"/><path d="M8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4"/>',
    "search": '<circle cx="7" cy="7" r="4.5"/><path d="m10.5 10.5 3.5 3.5"/>',
    "more": '<circle cx="3.5" cy="8" r=".9"/><circle cx="8" cy="8" r=".9"/><circle cx="12.5" cy="8" r=".9"/>',
    "bell": '<path d="M4 11V7a4 4 0 0 1 8 0v4l1.5 1.5h-11Z"/><path d="M6.5 14a1.5 1.5 0 0 0 3 0"/>',
    "windows": '<rect x="1.5" y="3.5" width="9" height="9" rx="1.5"/><path d="M5.5 1.5h7a2 2 0 0 1 2 2v7"/>',
}

RAIL = [
    ("code", "Code", '<path d="M5.5 4 2 8l3.5 4M10.5 4 14 8l-3.5 4"/>'),
    ("terminals", "Terminals", '<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="m4.5 6 2 2-2 2M8.5 10.5h3"/>'),
    ("agents", "Agents", ICONS["sparkle"]),
    ("backlog", "Backlog", '<path d="m1.8 4 1.2 1.2L5 3M7.5 4h7M7.5 8.5h7M7.5 13h7"/><circle cx="3.3" cy="8.5" r="1.3"/><circle cx="3.3" cy="13" r="1.3"/>'),
    ("processes", "Processes", '<rect x="4" y="4" width="8" height="8" rx="1.5"/><path d="M6.5 1.5v2.5M9.5 1.5v2.5M6.5 12v2.5M9.5 12v2.5M1.5 6.5H4M1.5 9.5H4M12 6.5h2.5M12 9.5h2.5"/>'),
    ("cron", "Cron", '<circle cx="8" cy="8" r="6.5"/><path d="M8 4.5V8l2.5 1.5"/>'),
    ("triggers", "Triggers", ICONS["bolt"]),
    ("graphs", "Graphs", '<circle cx="3.5" cy="4" r="1.8"/><circle cx="12.5" cy="4" r="1.8"/><circle cx="8" cy="12" r="1.8"/><path d="M5.3 4h5.4M4.4 5.6l2.7 4.8M11.6 5.6l-2.7 4.8"/>'),
]


def svg(name, cls):
    return f'<svg class="{cls}" viewBox="0 0 16 16" aria-hidden="true">{ICONS[name]}</svg>'


def rail(spec):
    on, _, extra = spec.partition("+")
    sections = list(RAIL)
    if extra == "gates":
        sections.insert(7, ("gates", "Gates", ICONS["shield"]))
    buttons = []
    for n, (sid, title, path) in enumerate(sections, 1):
        key = f" ⌘{n}" if n <= 9 else ""
        cls = "ri on" if sid == on else "ri"
        buttons.append(f'<button class="{cls}" aria-label="{title}{key}"><svg class="i s16" viewBox="0 0 16 16" aria-hidden="true">{path}</svg></button>')
    return '<nav class="rail" aria-label="Sections">' + "".join(buttons) + "</nav>"


def titlebar(branch):
    return (
        '<header class="tb"><div class="lights"><i style="background: #FF5F57"></i><i style="background: #FEBC2E"></i><i style="background: #28C840"></i></div>'
        f'<button class="pill"><span>clubmate</span><span class="faint">/</span>{svg("branch", "i mut")}<span>{branch}</span>{svg("down", "i s12 mut")}</button>'
        '<span style="flex: 1"></span>'
        f'<button class="pill" aria-label="Notifications">{svg("bell", "i")}</button>'
        f'<button class="pill" aria-label="Windows: 1">{svg("windows", "i")}<span class="t2">1</span></button></header>'
    )


def head():
    faces = []
    for family, weight, name in FONTS:
        data = base64.b64encode((ASSETS / name).read_bytes()).decode()
        faces.append(f"@font-face{{font-family:'{family}';font-weight:{weight};font-style:normal;src:url(data:font/woff2;base64,{data}) format('woff2')}}")
    css = "\n".join(faces) + "\n" + (ASSETS / "cm.css").read_text() + "\n" + (ASSETS / "c.css").read_text() + "\n" + (HERE / "src" / "boards.css").read_text()
    return f"<style>\n{css}\n</style>"


def render(text):
    text = text.replace("<!--HEAD-->", head())
    text = re.sub(r"\{\{rail:([a-z+]+)\}\}", lambda m: rail(m.group(1)), text)
    text = re.sub(r"\{\{tb:([^}]+)\}\}", lambda m: titlebar(m.group(1)), text)
    text = re.sub(r"\{\{(i|i16|i12):([a-z]+)((?: [a-z0-9-]+)*)\}\}", lambda m: svg(m.group(2), {"i": "i", "i16": "i s16", "i12": "i s12"}[m.group(1)] + m.group(3)), text)
    return text


def main():
    for src in sorted((HERE / "src").glob("*.html")):
        (HERE / src.name).write_text(render(src.read_text()))
        print(src.name)


if __name__ == "__main__":
    main()
