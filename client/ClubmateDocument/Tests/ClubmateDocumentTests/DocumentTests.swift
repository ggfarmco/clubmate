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

    func testMermaidErrorKeepsSource() async throws {
        let page = DocumentController()
        page.webView.frame = CGRect(x: 0, y: 0, width: 400, height: 300)
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
            ]
            """, contentWorld: .page) as? [Any]
        XCTAssertEqual(state?[0] as? String, "object")
        XCTAssertEqual(state?[1] as? Int, 1)
        XCTAssertEqual(state?[2] as? String, "flowchart LR\n  A -->")
        XCTAssertTrue((state?[3] as? String ?? "").contains("Parse error"), "\(state?[3] ?? "")")
        XCTAssertEqual(state?[4] as? String, "after")
    }
}
