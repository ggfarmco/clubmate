import Foundation
import SwiftUI
import WebKit

@MainActor
final class DocumentController: NSObject, WKNavigationDelegate, WKUIDelegate {
    private struct Request: Equatable {
        var content: DocumentContent
        var theme: DocumentTheme
        var size: DocumentSize
    }

    let webView: DocumentWKWebView
    let kind: DocumentKind
    var onLink: (DocumentLink) -> Void = { _ in }
    var onHeight: (CGFloat) -> Void = { _ in }
    var onScroll: ((DocumentScroll) -> Void)?
    var onTextTap: (() -> Void)?
    var onPlace: ((Double) -> Void)?
    var zoom: DocumentZoom? {
        didSet { zoom?.controller = self }
    }
    var finder: DocumentFind? {
        didSet { finder?.controller = self }
    }

    private var wanted: Request?
    private var shown: Request?
    private var work: Task<Void, Never>?
    private var loading: CheckedContinuation<Void, Never>?
    private let artifact = ArtifactScheme()
    private static let markdownStore = WKWebsiteDataStore.nonPersistent()
    private static let findScript = (try? String(contentsOf: DocumentPage.web.appendingPathComponent("find.js"), encoding: .utf8)) ?? ""
    private var artifactLoad: URL?
    private static var spare: DocumentController?
    private var warmed = false
    private var refills = false
    private var place: Double?
    private var placed = false
    private static let tapScript = "let start = null; addEventListener('touchstart', e => { start = e.touches.length === 1 ? { x: e.touches[0].clientX, y: e.touches[0].clientY, t: Date.now() } : null }, { passive: true, capture: true }); addEventListener('touchend', e => { const s = start; start = null; const p = e.changedTouches[0]; if (!s || !p || Date.now() - s.t > 400 || Math.hypot(p.clientX - s.x, p.clientY - s.y) > 10) return; if (e.target.closest && e.target.closest('a, button, input, textarea, select, summary, label, img, svg, [onclick], [contenteditable]')) return; if (getSelection().toString()) return; window.webkit.messageHandlers.clubmateTap.postMessage(1) }, { passive: true, capture: true })"
    #if os(iOS)
    private var headerHost: UIHostingController<AnyView>?
    private var offsetWatch: NSKeyValueObservation?
    private var lastOffset: CGFloat?
    private var lastTop: CGFloat?
    private var keeping = false
    #endif

    init(kind: DocumentKind = .markdown) {
        self.kind = kind
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = kind == .markdown ? Self.markdownStore : .nonPersistent()
        switch kind {
        case .markdown:
            configuration.setURLSchemeHandler(DocumentScheme(), forURLScheme: DocumentScheme.name)
        case .artifact:
            configuration.setURLSchemeHandler(artifact, forURLScheme: ArtifactScheme.name)
            configuration.preferences.javaScriptCanOpenWindowsAutomatically = false
            if configuration.preferences.responds(to: NSSelectorFromString("_setPeerConnectionEnabled:")) {
                configuration.preferences.setValue(false, forKey: "peerConnectionEnabled")
            }
            #if os(iOS)
            configuration.ignoresViewportScaleLimits = true
            #endif
        }
        webView = DocumentWKWebView(frame: .zero, configuration: configuration)
        super.init()
        switch kind {
        case .markdown:
            configuration.userContentController.add(MessageProxy(self), name: "clubmate")
        case .artifact:
            webView.uiDelegate = self
        }
        installFind()
        #if os(iOS)
        configuration.userContentController.add(MessageProxy(self), contentWorld: .defaultClient, name: "clubmateTap")
        offsetWatch = webView.scrollView.observe(\.contentOffset) { [weak self] scrollView, _ in
            MainActor.assumeIsolated { self?.scrolled(scrollView) }
        }
        #endif
        webView.navigationDelegate = self
        webView.allowsLinkPreview = false
        webView.allowsBackForwardNavigationGestures = false
        #if os(macOS)
        if ["setDrawsBackground:", "_setDrawsBackground:"].contains(where: { webView.responds(to: NSSelectorFromString($0)) }) {
            webView.setValue(false, forKey: "drawsBackground")
        } else {
            webView.underPageBackgroundColor = .clear
        }
        #else
        webView.isFindInteractionEnabled = true
        webView.isOpaque = false
        webView.backgroundColor = .clear
        webView.scrollView.backgroundColor = .clear
        #endif
    }

    static func prepare() {
        guard spare == nil else { return }
        let page = DocumentController()
        page.webView.loadHTMLString(DocumentPage.html(DocumentContent(path: "", html: "", rev: 0), theme: .system(dark: false), size: .fill), baseURL: DocumentScheme.base)
        spare = page
    }

    static func make(kind: DocumentKind) -> DocumentController {
        guard kind == .markdown, let page = spare, page.warmed else { return DocumentController(kind: kind) }
        spare = nil
        page.refills = true
        return page
    }

    func show(_ content: DocumentContent, theme: DocumentTheme, size: DocumentSize) {
        wanted = Request(content: content, theme: theme, size: size)
        #if os(iOS)
        webView.scrollView.isScrollEnabled = size == .fill
        #else
        webView.passesVerticalScroll = size == .fit
        #endif
        guard work == nil else { return }
        work = Task { await drain() }
    }

    func restore(_ y: Double?) {
        guard !placed else { return }
        place = y
    }

    func setScale(_ scale: Double) {
        #if os(macOS)
        let zoom = wanted?.size == .fill ? scale : 1
        if webView.pageZoom != zoom { webView.pageZoom = zoom }
        updateMagnification()
        #endif
    }

    private func updateMagnification() {
        #if os(macOS)
        webView.allowsMagnification = wanted?.size == .fill && zoom?.lightbox != true
        #endif
    }

    func resetMagnification() {
        #if os(macOS)
        webView.magnification = 1
        #else
        webView.scrollView.setZoomScale(webView.scrollView.minimumZoomScale, animated: false)
        #endif
    }

    func find(_ command: FindCommand) async -> FindResult? {
        let (script, arguments) = command.script
        return FindResult(try? await webView.callAsyncJavaScript(script, arguments: arguments, contentWorld: .defaultClient))
    }

    #if os(iOS)
    func presentFindNavigator() {
        webView.findInteraction?.presentFindNavigator(showingReplace: false)
    }

    func setHardTopEdge(_ hard: Bool) {
        webView.scrollView.topEdgeEffect.style = hard ? .hard : .automatic
    }

    func setHeader(_ header: AnyView?) {
        guard let header else {
            headerHost = nil
            webView.header = nil
            return
        }
        if let headerHost {
            headerHost.rootView = header
        } else {
            let host = UIHostingController(rootView: header)
            host.view.backgroundColor = .clear
            headerHost = host
            webView.header = host.view
        }
        webView.setNeedsLayout()
    }

    private func scrolled(_ scrollView: UIScrollView) {
        let top = scrollView.adjustedContentInset.top
        if !keeping, let lastOffset, let lastTop, top != lastTop, lastOffset > -lastTop + 1, !scrollView.isTracking, !scrollView.isDecelerating, scrollView.contentOffset.y != lastOffset {
            keeping = true
            scrollView.contentOffset.y = lastOffset
            keeping = false
        }
        let y = scrollView.contentOffset.y
        defer {
            lastOffset = y
            lastTop = top
        }
        guard let onScroll, let lastOffset else { return }
        let inset = scrollView.adjustedContentInset
        let offset = y + inset.top
        let range = scrollView.contentSize.height + inset.top + inset.bottom - scrollView.bounds.height
        onScroll(DocumentScroll(offset: offset, delta: y - lastOffset, progress: range > 0 ? min(max(offset / range, 0), 1) : 0, tracking: scrollView.isTracking || scrollView.isDecelerating))
    }
    #endif

    private func installFind() {
        webView.configuration.userContentController.addUserScript(WKUserScript(source: Self.findScript, injectionTime: .atDocumentEnd, forMainFrameOnly: true, in: .defaultClient))
        #if os(iOS)
        webView.configuration.userContentController.addUserScript(WKUserScript(source: Self.tapScript, injectionTime: .atDocumentEnd, forMainFrameOnly: true, in: .defaultClient))
        #endif
    }

    func settled() async {
        await work?.value
    }

    func close() {
        if kind == .markdown {
            webView.configuration.userContentController.removeScriptMessageHandler(forName: "clubmate")
        }
        #if os(iOS)
        webView.configuration.userContentController.removeScriptMessageHandler(forName: "clubmateTap", contentWorld: .defaultClient)
        offsetWatch = nil
        #endif
        webView.navigationDelegate = nil
        webView.uiDelegate = nil
        zoom?.lightbox = false
        loading?.resume()
        loading = nil
    }

    private func drain() async {
        while let next = wanted, next != shown {
            let previous = shown
            shown = next
            let change = DocumentPage.update(old: previous?.content, new: next.content)
            if previous == nil || previous?.size != next.size || change == .load {
                await load(next)
                await finder?.refresh()
                continue
            }
            if previous?.theme != next.theme {
                await setTheme(next.theme)
            }
            switch change {
            case .replaceBody:
                await call("return await window.clubmate.setBody(html)", ["html": next.content.html])
                await finder?.refresh()
                if next.content.anchor != previous?.content.anchor {
                    await scroll(to: next.content.anchor)
                }
            case .scroll:
                await scroll(to: next.content.anchor)
            case .load, .none:
                break
            }
        }
        work = nil
    }

    private func load(_ request: Request) async {
        zoom?.lightbox = false
        await withCheckedContinuation { continuation in
            loading?.resume()
            loading = continuation
            switch kind {
            case .markdown:
                webView.loadHTMLString(DocumentPage.html(request.content, theme: request.theme, size: request.size), baseURL: DocumentScheme.base)
            case .artifact:
                installTheme(request.theme)
                let url = artifact.serve(path: request.content.path, html: request.content.html)
                artifactLoad = url
                webView.load(URLRequest(url: url))
            }
        }
        if kind == .markdown {
            await call("return await window.clubmate.ready", [:])
        }
        if refills {
            refills = false
            Self.prepare()
        }
        await scroll(to: request.content.anchor)
        let start = placed ? nil : place
        placed = true
        place = nil
        if kind == .markdown, onPlace != nil {
            await call("window.clubmate.track(y)", ["y": start ?? NSNull()])
        }
    }

    private func installTheme(_ theme: DocumentTheme) {
        let css = String(data: (try? JSONEncoder().encode(theme.css)) ?? Data(), encoding: .utf8) ?? "''"
        let controller = webView.configuration.userContentController
        controller.removeAllUserScripts()
        installFind()
        controller.addUserScript(WKUserScript(
            source: "const viewport = document.createElement('meta'); viewport.name = 'viewport'; viewport.content = 'width=device-width, initial-scale=1'; const style = document.createElement('style'); style.id = 'clubmate-theme'; style.textContent = \(css); document.documentElement.append(viewport, style)",
            injectionTime: .atDocumentStart, forMainFrameOnly: true, in: .defaultClient
        ))
    }

    private func setTheme(_ theme: DocumentTheme) async {
        switch kind {
        case .markdown:
            await call("return await window.clubmate.setTheme(css)", ["css": theme.css])
        case .artifact:
            installTheme(theme)
            await call("const style = document.getElementById('clubmate-theme'); if (style) style.textContent = css", ["css": theme.css], in: .defaultClient)
        }
    }

    private func scroll(to anchor: String?) async {
        guard let anchor else { return }
        switch kind {
        case .markdown:
            await call("return window.clubmate.scrollTo(anchor)", ["anchor": anchor])
        case .artifact:
            await call("document.getElementById(anchor)?.scrollIntoView()", ["anchor": anchor], in: .defaultClient)
        }
    }

    private func call(_ script: String, _ arguments: [String: Any], in world: WKContentWorld = .page) async {
        _ = try? await webView.callAsyncJavaScript(script, arguments: arguments, contentWorld: world)
    }

    private func finishLoading() {
        loading?.resume()
        loading = nil
    }

    fileprivate func receive(_ name: String, _ body: Any) {
        if name == "clubmateTap" {
            onTextTap?()
            return
        }
        guard let message = body as? [String: Any] else { return }
        switch message["type"] as? String {
        case "height":
            if let value = message["value"] as? Double { onHeight(CGFloat(value)) }
        case "scroll":
            if let value = message["value"] as? Double { onPlace?(value) }
        case "lightbox":
            let open = message["open"] as? Bool == true
            zoom?.lightbox = open
            if open { resetMagnification() }
            updateMagnification()
        default:
            break
        }
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction) async -> WKNavigationActionPolicy {
        guard let url = navigationAction.request.url else { return .cancel }
        if kind == .artifact { return route(navigationAction, url) }
        if url.scheme == DocumentScheme.name, url.host() == DocumentScheme.base.host(), url.path == "/" {
            return navigationAction.navigationType == .other || url.fragment != nil ? .allow : .cancel
        }
        if navigationAction.navigationType == .linkActivated {
            onLink(DocumentLink(url))
        }
        return .cancel
    }

    private func route(_ action: WKNavigationAction, _ url: URL) -> WKNavigationActionPolicy {
        let main = action.targetFrame?.isMainFrame == true
        if main, url == artifactLoad {
            artifactLoad = nil
            return .allow
        }
        if main, artifact.isPage(url), url.fragment != nil {
            return .allow
        }
        if action.navigationType == .linkActivated, artifact.owns(url) {
            onLink(.doc(path: url.path, anchor: url.fragment))
        }
        return .cancel
    }

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if kind != .artifact, let url = navigationAction.request.url, case .external = DocumentLink(url) {
            onLink(.external(url))
        }
        return nil
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        warmed = true
        finishLoading()
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        finishLoading()
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        finishLoading()
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        if Self.spare === self { Self.spare = nil }
        finishLoading()
        shown = nil
        if work == nil, wanted != nil {
            work = Task { await drain() }
        }
    }
}

@MainActor
private final class MessageProxy: NSObject, WKScriptMessageHandler {
    weak var target: DocumentController?

    init(_ target: DocumentController) {
        self.target = target
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        target?.receive(message.name, message.body)
    }
}
