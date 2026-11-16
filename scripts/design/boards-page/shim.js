(function () {
  const src = window.BOARD_SRC;
  const doc = new DOMParser().parseFromString(src, 'text/html');
  const script = doc.querySelector('script[data-dc-script]');
  const propsDecl = JSON.parse(script.getAttribute('data-props') || '{}');
  const props = {};
  for (const [k, v] of Object.entries(propsDecl)) if (v && 'default' in v) props[k] = v.default;
  class DCLogic { constructor(p) { this.props = p; this.state = {}; } setState() {} forceUpdate() {} }
  const Component = new Function('DCLogic', script.textContent + '\nreturn Component;')(DCLogic);
  const vals = new Component(props).renderVals();
  const HOLE = /\{\{\s*([^}]+?)\s*\}\}/g;
  const look = (path, scope) => {
    if (path === 'true') return true;
    if (path === 'false') return false;
    if (/^-?\d+(\.\d+)?$/.test(path)) return Number(path);
    if (/^'.*'$/.test(path)) return path.slice(1, -1);
    let cur = scope;
    for (const part of path.split('.')) { if (cur == null) return undefined; cur = cur[part]; }
    return cur;
  };
  const whole = (s, scope) => { const m = /^\s*\{\{\s*([^}]+?)\s*\}\}\s*$/.exec(s || ''); return m ? look(m[1], scope) : s; };
  const interp = (s, scope) => s.replace(HOLE, (_, p) => { const v = look(p, scope); return v == null ? '' : String(v); });
  const expand = (node, scope, out) => {
    if (node.nodeType === 3) { out.appendChild(document.createTextNode(interp(node.nodeValue, scope))); return; }
    if (node.nodeType !== 1) return;
    const tag = node.tagName.toLowerCase();
    if (tag === 'sc-for') {
      const list = whole(node.getAttribute('list'), scope) || [];
      const as = node.getAttribute('as') || 'item';
      list.forEach((item, i) => { const s = Object.assign({}, scope, { [as]: item, $index: i }); node.childNodes.forEach((c) => expand(c, s, out)); });
      return;
    }
    if (tag === 'sc-if') {
      if (whole(node.getAttribute('value'), scope)) node.childNodes.forEach((c) => expand(c, scope, out));
      return;
    }
    if (tag === 'helmet' || tag === 'script') return;
    const el = document.createElementNS(node.namespaceURI, node.tagName);
    for (const a of node.attributes) {
      if (/^on[A-Z]/.test(a.name) || a.name.startsWith('hint-')) continue;
      el.setAttribute(a.name, interp(a.value, scope));
    }
    node.childNodes.forEach((c) => expand(c, scope, el));
    out.appendChild(el);
  };
  const root = doc.querySelector('x-dc');
  const host = document.getElementById('board');
  root.childNodes.forEach((c) => expand(c, vals, host));
  const styles = [...doc.querySelectorAll('helmet style')].map((s) => s.textContent).join('\n');
  document.getElementById('styles').textContent = styles;
})();
