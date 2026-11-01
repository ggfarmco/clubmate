import Foundation
import WebKit

@MainActor
final class DocumentController: NSObject, WKNavigationDelegate {
    private struct Request: Equatable {
        var content: DocumentContent
        var theme: DocumentTheme
        var size: DocumentSize
    }

    let webView: WKWebView
    var onLink: (DocumentLink) -> Void = { _ in }
    var onHeight: (CGFloat) -> Void = { _ in }

    private var wanted: Request?
    private var shown: Request?
    private var work: Task<Void, Never>?
    private var loading: CheckedContinuation<Void, Never>?

    override init() {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        configuration.setURLSchemeHandler(DocumentScheme(), forURLScheme: DocumentScheme.name)
        webView = WKWebView(frame: .zero, configuration: configuration)
        super.init()
        configuration.userContentController.add(MessageProxy(self), name: "clubmate")
        webView.navigationDelegate = self
        webView.allowsLinkPreview = false
        webView.allowsBackForwardNavigationGestures = false
        #if os(macOS)
        webView.setValue(false, forKey: "drawsBackground")
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
        webView.configuration.userContentController.removeScriptMessageHandler(forName: "clubmate")
        webView.navigationDelegate = nil
        loading?.resume()
        loading = nil
    }

    private func drain() async {
        while let next = wanted, next != shown {
            let previous = shown
            shown = next
            if previous == nil || previous?.size != next.size || DocumentPage.update(old: previous?.content, new: next.content) == .load {
                await load(next)
                continue
            }
            if previous?.theme != next.theme {
                await call("return await window.clubmate.setTheme(css)", ["css": next.theme.css])
            }
            if DocumentPage.update(old: previous?.content, new: next.content) == .replaceBody {
                await call("return await window.clubmate.setBody(html)", ["html": next.content.html])
            }
        }
        work = nil
    }

    private func load(_ request: Request) async {
        await withCheckedContinuation { continuation in
            loading?.resume()
            loading = continuation
            webView.loadHTMLString(DocumentPage.html(request.content, theme: request.theme, size: request.size), baseURL: DocumentScheme.base)
        }
        await call("return await window.clubmate.ready", [:])
    }

    private func call(_ script: String, _ arguments: [String: Any]) async {
        _ = try? await webView.callAsyncJavaScript(script, arguments: arguments, contentWorld: .page)
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
        if url.scheme == DocumentScheme.name, url.host() == DocumentScheme.base.host(), url.path == "/" {
            return navigationAction.navigationType == .other || url.fragment != nil ? .allow : .cancel
        }
        if navigationAction.navigationType == .linkActivated {
            onLink(DocumentLink(url))
        }
        return .cancel
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
