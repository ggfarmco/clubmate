import AppKit
import SwiftUI
import WebKit

import ClubmateKit

struct WebViewHost: View {
    let key: String
    let url: URL?
    let rev: Int

    @Environment(\.webViewDeck) private var deck
    @Environment(\.theme) private var theme

    var body: some View {
        if let deck, let url, !key.isEmpty {
            WebViewRepresentable(deck: deck, key: key, url: url, rev: rev)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            Text(url == nil ? "page unavailable" : "page \(key)")
                .font(theme.ui)
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

private struct WebViewRepresentable: NSViewRepresentable {
    let deck: WebViewDeck
    let key: String
    let url: URL
    let rev: Int

    func makeNSView(context: Context) -> WebViewContainer {
        WebViewContainer()
    }

    func updateNSView(_ container: WebViewContainer, context: Context) {
        guard let view = deck.show(key, url: url, rev: rev) else { return }
        container.hold(view)
    }
}

final class WebViewContainer: NSView {
    private weak var held: CodeWebView?

    func hold(_ view: CodeWebView) {
        guard view.superview !== self else { return }
        held = view
        view.frame = bounds
        view.autoresizingMask = [.width, .height]
        addSubview(view)
        focusHeld()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        focusHeld()
    }

    private func focusHeld() {
        guard let window, let held, held.superview === self else { return }
        window.makeFirstResponder(held)
    }
}

@MainActor
final class WebViewDeck: NSObject, WKNavigationDelegate, WKUIDelegate {
    private var views: [String: CodeWebView] = [:]
    private var closed = false

    func show(_ key: String, url: URL, rev: Int) -> CodeWebView? {
        guard !closed else { return nil }
        let view = views[key] ?? mount(key)
        switch WebViewNavigation.decide(current: view.requested, currentRev: view.rev, url: url, rev: rev) {
        case .none: break
        case .load(let url): view.load(URLRequest(url: url))
        case .reload: view.reload()
        }
        view.requested = url
        view.rev = rev
        return view
    }

    func closeAll() {
        closed = true
        for view in views.values {
            view.stopLoading()
            view.removeFromSuperview()
        }
        views = [:]
    }

    private func mount(_ key: String) -> CodeWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .default()
        let view = CodeWebView(frame: .zero, configuration: configuration)
        view.navigationDelegate = self
        view.uiDelegate = self
        views[key] = view
        return view
    }

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction, decisionHandler: @escaping @MainActor (WKNavigationActionPolicy) -> Void) {
        guard action.targetFrame?.isMainFrame != false, let url = action.request.url, !Self.isLoopback(url) else {
            return decisionHandler(.allow)
        }
        decisionHandler(.cancel)
        openExternal(url)
    }

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let url = action.request.url { openExternal(url) }
        return nil
    }

    private func openExternal(_ url: URL) {
        guard url.scheme == "http" || url.scheme == "https" else { return }
        NSWorkspace.shared.open(url)
    }

    private static func isLoopback(_ url: URL) -> Bool {
        (url.scheme == "http" && url.host() == "127.0.0.1") || url.scheme == "about" || url.scheme == "blob" || url.scheme == "data"
    }
}

final class CodeWebView: WKWebView {
    var requested: URL?
    var rev = 0
    private weak var forwarded: NSEvent?

    private static let claimed: [(String, NSEvent.ModifierFlags)] = [
        ("w", .command), ("n", .command), ("o", .command), ("p", .command), ("p", [.command, .shift]),
    ]

    private static let editing: Set<Selector> = [
        #selector(NSText.copy(_:)), #selector(NSText.paste(_:)), #selector(NSText.cut(_:)), #selector(NSText.selectAll(_:)),
        Selector(("undo:")), Selector(("redo:")),
    ]

    static func owns(_ responder: NSResponder?) -> Bool {
        var view = responder as? NSView
        while let current = view {
            if current is CodeWebView { return true }
            view = current.superview
        }
        return false
    }

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        guard let window, Self.owns(window.firstResponder), event.type == .keyDown else { return super.performKeyEquivalent(with: event) }
        let flags = event.modifierFlags.intersection([.command, .shift, .option, .control])
        let key = event.charactersIgnoringModifiers?.lowercased() ?? ""
        if Self.claimed.contains(where: { $0.0 == key && $0.1 == flags }) {
            guard event !== forwarded else { return false }
            forwarded = event
            keyDown(with: event)
            return true
        }
        if let item = NSApp.mainMenu.flatMap({ Self.item(in: $0, key: key, flags: flags) }),
           item.target != nil || !Self.editing.contains(item.action ?? Selector(("none"))) {
            return false
        }
        return super.performKeyEquivalent(with: event)
    }

    private static func item(in menu: NSMenu, key: String, flags: NSEvent.ModifierFlags) -> NSMenuItem? {
        for item in menu.items {
            if let submenu = item.submenu, let found = Self.item(in: submenu, key: key, flags: flags) { return found }
            guard !item.keyEquivalent.isEmpty else { continue }
            var mask = item.keyEquivalentModifierMask.intersection([.command, .shift, .option, .control])
            if item.keyEquivalent != item.keyEquivalent.lowercased() { mask.insert(.shift) }
            if item.keyEquivalent.lowercased() == key, mask == flags { return item }
        }
        return nil
    }
}

private struct WebViewDeckKey: EnvironmentKey {
    static let defaultValue: WebViewDeck? = nil
}

extension EnvironmentValues {
    var webViewDeck: WebViewDeck? {
        get { self[WebViewDeckKey.self] }
        set { self[WebViewDeckKey.self] = newValue }
    }
}
