(() => {
  const doc = document.getElementById('doc');
  const post = (message) => window.webkit?.messageHandlers?.clubmate?.postMessage(message);
  let body = doc.innerHTML;
  let generation = 0;
  let diagrams = new Map();

  const configure = () => {
    if (typeof mermaid !== 'object') return;
    const style = getComputedStyle(document.documentElement);
    const v = (name) => style.getPropertyValue(`--${name}`).trim();
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: 'strict',
      suppressErrorRendering: true,
      theme: 'base',
      fontFamily: v('font'),
      sequence: {
        actorFontFamily: v('font'),
        actorFontSize: v('size'),
        messageFontFamily: v('font'),
        messageFontSize: v('size'),
        noteFontFamily: v('font'),
        noteFontSize: v('size'),
      },
      themeVariables: {
        darkMode: v('dark') === '1',
        fontFamily: v('font'),
        fontSize: v('size'),
        background: v('bg'),
        primaryColor: v('code-bg'),
        primaryTextColor: v('text'),
        primaryBorderColor: v('muted'),
        secondaryColor: v('code-bg'),
        secondaryTextColor: v('text'),
        secondaryBorderColor: v('muted'),
        tertiaryColor: v('bg'),
        tertiaryTextColor: v('text'),
        tertiaryBorderColor: v('border'),
        lineColor: v('muted'),
        textColor: v('text'),
        mainBkg: v('code-bg'),
        nodeBorder: v('muted'),
        clusterBkg: v('bg'),
        clusterBorder: v('border'),
        edgeLabelBackground: v('bg'),
        noteBkgColor: v('code-bg'),
        noteTextColor: v('text'),
        noteBorderColor: v('muted'),
        actorBkg: v('code-bg'),
        actorBorder: v('muted'),
        actorTextColor: v('text'),
        actorLineColor: v('muted'),
        signalColor: v('text'),
        signalTextColor: v('text'),
        labelBoxBkgColor: v('code-bg'),
        labelBoxBorderColor: v('muted'),
        labelTextColor: v('text'),
        loopTextColor: v('text'),
        activationBkgColor: v('code-bg'),
        activationBorderColor: v('muted'),
        sequenceNumberColor: v('bg'),
        errorBkgColor: v('danger'),
        errorTextColor: v('text'),
      },
    });
  };

  const draw = async (source, id) => {
    if (typeof mermaid !== 'object') return { error: 'mermaid is not loaded' };
    try {
      return { svg: (await mermaid.render(id, source)).svg };
    } catch (error) {
      return { error: String(error?.message ?? error) };
    }
  };

  const height = () => {
    const rect = doc.getBoundingClientRect();
    post({ type: 'height', value: Math.ceil(rect.bottom + window.scrollY) });
  };

  const lightbox = (() => {
    const root = document.createElement('div');
    const stage = document.createElement('div');
    const bar = document.createElement('div');
    root.className = 'lightbox';
    root.hidden = true;
    root.setAttribute('role', 'dialog');
    root.setAttribute('aria-modal', 'true');
    root.setAttribute('aria-label', 'Diagram');
    stage.className = 'lightbox-stage';
    bar.className = 'lightbox-bar';
    root.append(stage, bar);
    document.body.append(root);

    let scale = 1;
    let x = 0;
    let y = 0;
    let width = 0;
    let height = 0;
    const pointers = new Map();
    let pinch = null;
    let gesture = 1;

    const apply = () => {
      stage.style.transform = `translate(${x}px, ${y}px) scale(${scale})`;
    };

    const zoomAt = (factor, cx, cy) => {
      const next = Math.min(10, Math.max(0.1, scale * factor));
      x = cx - ((cx - x) * next) / scale;
      y = cy - ((cy - y) * next) / scale;
      scale = next;
      apply();
    };

    const zoomCenter = (factor) => zoomAt(factor, root.clientWidth / 2, root.clientHeight / 2);

    const fit = () => {
      const pad = 24;
      const w = root.clientWidth;
      const h = root.clientHeight;
      scale = Math.max(0.1, Math.min((w - pad * 2) / width, (h - pad * 2) / height));
      x = (w - width * scale) / 2;
      y = (h - height * scale) / 2;
      apply();
    };

    const close = () => {
      if (root.hidden) return;
      root.hidden = true;
      stage.replaceChildren();
      pointers.clear();
      pinch = null;
      document.documentElement.classList.remove('lightbox-open');
      post({ type: 'lightbox', open: false });
    };

    const button = (label, glyph, action) => {
      const element = document.createElement('button');
      element.type = 'button';
      element.className = 'lightbox-button';
      element.title = label;
      element.setAttribute('aria-label', label);
      element.textContent = glyph;
      element.addEventListener('click', action);
      bar.append(element);
      return element;
    };

    button('Zoom out', '\u2212', () => zoomCenter(1 / 1.25));
    button('Zoom in', '+', () => zoomCenter(1.25));
    button('Fit', 'Fit', fit);
    const closer = button('Close', '\u2715', close);

    const open = (svg) => {
      const box = svg.viewBox?.baseVal;
      const rect = svg.getBoundingClientRect();
      width = box?.width || rect.width || 1;
      height = box?.height || rect.height || 1;
      const copy = svg.cloneNode(true);
      copy.removeAttribute('style');
      copy.setAttribute('width', width);
      copy.setAttribute('height', height);
      stage.style.width = `${width}px`;
      stage.style.height = `${height}px`;
      stage.replaceChildren(copy);
      root.hidden = false;
      document.documentElement.classList.add('lightbox-open');
      fit();
      closer.focus({ preventScroll: true });
      post({ type: 'lightbox', open: true });
    };

    const distance = () => {
      const [a, b] = [...pointers.values()];
      return { d: Math.hypot(a.x - b.x, a.y - b.y), x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
    };

    root.addEventListener('pointerdown', (event) => {
      if (event.target.closest('.lightbox-bar')) return;
      root.setPointerCapture(event.pointerId);
      pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
      pinch = pointers.size === 2 ? distance() : null;
    });
    root.addEventListener('pointermove', (event) => {
      const last = pointers.get(event.pointerId);
      if (!last) return;
      const point = { x: event.clientX, y: event.clientY };
      pointers.set(event.pointerId, point);
      if (pinch && pointers.size === 2) {
        const next = distance();
        x += next.x - pinch.x;
        y += next.y - pinch.y;
        zoomAt(next.d / pinch.d, next.x, next.y);
        pinch = next;
      } else if (pointers.size === 1) {
        x += point.x - last.x;
        y += point.y - last.y;
        apply();
      }
    });
    const release = (event) => {
      pointers.delete(event.pointerId);
      pinch = pointers.size === 2 ? distance() : null;
    };
    root.addEventListener('pointerup', release);
    root.addEventListener('pointercancel', release);
    root.addEventListener('wheel', (event) => {
      event.preventDefault();
      if (event.ctrlKey || event.metaKey) {
        zoomAt(Math.exp(-event.deltaY / 100), event.clientX, event.clientY);
      } else {
        x -= event.deltaX;
        y -= event.deltaY;
        apply();
      }
    }, { passive: false });
    root.addEventListener('touchmove', (event) => event.preventDefault(), { passive: false });
    root.addEventListener('gesturestart', (event) => {
      event.preventDefault();
      gesture = 1;
    });
    root.addEventListener('gesturechange', (event) => {
      event.preventDefault();
      if (pointers.size < 2) zoomAt(event.scale / gesture, event.clientX, event.clientY);
      gesture = event.scale;
    });
    document.addEventListener('keydown', (event) => {
      if (root.hidden || event.metaKey || event.ctrlKey || event.altKey) return;
      const actions = {
        Escape: close,
        '+': () => zoomCenter(1.25),
        '=': () => zoomCenter(1.25),
        '-': () => zoomCenter(1 / 1.25),
        0: fit,
      };
      const action = actions[event.key];
      if (!action) return;
      event.preventDefault();
      event.stopPropagation();
      action();
    });
    doc.addEventListener('click', (event) => {
      const svg = event.target.closest?.('.mermaid-diagram')?.querySelector('svg');
      if (!svg || document.documentElement.classList.contains('fit') || event.target.closest('a')) return;
      open(svg);
    });
    return { close };
  })();

  const setBody = async (html) => {
    const gen = ++generation;
    body = html;
    const next = document.createElement('main');
    next.innerHTML = html;
    const blocks = [...next.querySelectorAll('pre.mermaid')];
    const used = new Map();
    const errors = [];
    for (const [index, pre] of blocks.entries()) {
      const source = pre.textContent;
      let result = diagrams.get(source);
      if (!result) {
        result = await draw(source, `mermaid-${gen}-${index}`);
        if (gen !== generation) return false;
      }
      used.set(source, result);
      if (result.svg) {
        const figure = document.createElement('div');
        figure.className = 'mermaid-diagram';
        figure.innerHTML = result.svg;
        pre.replaceWith(figure);
      } else {
        pre.classList.add('mermaid-failed');
        const message = document.createElement('div');
        message.className = 'mermaid-error';
        message.setAttribute('role', 'alert');
        message.textContent = result.error;
        pre.after(message);
        errors.push({ index, message: result.error });
      }
    }
    diagrams = used;
    lightbox.close();
    const y = window.scrollY;
    doc.replaceChildren(...next.childNodes);
    window.scrollTo(0, y);
    errors.forEach((error) => post({ type: 'mermaidError', ...error }));
    height();
    return true;
  };

  const setTheme = (css) => {
    document.getElementById('theme').textContent = css;
    diagrams = new Map();
    configure();
    return setBody(body);
  };

  const scrollTo = (anchor) => {
    let decoded = anchor;
    try {
      decoded = decodeURIComponent(anchor);
    } catch {}
    const target = document.getElementById(anchor) ?? document.getElementById(decoded);
    target?.scrollIntoView({ block: 'start' });
    return target != null;
  };

  new ResizeObserver(height).observe(doc);
  configure();
  window.clubmate = { setBody, setTheme, scrollTo, ready: setBody(body) };
})();
