import XCTest

import ClubmateKit

final class WorkspaceUIStoreTests: XCTestCase {
    func testReviewSlotSelectsReviewTab() {
        let tab = CodeTab.chat.following(review: false, to: true)
        XCTAssertEqual(tab, .review)
        XCTAssertEqual(CodeSection.columnSlot(tab: tab, review: true, chat: true), CodeSection.reviewSlot)
        XCTAssertEqual(CodeTab.chat.following(review: true, to: true), .chat)
    }

    func testReviewSlotGoneFallsBackToChat() {
        let tab = CodeTab.review.following(review: true, to: false)
        XCTAssertEqual(tab, .chat)
        XCTAssertEqual(CodeSection.columnSlot(tab: tab, review: false, chat: true), CodeSection.chatSlot)
        XCTAssertNil(CodeSection.columnSlot(tab: tab, review: false, chat: false))
    }

    func testSwitchCodeTabTogglesBetweenSlots() {
        let chat = CodeTab.review.switched(review: true, chat: true)
        XCTAssertEqual(CodeSection.columnSlot(tab: chat, review: true, chat: true), CodeSection.chatSlot)
        XCTAssertEqual(chat.switched(review: true, chat: true), .review)
        XCTAssertEqual(CodeTab.review.switched(review: true, chat: false), .review)
        XCTAssertEqual(CodeSection.columnSlot(tab: .chat, review: true, chat: false), CodeSection.reviewSlot)
    }

    func testReviewNarrowThreshold() {
        XCTAssertEqual([899, 900, 0].map(CodeSection.reviewNarrow), [true, false, false])
    }

    func testReviewColumnAtDefaultWidthKeeps1280Wide() {
        let column = CodeSection.columnWidth(saved: nil, review: true, limit: 640)
        XCTAssertFalse(CodeSection.reviewNarrow(width: CodeSection.editorWidth(row: 1280, rail: 40, column: column)))
        let chat = CodeSection.columnWidth(saved: nil, review: false, limit: 640)
        XCTAssertTrue(CodeSection.reviewNarrow(width: CodeSection.editorWidth(row: 1280, rail: 40, column: chat)))
    }
}
