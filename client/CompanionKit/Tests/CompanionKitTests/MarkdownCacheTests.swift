import XCTest

import CompanionKit

final class MarkdownCacheTests: XCTestCase {
    func testCachedParseKeysOnTextAndLinks() {
        let text = "See `docs/plan.md` and #12 ↩"
        let docs = ["docs/plan.md": "docs/plan.md"]
        let tasks = ["12": "12"]
        XCTAssertEqual(MarkdownCache.blocks(text), Markdown.blocks(text))
        XCTAssertEqual(MarkdownCache.inline(text), Markdown.inline(text))
        XCTAssertEqual(MarkdownCache.inline(text, docLinks: docs, taskLinks: tasks), Markdown.inline(text, docLinks: docs, taskLinks: tasks))
        XCTAssertNotEqual(MarkdownCache.inline(text, docLinks: docs, taskLinks: tasks), MarkdownCache.inline(text))
    }
}
