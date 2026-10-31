import AppKit
import Foundation
import WebKit

@MainActor
public final class BrowserTab: NSObject {
    public let webView: WKWebView
    private let navigationTimeout: Duration
    private var pending: WKNavigation?
    private var starts = 0
    private var status = 0
    private var responded = false
    private var provisional = false
    private var nextRef = 0

    public init(configuration: WKWebViewConfiguration, userAgent: String, navigationTimeout: Duration = .seconds(30)) {
        webView = WKWebView(frame: CGRect(x: 0, y: 0, width: 1280, height: 900), configuration: configuration)
        webView.customUserAgent = userAgent
        self.navigationTimeout = navigationTimeout
        super.init()
        webView.navigationDelegate = self
        webView.uiDelegate = self
    }

    public func open(_ url: URL) async -> PageSummary {
        await navigate { pending = webView.load(URLRequest(url: url)) }
    }

    public func back() async -> PageSummary {
        await navigate {
            if webView.canGoBack { pending = webView.goBack() }
        }
    }

    public func snapshot() async throws -> (url: String, title: String, text: String) {
        let snapshot = try BrowserScript.decode(BrowserSnapshot.self, from: try await run(BrowserScript.snapshot, ["next": nextRef]))
        nextRef = snapshot.next
        return (webView.url?.absoluteString ?? "", webView.title ?? "", snapshot.text)
    }

    public func click(ref: String) async throws -> PageSummary {
        try await act(BrowserScript.click, ref: ref, ["ref": ref])
    }

    public func type(ref: String, text: String, submit: Bool) async throws -> PageSummary {
        try await act(BrowserScript.type, ref: ref, ["ref": ref, "text": text, "submit": submit])
    }

    public func select(ref: String, option: String) async throws -> PageSummary {
        try await act(BrowserScript.select, ref: ref, ["ref": ref, "option": option])
    }

    public func press(key: String) async throws -> PageSummary {
        try await act(BrowserScript.press, ref: nil, ["key": key])
    }

    public func scroll(direction: String?, ref: String?) async throws -> PageSummary {
        try await act(BrowserScript.scroll, ref: ref, ["direction": direction ?? "", "ref": ref ?? ""])
    }

    public func screenshot(to directory: URL) async throws -> URL {
        let png: Data = try await bounded { finish in
            webView.takeSnapshot(with: nil) { image, _ in
                guard let cgImage = image?.cgImage(forProposedRect: nil, context: nil, hints: nil),
                      let png = NSBitmapImageRep(cgImage: cgImage).representation(using: .png, properties: [:])
                else { return finish(.failure(BrowserError(message: "screenshot failed"))) }
                finish(.success(png))
            }
        }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let file = directory.appending(path: "browser-\(UUID().uuidString).png")
        try png.write(to: file)
        return file
    }

    private func act(_ script: String, ref: String?, _ arguments: [String: Any]) async throws -> PageSummary {
        let (before, deadline) = begin()
        do {
            let outcome = try BrowserScript.decode(BrowserActionOutcome.self, from: try await run(script, arguments))
            if outcome.missing == true { throw BrowserError(message: "ref \(ref ?? "") not found; take a new browser_snapshot") }
            if let message = outcome.error { throw BrowserError(message: message) }
        } catch let error as BrowserError {
            throw error
        } catch {
            if starts == before { throw BrowserError(message: error.localizedDescription) }
        }
        return await complete(since: before, deadline: deadline)
    }

    private func navigate(_ start: () -> Void) async -> PageSummary {
        let (before, deadline) = begin()
        start()
        return await complete(since: before, deadline: deadline)
    }

    private func begin() -> (Int, ContinuousClock.Instant) {
        (starts, ContinuousClock.now + navigationTimeout)
    }

    private func complete(since before: Int, deadline: ContinuousClock.Instant) async -> PageSummary {
        await settle(since: before, deadline: deadline)
        return await summary()
    }

    private func settle(since before: Int, deadline: ContinuousClock.Instant) async {
        let quiet = ContinuousClock.now + .seconds(1)
        while ContinuousClock.now < deadline {
            if pending == nil, starts != before || ContinuousClock.now >= quiet { return }
            try? await Task.sleep(for: .milliseconds(20))
        }
        if pending != nil {
            if provisional { status = 0 }
            pending = nil
            webView.stopLoading()
        }
    }

    private func summary() async -> PageSummary {
        let html = (try? await run(BrowserScript.html, [:], within: min(navigationTimeout, .seconds(5)))) ?? ""
        let title = webView.title ?? ""
        return PageSummary(
            url: webView.url?.absoluteString ?? "",
            status: status,
            title: title,
            blocked: BlockDetector.blocked(status: status, title: title, html: html)
        )
    }

    private func run(_ script: String, _ arguments: [String: Any], within limit: Duration? = nil) async throws -> String {
        try await bounded(within: limit) { finish in
            webView.callAsyncJavaScript(script, arguments: arguments, in: nil, in: .defaultClient) { result in
                finish(result.map { $0 as? String ?? "" })
            }
        }
    }

    private func bounded<Value: Sendable>(within limit: Duration? = nil, _ start: (@escaping @MainActor (Result<Value, Error>) -> Void) -> Void) async throws -> Value {
        let call = ScriptCall<Value>()
        let limit = limit ?? navigationTimeout
        return try await withCheckedThrowingContinuation { continuation in
            call.continuation = continuation
            call.timer = Task {
                try? await Task.sleep(for: limit)
                call.finish(.failure(BrowserError(message: "page did not respond")))
            }
            start { call.finish($0) }
        }
    }

    private static func loadable(_ url: URL?) -> Bool {
        guard let url else { return false }
        return ["http", "https"].contains(url.scheme?.lowercased()) || url.absoluteString == "about:blank"
    }

    private func finished(_ navigation: WKNavigation?) {
        if navigation === pending { pending = nil }
    }
}

extension BrowserTab: WKNavigationDelegate {
    public func webView(_ webView: WKWebView, didStartProvisionalNavigation navigation: WKNavigation!) {
        starts += 1
        pending = navigation
        provisional = true
        responded = false
    }

    public func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction) async -> WKNavigationActionPolicy {
        if navigationAction.targetFrame?.isMainFrame == false { return .allow }
        return Self.loadable(navigationAction.request.url) ? .allow : .cancel
    }

    public func webView(_ webView: WKWebView, decidePolicyFor navigationResponse: WKNavigationResponse) async -> WKNavigationResponsePolicy {
        if navigationResponse.isForMainFrame {
            status = (navigationResponse.response as? HTTPURLResponse)?.statusCode ?? 0
            responded = true
        }
        return .allow
    }

    public func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
        provisional = false
        if !responded { status = 0 }
    }

    public func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        finished(navigation)
    }

    public func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        finished(navigation)
    }

    public func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        if navigation === pending {
            provisional = false
            status = 0
        }
        finished(navigation)
    }
}

extension BrowserTab: WKUIDelegate {
    public func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if Self.loadable(navigationAction.request.url) { webView.load(navigationAction.request) }
        return nil
    }
}

@MainActor
private final class ScriptCall<Value: Sendable> {
    var continuation: CheckedContinuation<Value, Error>?
    var timer: Task<Void, Never>?

    func finish(_ result: Result<Value, Error>) {
        timer?.cancel()
        continuation?.resume(with: result)
        continuation = nil
    }
}
