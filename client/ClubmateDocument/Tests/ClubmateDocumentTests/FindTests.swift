import Foundation
import WebKit
import XCTest

@testable import ClubmateDocument

@MainActor
final class FindTests: XCTestCase {
    private func open(_ html: String, kind: DocumentKind = .markdown, finder: DocumentFind? = nil) async -> DocumentController {
        let page = DocumentController(kind: kind)
        page.webView.frame = CGRect(x: 0, y: 0, width: 400, height: 300)
        page.finder = finder
        page.show(DocumentContent(path: "/r/a.\(kind == .artifact ? "html" : "md")", html: html, rev: 1, kind: kind), theme: .mate(dark: true), size: .fill)
        await page.settled()
        return page
    }

    func testMatchesSpanInlineNodesNotBlocksAndStepWraps() async throws {
        let page = await open("<p>Alpha <b>be</b>ta</p><p>alpha</p><p>be</p><p>ta</p><p hidden>beta</p>")
        let beta = await page.find(.find("BETA"))
        XCTAssertEqual(beta?.total, 1)
        let alpha = await page.find(.find("alpha"))
        XCTAssertEqual(alpha, FindResult(query: "alpha", total: 2, current: 1, more: false))
        var steps: [Int] = []
        for delta in [1, 1, -1] {
            steps.append(await page.find(.step(delta))?.current ?? 0)
        }
        XCTAssertEqual(steps, [2, 1, 2])
    }

    func testArtifactFindIsHiddenFromThePageButPaints() async throws {
        let page = await open("<html><body><p>needle</p><script>document.body.append('needle')</script></body></html>", kind: .artifact)
        let result = await page.find(.find("needle"))
        XCTAssertEqual(result?.total, 2)
        let seen = try await page.webView.callAsyncJavaScript("return [typeof window.clubmateFind, CSS.highlights.has('clubmate-find')]", contentWorld: .page) as? [Any]
        XCTAssertEqual(seen?.first as? String, "undefined")
        XCTAssertEqual(seen?.last as? Bool, true)
    }

    func testLiveReloadRefreshesTheCount() async throws {
        let finder = DocumentFind()
        finder.show()
        let page = await open("<p>one x</p>", finder: finder)
        await finder.search("x")
        XCTAssertEqual(finder.total, 1)
        page.show(DocumentContent(path: "/r/a.md", html: "<p>one x</p><p>two x</p>", rev: 2), theme: .mate(dark: true), size: .fill)
        await page.settled()
        XCTAssertEqual(finder.total, 2)
        XCTAssertEqual(finder.current, 1)
    }

    func testWebPageFinderFollowsNavigationAndClearsAfterRelease() async throws {
        let webView = WKWebView(frame: CGRect(x: 0, y: 0, width: 400, height: 300))
        let loaded = Loaded()
        webView.navigationDelegate = loaded
        func load(_ html: String) async {
            await withCheckedContinuation { continuation in
                loaded.done = continuation
                webView.loadHTMLString(html, baseURL: nil)
            }
        }
        await load("<p>one x</p><p>two x</p>")
        let find = DocumentFind()
        var finder: WebPageFinder? = WebPageFinder(webView: webView, find: find)
        find.show()
        await find.search("x")
        XCTAssertEqual(find.total, 2)
        await find.next()
        XCTAssertEqual(find.current, 2)
        let hidden = try await webView.callAsyncJavaScript("return typeof window.clubmateFind", contentWorld: .page) as? String
        XCTAssertEqual(hidden, "undefined")

        await load("<p>x</p><p>x</p><p>x</p>")
        try await waitUntil { find.total == 3 }

        find.hide()
        finder = nil
        try await waitUntil {
            try await webView.callAsyncJavaScript("return CSS.highlights.has('clubmate-find')", contentWorld: .defaultClient) as? Bool == false
        }
        XCTAssertNil(finder)
    }
}

private struct TimedOut: Error {}

@MainActor
private func waitUntil(_ condition: () async throws -> Bool) async throws {
    let scale = Double(ProcessInfo.processInfo.environment["TEST_TIMEOUT_SCALE"] ?? "").flatMap { $0 > 0 ? $0 : nil } ?? 3
    let deadline = ContinuousClock.now + .seconds(5 * scale)
    while try await !condition() {
        guard ContinuousClock.now < deadline else { throw TimedOut() }
        try await Task.sleep(for: .milliseconds(20))
    }
}

@MainActor
private final class Loaded: NSObject, WKNavigationDelegate {
    var done: CheckedContinuation<Void, Never>?

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        done?.resume()
        done = nil
    }
}
