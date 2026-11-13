import Foundation

struct BrowserSnapshot: Decodable {
    let text: String
    let next: Int
}

struct BrowserActionOutcome: Decodable {
    let missing: Bool?
    let error: String?
}

enum BrowserScript {
    static func decode<T: Decodable>(_ type: T.Type, from json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    static let snapshot = #"""
    const limit = 40000
    const tail = '… truncated, scroll and snapshot again'
    const head = '… content above the scroll position left out, scroll up and snapshot again'
    const roles = new Set(['button', 'link', 'checkbox', 'radio', 'tab', 'menuitem', 'menuitemcheckbox', 'menuitemradio', 'switch', 'combobox', 'textbox', 'searchbox', 'option', 'slider', 'spinbutton'])
    const skipped = new Set(['script', 'style', 'noscript', 'template', 'head', 'svg', 'iframe', 'object', 'canvas'])
    const blockLimit = 4000
    const state = globalThis.__clubmateRefs ??= { refs: new WeakMap(), elements: new Map() }
    for (const [ref, weak] of state.elements) if (!weak.deref()) state.elements.delete(ref)
    const lines = []
    let size = 0
    let full = false
    let buffer = ''
    let fromView = false
    let cut = false
    let counter = next
    const clean = s => (s || '').replace(/[\s\u0085]+/g, ' ').trim()
    const quote = (s, max = 200) => JSON.stringify(clean(s).slice(0, max))
    const push = line => {
        if (full) return
        if (size + line.length + 1 > limit - tail.length - 1) { full = true; return }
        lines.push(line)
        size += line.length + 1
    }
    const flush = () => {
        const text = clean(buffer)
        buffer = ''
        if (text) push(`text ${quote(text.length > blockLimit ? text.slice(0, blockLimit) + ' …' : text, blockLimit + 2)}`)
    }
    const ownText = root => {
        let out = ''
        const walk = node => {
            for (const child of node.childNodes) {
                if (child.nodeType === Node.TEXT_NODE) out += child.data + ' '
                else if (child.nodeType === Node.ELEMENT_NODE && !['select', 'textarea', 'option', 'script', 'style'].includes(child.localName)) walk(child)
            }
        }
        walk(root)
        return clean(out)
    }
    const roleOf = el => {
        const explicit = (el.getAttribute('role') || '').trim().split(/\s+/)[0]
        if (roles.has(explicit)) return explicit
        switch (el.localName) {
        case 'a': return el.hasAttribute('href') ? 'link' : null
        case 'button': case 'summary': return 'button'
        case 'select': return 'combobox'
        case 'textarea': return 'textbox'
        case 'input':
            if (el.type === 'hidden') return null
            if (['button', 'submit', 'reset', 'image'].includes(el.type)) return 'button'
            if (el.type === 'checkbox' || el.type === 'radio') return el.type
            if (el.type === 'range') return 'slider'
            return 'textbox'
        }
        if (el.hasAttribute('contenteditable') && el.isContentEditable) return 'textbox'
        return null
    }
    const nameOf = el => {
        const labelledBy = el.getAttribute('aria-labelledby')
        if (labelledBy) {
            const text = clean(labelledBy.split(/\s+/).map(id => document.getElementById(id)?.textContent || '').join(' '))
            if (text) return text
        }
        const aria = clean(el.getAttribute('aria-label'))
        if (aria) return aria
        if (['input', 'select', 'textarea'].includes(el.localName)) {
            if (['button', 'submit', 'reset'].includes(el.type) && el.value) return el.value
            const label = clean([...(el.labels || [])].map(ownText).join(' '))
            if (label) return label
            return el.getAttribute('placeholder') || el.getAttribute('alt') || el.getAttribute('title') || ''
        }
        return ownText(el) || el.querySelector('img[alt]')?.getAttribute('alt') || el.getAttribute('title') || ''
    }
    const describe = (el, role) => {
        let ref = state.refs.get(el)
        if (!ref) {
            counter += 1
            ref = 'e' + counter
            state.refs.set(el, ref)
            state.elements.set(ref, new WeakRef(el))
        }
        let line = `[${ref}] ${role} ${quote(nameOf(el))}`
        if (role === 'link' && el.localName === 'a') line += ` -> ${quote(el.getAttribute('href'), 500)}`
        if (role === 'textbox' && 'value' in el && el.value && el.type !== 'password') line += ` value=${quote(el.value)}`
        if (el.localName === 'select') {
            const options = [...el.options]
            line += ` selected=${quote(options.filter(o => o.selected).map(o => clean(o.text)).join('|'))}`
            line += ` options=${JSON.stringify(options.slice(0, 30).map(o => clean(o.text)).join('|') + (options.length > 30 ? '|…' : ''))}`
        }
        if (el.checked || el.getAttribute('aria-checked') === 'true') line += ' checked'
        if (el.disabled || el.getAttribute('aria-disabled') === 'true') line += ' disabled'
        return line
    }
    const walk = el => {
        if (full || skipped.has(el.localName)) return
        if (el.getAttribute('aria-hidden') === 'true' || !el.checkVisibility()) return
        if (fromView) {
            const rect = el.getBoundingClientRect()
            if (rect.height > 0 && rect.bottom < 0) {
                cut = true
                for (const child of el.children) walk(child)
                return
            }
        }
        const style = getComputedStyle(el)
        const shown = style.visibility === 'visible'
        const role = roleOf(el)
        if (role && shown) {
            const rect = el.getBoundingClientRect()
            if (rect.width > 0 || rect.height > 0) {
                flush()
                push(describe(el, role))
            }
            return
        }
        if (/^h[1-6]$/.test(el.localName) && shown) {
            flush()
            const text = ownText(el)
            if (text) push(`heading ${quote(text)}`)
            return
        }
        const block = !style.display.startsWith('inline')
        if (block) flush()
        for (const child of el.childNodes) {
            if (child.nodeType === Node.TEXT_NODE) { if (shown) buffer += child.data }
            else if (child.nodeType === Node.ELEMENT_NODE) walk(child)
        }
        if (block) flush()
    }
    const collect = () => {
        lines.length = 0
        size = fromView ? head.length + 1 : 0
        full = false
        buffer = ''
        if (document.body) walk(document.body)
        flush()
    }
    collect()
    if (full) {
        fromView = true
        collect()
    }
    return JSON.stringify({ text: (cut ? head + '\n' : '') + lines.join('\n') + (full ? '\n' + tail : ''), next: counter })
    """#

    private static let prelude = #"""
    const ok = JSON.stringify({})
    const missing = JSON.stringify({ missing: true })
    const fail = message => JSON.stringify({ error: message })
    const find = ref => {
        const el = globalThis.__clubmateRefs?.elements.get(ref)?.deref()
        return el && el.isConnected ? el : null
    }
    const codes = { Enter: 13, Escape: 27, Tab: 9, Backspace: 8, Delete: 46, Space: 32, ArrowLeft: 37, ArrowUp: 38, ArrowRight: 39, ArrowDown: 40, Home: 36, End: 35, PageUp: 33, PageDown: 34 }
    const focusable = () => [...document.querySelectorAll('a[href], button, input:not([type=hidden]), select, textarea, [tabindex]:not([tabindex="-1"]), [contenteditable]')]
        .filter(el => !el.disabled && el.checkVisibility())
    const pressKey = (el, name) => {
        const key = name === 'Space' ? ' ' : name
        const init = { key, code: name.length === 1 ? '' : name, keyCode: codes[name] || 0, which: codes[name] || 0, bubbles: true, cancelable: true, composed: true }
        const allowed = el.dispatchEvent(new KeyboardEvent('keydown', init))
        if (allowed && (name === 'Enter' || key.length === 1)) el.dispatchEvent(new KeyboardEvent('keypress', init))
        el.dispatchEvent(new KeyboardEvent('keyup', init))
        if (!allowed) return
        if (name === 'Enter') {
            const submits = el.form && (el.localName === 'select' || (el.localName === 'input' && !['button', 'submit', 'reset', 'image', 'checkbox', 'radio', 'file'].includes(el.type)))
            if (submits) el.form.requestSubmit()
            else if (['a', 'button', 'summary'].includes(el.localName) || (el.localName === 'input' && ['button', 'submit', 'reset', 'image'].includes(el.type))) el.click()
        } else if (name === 'Tab') {
            const all = focusable()
            const target = all[(all.indexOf(el) + 1) % Math.max(all.length, 1)]
            if (target) target.focus()
        }
    }

    """#

    static let click = prelude + #"""
    const el = find(ref)
    if (!el) return missing
    el.scrollIntoView({ block: 'center', inline: 'center' })
    const rect = el.getBoundingClientRect()
    const init = { bubbles: true, cancelable: true, composed: true, button: 0, clientX: rect.left + rect.width / 2, clientY: rect.top + rect.height / 2 }
    el.dispatchEvent(new PointerEvent('pointerdown', init))
    el.dispatchEvent(new MouseEvent('mousedown', init))
    if (typeof el.focus === 'function') el.focus()
    el.dispatchEvent(new PointerEvent('pointerup', init))
    el.dispatchEvent(new MouseEvent('mouseup', init))
    el.click()
    return ok
    """#

    static let type = prelude + #"""
    const el = find(ref)
    if (!el) return missing
    el.focus()
    if (el.localName === 'input' || el.localName === 'textarea') {
        const proto = el.localName === 'input' ? HTMLInputElement.prototype : HTMLTextAreaElement.prototype
        Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, text)
    } else if (el.isContentEditable) {
        el.textContent = text
    } else {
        return fail(`${ref} is not a text field`)
    }
    el.dispatchEvent(new InputEvent('input', { bubbles: true, composed: true, inputType: 'insertText', data: text }))
    el.dispatchEvent(new Event('change', { bubbles: true }))
    if (submit) {
        if (el.form) el.form.requestSubmit()
        else pressKey(el, 'Enter')
    }
    return ok
    """#

    static let select = prelude + #"""
    const el = find(ref)
    if (!el) return missing
    if (el.localName !== 'select') return fail(`${ref} is not a select`)
    const options = [...el.options]
    const wanted = option.trim().toLowerCase()
    const match = options.find(o => clean(o.text) === option.trim())
        || options.find(o => o.value === option)
        || options.find(o => clean(o.text).toLowerCase() === wanted)
    if (!match) return fail(`option "${option}" not found in ${ref}; options: ${options.map(o => clean(o.text)).join('|')}`)
    el.focus()
    el.selectedIndex = match.index
    el.dispatchEvent(new Event('input', { bubbles: true, composed: true }))
    el.dispatchEvent(new Event('change', { bubbles: true }))
    return ok
    function clean(s) { return (s || '').replace(/[\s\u0085]+/g, ' ').trim() }
    """#

    static let press = prelude + #"""
    pressKey(document.activeElement || document.body, key)
    return ok
    """#

    static let scroll = prelude + #"""
    if (ref) {
        const el = find(ref)
        if (!el) return missing
        el.scrollIntoView({ block: 'center', inline: 'center' })
        return ok
    }
    const steps = { down: [0, 0.8], up: [0, -0.8], right: [0.8, 0], left: [-0.8, 0] }
    const way = direction || 'down'
    if (!Object.hasOwn(steps, way)) return fail('direction must be up, down, left or right')
    window.scrollBy(steps[way][0] * innerWidth, steps[way][1] * innerHeight)
    return ok
    """#

    static let html = #"""
    return document.documentElement ? document.documentElement.outerHTML.slice(0, 300000) : ''
    """#
}
