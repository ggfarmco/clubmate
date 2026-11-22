import re, shutil, sys
from pathlib import Path

src, lang, out = map(Path, sys.argv[1:4])
shutil.rmtree(out, ignore_errors=True)
shutil.copytree(src, out)
for board in out.rglob('*.dc.html'):
    text = board.read_text()
    def inline(m):
        child = (lang / f"{m.group(1)}.dc.html").read_text()
        body = child.split('</helmet>', 1)[1].split('</x-dc>', 1)[0]
        return body.replace('{{theme}}', m.group(2))
    new = re.sub(r'<dc-import name="([^"]+)" theme="([^"]+)"[^>]*></dc-import>', inline, text)
    if new != text:
        board.write_text(new)
