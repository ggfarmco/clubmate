import XCTest

import ClubmateProtocol
@testable import CompanionKit

final class DiffCacheTests: XCTestCase {
    func testCachedParseMatchesTheParserAndSumsChangesAcrossFiles() {
        let text = "--- a/x.txt\n+++ b/x.txt\n@@ -1,2 +1,2 @@\n one\n-two\n+three\n--- a/y.txt\n+++ b/y.txt\n@@ -1 +1,2 @@\n+four\n five\n"
        let parsed = DiffCache.parse(text)
        XCTAssertEqual(parsed.files, DiffFile.parse(text))
        XCTAssertEqual(parsed.changes.added, 2)
        XCTAssertEqual(parsed.changes.removed, 1)
        XCTAssertIdentical(DiffCache.parse(String(text.utf8)), parsed)
    }

    func testADiffStillInUseOutlivesAFullCache() {
        let head = "--- a/k.txt\n+++ b/k.txt\n@@ -1 +1 @@\n-kept\n+"
        let kept = DiffCache.parse(head + "kept\n")
        for i in 0..<DiffCache.limit {
            _ = DiffCache.parse(head + "other \(i)\n")
        }
        XCTAssertIdentical(DiffCache.parse(head + "kept\n"), kept)
    }
}
