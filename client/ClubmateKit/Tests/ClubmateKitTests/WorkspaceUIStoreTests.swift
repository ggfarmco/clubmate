import XCTest

import ClubmateKit

final class WorkspaceUIStoreTests: XCTestCase {
    func testReviewLayoutGoesInlineBelow900() {
        XCTAssertEqual([899, 900, 0].map(CodeSection.reviewLayout), ["inline", "split", "split"])
    }

    func testChatColumnAtDefaultWidthMakes1280Inline() {
        let chat = CodeSection.columnWidth(saved: nil, limit: 640)
        XCTAssertEqual(CodeSection.reviewLayout(width: CodeSection.editorWidth(row: 1280, rail: 40, column: chat)), "inline")
        XCTAssertEqual(CodeSection.reviewLayout(width: CodeSection.editorWidth(row: 1280, rail: 40, column: nil)), "split")
    }
}
