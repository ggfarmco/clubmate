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

  new ResizeObserver(height).observe(doc);
  configure();
  window.clubmate = { setBody, setTheme, ready: setBody(body) };
})();
