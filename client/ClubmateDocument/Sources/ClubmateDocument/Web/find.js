(() => {
  const all = 'clubmate-find';
  const current = 'clubmate-find-current';
  const limit = 1000;
  const skipped = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE', 'TEXTAREA', 'SELECT']);
  let query = '';
  let ranges = [];
  let index = -1;
  let more = false;
  let text = null;

  const ours = (record) => record.target.id === all || [...record.addedNodes].some((node) => node.id === all);
  new MutationObserver((records) => {
    if (!records.every(ours)) text = null;
  }).observe(document.documentElement, { childList: true, subtree: true, characterData: true, attributes: true });

  const style = () => {
    if (document.getElementById(all)) return;
    const element = document.createElement('style');
    element.id = all;
    element.textContent =
      `::highlight(${all}){background-color:color-mix(in srgb, var(--warning, #F2B84B) 30%, transparent);color:inherit}` +
      `::highlight(${current}){background-color:var(--warning, #F2B84B);color:var(--bg, #000)}`;
    (document.head ?? document.documentElement).append(element);
  };

  const build = () => {
    const visible = new Map();
    const blocks = new Map();
    const shown = (element) => {
      if (!visible.has(element)) {
        visible.set(element, !skipped.has(element.tagName) && !element.closest('svg') && element.checkVisibility({ visibilityProperty: true }));
      }
      return visible.get(element);
    };
    const block = (element) => {
      if (!blocks.has(element)) {
        const display = getComputedStyle(element).display;
        blocks.set(element, display.startsWith('inline') || display === 'contents' ? (element.parentElement ? block(element.parentElement) : element) : element);
      }
      return blocks.get(element);
    };
    const nodes = [];
    let haystack = '';
    let last = null;
    const walker = document.createTreeWalker(document.body ?? document.documentElement, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const parent = node.parentElement;
      if (!parent || !shown(parent)) continue;
      const owner = block(parent);
      if (last && owner !== last) haystack += '\n';
      last = owner;
      const lower = node.data.toLowerCase();
      nodes.push({ node, start: haystack.length });
      haystack += lower.length === node.data.length ? lower : node.data;
    }
    return { haystack, nodes };
  };

  const locate = (offset) => {
    const nodes = text.nodes;
    let low = 0;
    let high = nodes.length - 1;
    while (low < high) {
      const mid = (low + high + 1) >> 1;
      if (nodes[mid].start <= offset) low = mid;
      else high = mid - 1;
    }
    return nodes[low];
  };

  const search = () => {
    text ??= build();
    ranges = [];
    more = false;
    const needle = query.toLowerCase();
    if (!needle || !text.nodes.length) return;
    for (let at = text.haystack.indexOf(needle); at !== -1; at = text.haystack.indexOf(needle, at + needle.length)) {
      if (ranges.length === limit) {
        more = true;
        break;
      }
      const start = locate(at);
      const end = locate(at + needle.length - 1);
      const range = document.createRange();
      range.setStart(start.node, Math.min(at - start.start, start.node.length));
      range.setEnd(end.node, Math.min(at + needle.length - end.start, end.node.length));
      ranges.push(range);
    }
  };

  const paint = () => {
    style();
    CSS.highlights.set(all, new Highlight(...ranges));
    if (ranges[index]) CSS.highlights.set(current, new Highlight(ranges[index]));
    else CSS.highlights.delete(current);
  };

  const reveal = () => {
    const range = ranges[index];
    if (!range) return;
    range.startContainer.parentElement?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' });
    const rect = range.getBoundingClientRect();
    if (rect.top < 0 || rect.bottom > window.innerHeight) {
      window.scrollBy({ top: rect.top - window.innerHeight / 3, behavior: 'instant' });
    }
  };

  const state = () => ({ query, total: ranges.length, current: index + 1, more });

  const firstInView = () => {
    const found = ranges.findIndex((range) => range.getBoundingClientRect().bottom >= 0);
    return found === -1 ? 0 : found;
  };

  const find = (next) => {
    query = next;
    search();
    index = ranges.length ? firstInView() : -1;
    paint();
    reveal();
    return state();
  };

  const step = (delta) => {
    if (!text) {
      const keep = index;
      search();
      index = Math.min(keep, ranges.length - 1);
    }
    if (ranges.length) index = index === -1 ? (delta > 0 ? 0 : ranges.length - 1) : (index + delta + ranges.length) % ranges.length;
    paint();
    reveal();
    return state();
  };

  const refresh = (next) => {
    query = next;
    const keep = index;
    text = null;
    search();
    index = ranges.length ? Math.min(Math.max(keep, 0), ranges.length - 1) : -1;
    paint();
    return state();
  };

  const clear = () => {
    query = '';
    ranges = [];
    index = -1;
    CSS.highlights.delete(all);
    CSS.highlights.delete(current);
    return state();
  };

  window.clubmateFind = { find, step, refresh, clear };
})();
