import Foundation
import WebKit

@MainActor
final class DocumentController: NSObject, WKNavigationDelegate, WKUIDelegate {
    private struct Request: Equatable {
        var content: DocumentContent
        var theme: DocumentTheme
        var size: DocumentSize
    }

    let webView: WKWebView
    let kind: DocumentKind
    var onLink: (DocumentLink) -> Void = { _ in }
    var onHeight: (CGFloat) -> Void = { _ in }

    private var wanted: Request?
    private var shown: Request?
    private var work: Task<Void, Never>?
    private var loading: CheckedContinuation<Void, Never>?
    private let artifact = ArtifactScheme()
    private var artifactLoad: URL?

    init(kind: DocumentKind = .markdown) {
        self.kind = kind
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        switch kind {
        case .markdown:
            configuration.setURLSchemeHandler(DocumentScheme(), forURLScheme: DocumentScheme.name)
        case .artifact:
            configuration.setURLSchemeHandler(artifact, forURLScheme: ArtifactScheme.name)
            configuration.preferences.javaScriptCanOpenWindowsAutomatically = false
            if configuration.preferences.responds(to: NSSelectorFromString("_setPeerConnectionEnabled:")) {
                configuration.preferences.setValue(false, forKey: "peerConnectionEnabled")
            }
        }
        webView = WKWebView(frame: .zero, configuration: configuration)
        super.init()
        switch kind {
        case .markdown:
            configuration.userContentController.add(MessageProxy(self), name: "clubmate")
        case .artifact:
            webView.uiDelegate = self
        }
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
        webView.isOpaque = false
        webView.backgroundColor = .clear
        webView.scrollView.backgroundColor = .clear
        #endif
    }

    func show(_ content: DocumentContent, theme: DocumentTheme, size: DocumentSize) {
        wanted = Request(content: content, theme: theme, size: size)
        #if os(iOS)
        webView.scrollView.isScrollEnabled = size == .fill
        #endif
        guard work == nil else { return }
        work = Task { await drain() }
    }

    func settled() async {
        await work?.value
    }

    func close() {
        if kind == .markdown {
            webView.configuration.userContentController.removeScriptMessageHandler(forName: "clubmate")
        }
        webView.navigationDelegate = nil
        webView.uiDelegate = nil
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
                continue
            }
            if previous?.theme != next.theme {
                await setTheme(next.theme)
            }
            switch change {
            case .replaceBody:
                await call("return await window.clubmate.setBody(html)", ["html": next.content.html])
            case .scroll:
                await scroll(to: next.content.anchor)
            case .load, .none:
                break
            }
        }
        work = nil
    }

    private func load(_ request: Request) async {
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
        await scroll(to: request.content.anchor)
    }

    private func installTheme(_ theme: DocumentTheme) {
        let css = String(data: (try? JSONEncoder().encode(theme.css)) ?? Data(), encoding: .utf8) ?? "''"
        let controller = webView.configuration.userContentController
        controller.removeAllUserScripts()
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

    fileprivate func receive(_ body: Any) {
        guard let message = body as? [String: Any], message["type"] as? String == "height", let value = message["value"] as? Double else { return }
        onHeight(CGFloat(value))
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
        finishLoading()
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        finishLoading()
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        finishLoading()
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
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
        target?.receive(message.body)
    }
}
