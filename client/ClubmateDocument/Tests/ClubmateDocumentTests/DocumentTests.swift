import Foundation
import WebKit
import XCTest

@testable import ClubmateDocument

@MainActor
final class DocumentTests: XCTestCase {
    func testLinkRouting() throws {
        let cases: [(String, DocumentLink)] = [
            ("clubmate-doc:/r/a.md#x", .doc(path: "/r/a.md", anchor: "x")),
            ("clubmate-doc:/r/my%20plan.md", .doc(path: "/r/my plan.md", anchor: nil)),
            ("clubmate-file:/r/m.go", .file(path: "/r/m.go")),
            ("https://e.com", .external(URL(string: "https://e.com")!)),
            ("#sec", .anchor("sec")),
            ("file:///etc/passwd", .ignore),
            ("javascript:alert(1)", .ignore),
        ]
        for (raw, want) in cases {
            XCTAssertEqual(DocumentLink(try XCTUnwrap(URL(string: raw))), want, raw)
        }
    }

    func testUpdate() {
        let a1 = DocumentContent(path: "/r/a.md", html: "<p>1</p>", rev: 1)
        let a2 = DocumentContent(path: "/r/a.md", html: "<p>2</p>", rev: 2)
        let b1 = DocumentContent(path: "/r/b.md", html: "<p>1</p>", rev: 1)
        XCTAssertEqual(DocumentPage.update(old: nil, new: a1), .load)
        XCTAssertEqual(DocumentPage.update(old: a1, new: a2), .replaceBody)
        XCTAssertEqual(DocumentPage.update(old: a1, new: a1), .none)
        XCTAssertEqual(DocumentPage.update(old: a1, new: b1), .load)
        let a1Intro = DocumentContent(path: "/r/a.md", html: "<p>1</p>", rev: 1, anchor: "intro")
        XCTAssertEqual(DocumentPage.update(old: a1, new: a1Intro), .scroll)
        XCTAssertEqual(DocumentPage.update(old: a1Intro, new: a1), .none)
        let r1 = DocumentContent(path: "/r/r.html", html: "<p>1</p>", rev: 1, kind: .artifact)
        let r2 = DocumentContent(path: "/r/r.html", html: "<p>2</p>", rev: 2, kind: .artifact)
        XCTAssertEqual(DocumentPage.update(old: r1, new: r2), .load)
        XCTAssertEqual(DocumentPage.update(old: r1, new: r1), .none)
    }

    func testLoadScrollsToAnchorWhenItExists() async throws {
        let long = (1...200).map { "<p>line \($0)</p>" }.joined()
        for (anchor, scrolled) in [("sec2", true), ("missing", false)] {
            let page = DocumentController()
            page.webView.frame = CGRect(x: 0, y: 0, width: 400, height: 300)
            page.show(DocumentContent(path: "/r/a.md", html: long + "<h2 id=\"sec2\">Two</h2>" + long, rev: 1, anchor: anchor), theme: .mate(dark: true), size: .fill)
            await page.settled()
            let y = try await page.webView.callAsyncJavaScript("return window.scrollY", contentWorld: .page) as? Double ?? 0
            XCTAssertEqual(y > 0, scrolled, anchor)
        }
    }

    func testTemplateHasStrictCSPAndNoRemoteURL() throws {
        let html = try String(contentsOf: DocumentPage.web.appendingPathComponent("document.html"), encoding: .utf8)
        XCTAssertTrue(html.contains(#"<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src data:">"#))
        XCTAssertFalse(html.replacingOccurrences(of: "http-equiv", with: "").contains("http"))
    }

    func testColumnPlacesProseMarginAndCode() async throws {
        let long = String(repeating: "booking.roots2:30:17: error ", count: 40)
        let margin = "<aside class=\"wiki-margin\" id=\"aside\"><nav class=\"wm-sec\">On this page</nav></aside><details class=\"wiki-fold\" id=\"fold\"><summary>On this page</summary></details>"
        let html = margin + "<p id=\"p\">text</p><pre id=\"long\"><code>\(long)</code></pre><pre id=\"short\"><code>ok</code></pre>"
        let cases: [(width: Double, column: Double?, aside: Double?, left: Double, code: Double, asideLeft: Double?)] = [
            (1000, 780, 240, 110, 862, nil),
            (1210, 780, 240, 75, 780, 895),
            (1210, 780, nil, 215, 967, nil),
            (1000, nil, nil, 28, 704, 772),
        ]
        for c in cases {
            var theme = DocumentTheme.system(dark: false)
            theme.inset = 28
            theme.column = c.column
            theme.aside = c.aside
            let page = DocumentController()
            page.webView.frame = CGRect(x: 0, y: 0, width: c.width, height: 600)
            page.show(DocumentContent(path: "/r/a.md", html: html, rev: 1), theme: theme, size: .fill)
            await page.settled()
            let state = try await page.webView.callAsyncJavaScript("""
                const box = (id) => document.getElementById(id).getBoundingClientRect();
                const shown = (id) => getComputedStyle(document.getElementById(id)).display !== 'none';
                return [
                    document.documentElement.classList.contains('column'),
                    Math.round(box('p').left),
                    Math.round(box('long').left),
                    Math.round(box('long').width),
                    shown('aside') ? Math.round(box('aside').left) : -1,
                    shown('fold'),
                    document.getElementById('long').classList.contains('more'),
                    document.getElementById('short').classList.contains('more'),
                ]
                """, contentWorld: .page) as? [Any]
            let label = "\(c.width) column \(String(describing: c.column)) aside \(String(describing: c.aside))"
            XCTAssertEqual(state?[0] as? Bool, c.column != nil, label)
            XCTAssertEqual(state?[1] as? Double, c.left, label)
            XCTAssertEqual(state?[2] as? Double, c.left, label)
            XCTAssertEqual(state?[3] as? Double, c.code, label)
            XCTAssertEqual(state?[4] as? Double, c.asideLeft ?? -1, label)
            XCTAssertEqual(state?[5] as? Bool, c.asideLeft == nil, label)
            XCTAssertEqual(state?[6] as? Bool, true, label)
            XCTAssertEqual(state?[7] as? Bool, false, label)
        }
    }

    func testSamePathKeepsScroll() async throws {
        let page = DocumentController()
        page.webView.frame = CGRect(x: 0, y: 0, width: 400, height: 300)
        let long = (1...200).map { "<p>line \($0)</p>" }.joined()
        page.show(DocumentContent(path: "/r/a.md", html: long, rev: 1), theme: .mate(dark: true), size: .fill)
        await page.settled()
        _ = try await page.webView.evaluateJavaScript("window.scrollTo(0, 1500)")
        page.show(DocumentContent(path: "/r/a.md", html: "<h1>new</h1>" + long, rev: 2), theme: .mate(dark: true), size: .fill)
        await page.settled()
        let state = try await page.webView.callAsyncJavaScript("return [window.scrollY, document.querySelector('h1').textContent]", contentWorld: .page) as? [Any]
        XCTAssertEqual(state?.first as? Double, 1500)
        XCTAssertEqual(state?.last as? String, "new")
    }

    func testNewPageRestoresReportedPlaceOverAnchor() async throws {
        let long = (1...200).map { "<p>line \($0)</p>" }.joined()
        let content = DocumentContent(path: "/r/a.md", html: long + "<h2 id=\"sec2\">Two</h2>" + long, rev: 1, anchor: "sec2")
        let first = DocumentController()
        first.webView.frame = CGRect(x: 0, y: 0, width: 400, height: 300)
        let reported = expectation(description: "scroll reported")
        first.onPlace = { if $0 == 1500 { reported.fulfill() } }
        first.show(content, theme: .mate(dark: true), size: .fill)
        await first.settled()
        _ = try await first.webView.evaluateJavaScript("window.scrollTo(0, 1500)")
        await fulfillment(of: [reported], timeout: 10)
        first.close()

        let second = DocumentController()
        second.webView.frame = CGRect(x: 0, y: 0, width: 400, height: 300)
        second.onPlace = { _ in }
        second.restore(1500)
        second.show(content, theme: .mate(dark: true), size: .fill)
        await second.settled()
        let y = try await second.webView.callAsyncJavaScript("return window.scrollY", contentWorld: .page) as? Double
        XCTAssertEqual(y, 1500)
    }

    func testPlacesMatchPathAndAnchor() {
        let places = DocumentPlaces()
        let page = DocumentContent(path: "/r/a.md", html: "", rev: 1, anchor: "hit")
        places.save("w\nwiki.main", for: page, y: 900)
        XCTAssertEqual(places.place("w\nwiki.main", for: DocumentContent(path: "/r/a.md", html: "<p>new</p>", rev: 2, anchor: "hit")), 900)
        XCTAssertNil(places.place("w\nwiki.main", for: DocumentContent(path: "/r/a.md", html: "", rev: 1, anchor: "other")))
        XCTAssertNil(places.place("w\nwiki.main", for: DocumentContent(path: "/r/b.md", html: "", rev: 1, anchor: "hit")))
        XCTAssertNil(places.place("v\nwiki.main", for: page))
    }

    func testMermaidErrorKeepsSource() async throws {
        let page = DocumentController()
        page.webView.frame = CGRect(x: 0, y: 0, width: 1200, height: 300)
        let html = "<pre class=\"mermaid\">flowchart LR\n  A --&gt; B</pre><pre class=\"mermaid\">flowchart LR\n  A --&gt;</pre><p id=\"after\">after</p>"
        page.show(DocumentContent(path: "/r/a.md", html: html, rev: 1), theme: .mate(dark: false), size: .fill)
        await page.settled()
        let state = try await page.webView.callAsyncJavaScript("""
            return [
                typeof mermaid,
                document.querySelectorAll('.mermaid-diagram svg').length,
                document.querySelector('pre.mermaid-failed')?.textContent ?? '',
                document.querySelector('pre.mermaid-failed + .mermaid-error')?.textContent ?? '',
                document.getElementById('after')?.textContent ?? '',
                Math.round(document.querySelector('pre.mermaid-failed').getBoundingClientRect().right),
                Math.round(document.querySelector('.mermaid-error').getBoundingClientRect().right),
            ]
            """, contentWorld: .page) as? [Any]
        XCTAssertEqual(state?[0] as? String, "object")
        XCTAssertEqual(state?[1] as? Int, 1)
        XCTAssertEqual(state?[2] as? String, "flowchart LR\n  A -->")
        XCTAssertTrue((state?[3] as? String ?? "").contains("Parse error"), "\(state?[3] ?? "")")
        XCTAssertEqual(state?[4] as? String, "after")
        XCTAssertEqual(state?[5] as? Int, state?[6] as? Int)
    }

    func testZoomSteps() {
        XCTAssertEqual(DocumentZoom.step(1, .zoomIn), 1.25)
        XCTAssertEqual(DocumentZoom.step(DocumentZoom.step(1, .zoomOut), .zoomIn), 1)
        XCTAssertEqual((0..<10).reduce(1) { scale, _ in DocumentZoom.step(scale, .zoomIn) }, 3)
        XCTAssertEqual((0..<10).reduce(1) { scale, _ in DocumentZoom.step(scale, .zoomOut) }, 0.5)
        XCTAssertEqual(DocumentZoom.step(2, .fit), 1)
    }

    func testMermaidLightbox() async throws {
        let page = DocumentController()
        let zoom = DocumentZoom()
        page.zoom = zoom
        page.webView.frame = CGRect(x: 0, y: 0, width: 400, height: 300)
        page.show(DocumentContent(path: "/r/a.md", html: "<pre class=\"mermaid\">flowchart LR\n  A --&gt; B</pre>", rev: 1), theme: .mate(dark: true), size: .fill)
        await page.settled()
        let opened = try await page.webView.callAsyncJavaScript("""
            document.querySelector('.mermaid-diagram svg').dispatchEvent(new MouseEvent('click', { bubbles: true }));
            const box = document.querySelector('.lightbox');
            const scale = () => new DOMMatrix(box.querySelector('.lightbox-stage').style.transform).a;
            const before = scale();
            box.querySelector('button[aria-label="Zoom in"]').click();
            return [!box.hidden, box.querySelectorAll('svg').length, scale() / before];
            """, contentWorld: .page) as? [Any]
        XCTAssertEqual(opened?[0] as? Bool, true)
        XCTAssertEqual(opened?[1] as? Int, 1)
        XCTAssertEqual(opened?[2] as? Double ?? 0, 1.25, accuracy: 0.001)
        for _ in 0..<50 where !zoom.lightbox { try await Task.sleep(for: .milliseconds(10)) }
        XCTAssertTrue(zoom.lightbox)
        let closed = try await page.webView.callAsyncJavaScript("""
            document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
            return document.querySelector('.lightbox').hidden;
            """, contentWorld: .page) as? Bool
        XCTAssertEqual(closed, true)
        for _ in 0..<50 where zoom.lightbox { try await Task.sleep(for: .milliseconds(10)) }
        XCTAssertFalse(zoom.lightbox)
    }
}
