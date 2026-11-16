import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { join, dirname } from 'node:path';

const [boardsDir, assetsDir, configPath, outPath] = process.argv.slice(2);
const here = dirname(new URL(import.meta.url).pathname);
const work = join(process.env.TMPDIR || '/tmp', 'boards-page');
mkdirSync(work, { recursive: true });
const config = JSON.parse(readFileSync(configPath, 'utf8'));
const chrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const shim = readFileSync(join(here, 'shim.js'), 'utf8');

const rendered = config.boards.map((b) => {
  const src = readFileSync(join(boardsDir, b.file), 'utf8');
  const page = `<!doctype html><html><head><meta charset="utf-8"><style id="styles"></style></head><body><div id="board"></div><script>window.BOARD_SRC=${JSON.stringify(src).replace(/</g, "\\u003c")};</script><script>${shim}</script></body></html>`;
  const pagePath = join(work, b.file.replace('.dc.html', '.page.html'));
  writeFileSync(pagePath, page);
  const dom = execFileSync(chrome, ['--headless=new', '--disable-gpu', '--virtual-time-budget=2000', '--dump-dom', 'file://' + pagePath], { maxBuffer: 64 << 20 }).toString();
  const styles = /<style id="styles">([\s\S]*?)<\/style>/.exec(dom)[1];
  const board = /<div id="board">([\s\S]*?)<\/div><script>/.exec(dom)[1];
  return { ...b, styles, board };
});

const font = (f) => readFileSync(join(assetsDir, f)).toString('base64');
const fonts = readFileSync(join(assetsDir, 'fonts.css'), 'utf8').replace(/url\('assets\/([^']+)'\)/g, (_, f) => `url(data:font/woff2;base64,${font(f)})`);
const css = readFileSync(join(assetsDir, 'cm.css'), 'utf8') + '\n' + readFileSync(join(assetsDir, 'c.css'), 'utf8');
const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;');

const cards = rendered.map((b) => {
  const zoom = Math.min(1, config.fit / b.w);
  return `<section class="card"><h2>${esc(b.title)}</h2><p class="cap">${esc(b.caption)}</p><div class="frame" style="width:${Math.round(b.w * zoom)}px"><div style="zoom:${zoom.toFixed(4)};width:${b.w}px">${b.board}</div></div></section>`;
}).join('\n');

const html = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>${esc(config.title)}</title>
<style>${fonts}</style>
<style>${css}</style>
<style>${rendered.map((b) => b.styles).join('\n')}</style>
<style>
:root{--pg:#f4f4f2;--pgfg:#1b1d1c;--pgmut:#5d625f;--pgbd:#d9dbd8}
@media (prefers-color-scheme: dark){:root{--pg:#141515;--pgfg:#e7e9e6;--pgmut:#9aa09c;--pgbd:#2c2f2e}}
body.page{margin:0;background:var(--pg);color:var(--pgfg);font-family:'IBM Plex Sans',system-ui,sans-serif}
.page header.top{max-width:${config.fit + 48}px;margin:0 auto;padding:32px 24px 8px}
.page header.top h1{font-size:28px;margin:0 0 8px}
.page header.top p{color:var(--pgmut);margin:0 0 6px;line-height:1.5;max-width:900px}
.page .card{max-width:${config.fit + 48}px;margin:24px auto;padding:0 24px}
.page .card h2{font-size:18px;margin:0 0 4px}
.page .card p.cap{color:var(--pgmut);margin:0 0 12px;font-size:14px}
.page .frame{overflow:hidden;border:1px solid var(--pgbd);border-radius:12px;max-width:100%}
</style></head>
<body class="page">
<header class="top"><h1>${esc(config.title)}</h1>${config.intro.map((p) => `<p>${esc(p)}</p>`).join('')}</header>
${cards}
</body></html>`;
writeFileSync(outPath, html);
console.log(outPath, html.length);
