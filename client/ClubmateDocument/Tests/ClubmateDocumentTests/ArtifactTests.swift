import Foundation
import WebKit
import XCTest

@testable import ClubmateDocument

@MainActor
final class ArtifactTests: XCTestCase {
    private func open(_ html: String, onLink: @escaping (DocumentLink) -> Void = { _ in }) async -> DocumentController {
        let page = DocumentController(kind: .artifact)
        page.webView.frame = CGRect(x: 0, y: 0, width: 400, height: 300)
        page.onLink = onLink
        page.show(DocumentContent(path: "/r/out/report.html", html: html, rev: 1, kind: .artifact), theme: .mate(dark: true), size: .fill)
        await page.settled()
        return page
    }

    private func run(_ page: DocumentController, _ script: String) async throws -> Any? {
        try await page.webView.callAsyncJavaScript(script, contentWorld: .page)
    }

    func testArtifactRunsScript() async throws {
        let page = await open("<html><head><script>document.title = 'ran ' + (1 + 1)</script></head><body></body></html>")
        let title = try await run(page, "return document.title") as? String
        XCTAssertEqual(title, "ran 2")
    }

    func testArtifactCannotFetch() async throws {
        let page = await open("""
            <html><head><script>window.violations = []; document.addEventListener('securitypolicyviolation', e => window.violations.push(e.violatedDirective))</script>
            <meta http-equiv="Content-Security-Policy" content="default-src *">
            </head><body><iframe src="https://example.com"></iframe><p id="alive">alive</p></body></html>
            """)
        let fetched = try await run(page, "try { await fetch('https://example.com'); return 'loaded' } catch (e) { return 'blocked' }") as? String
        XCTAssertEqual(fetched, "blocked")
        let state = try await run(page, "return [window.violations.join(' '), document.getElementById('alive').textContent]") as? [String]
        XCTAssertTrue(state?.first?.contains("frame-src") == true, "\(state ?? [])")
        XCTAssertTrue(state?.first?.contains("connect-src") == true, "\(state ?? [])")
        XCTAssertEqual(state?.last, "alive")
    }

    func testArtifactHasNoWebRTC() async throws {
        let page = await open("<html><body>x</body></html>")
        let types = try await run(page, "const frame = document.body.appendChild(document.createElement('iframe')).contentWindow; return [typeof RTCPeerConnection, typeof frame.RTCPeerConnection]") as? [String]
        XCTAssertEqual(types, ["undefined", "undefined"])
    }

    func testArtifactNavigationIsRouted() async throws {
        var links: [DocumentLink] = []
        let page = await open("""
            <html><body><a id="b" href="b.md#x">b</a><p id="alive">alive</p>
            <script>setTimeout(() => { window.open('https://example.com'); location = 'https://example.com' }, 0)</script>
            </body></html>
            """) { links.append($0) }
        try await Task.sleep(for: .milliseconds(300))
        let alive = try await run(page, "return document.getElementById('alive').textContent") as? String
        XCTAssertEqual(alive, "alive")
        XCTAssertEqual(links, [])
        _ = try await run(page, "document.getElementById('b').click()")
        try await Task.sleep(for: .milliseconds(300))
        XCTAssertEqual(links, [.doc(path: "/r/out/b.md", anchor: "x")])
    }

    func testArtifactThemeVariablesAndViewport() async throws {
        let page = await open("<html><body>x</body></html>")
        let state = try await run(page, "return [getComputedStyle(document.documentElement).getPropertyValue('--bg').trim(), document.querySelector('meta[name=viewport]')?.content ?? '']") as? [String]
        XCTAssertEqual(state, [DocumentTheme.mate(dark: true).background, "width=device-width, initial-scale=1"])
    }
}
