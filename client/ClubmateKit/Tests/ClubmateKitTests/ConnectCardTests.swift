import XCTest

import ClubmateProtocol
@testable import ClubmateKit

@MainActor
final class ConnectCardTests: XCTestCase {
    private let repos = [
        SDUIConnectRepo(key: "/a", name: "a"),
        SDUIConnectRepo(key: "/b", name: "b"),
        SDUIConnectRepo(key: "https://x/c.git", name: "c", clone: true, folded: true),
    ]

    func testUntickedRepoIsSkippedAndOtherFolderIsSent() {
        let model = ConnectCardModel()
        model.toggle(repos)
        model.choose(other: "/elsewhere", for: "/b")

        XCTAssertEqual(model.ticked(repos), 2)
        XCTAssertEqual(model.answers(repos), [
            .object(["path": .string("/a"), "action": .string("skip")]),
            .object(["path": .string("/b"), "action": .string("other"), "otherPath": .string("/elsewhere")]),
            .object(["path": .string("https://x/c.git"), "action": .string("connect")]),
        ])
    }

    func testMovingOntoAFoldedRepoOpensTheFold() {
        let model = ConnectCardModel()
        XCTAssertEqual(model.visible(repos).count, 2)

        model.move(1, in: repos)
        XCTAssertFalse(model.unfolded)
        model.move(1, in: repos)
        model.move(1, in: repos)

        XCTAssertEqual(model.cursor, 2)
        XCTAssertEqual(model.visible(repos).count, 3)
    }

    func testChoosingAnotherFolderTicksTheRepo() {
        let model = ConnectCardModel()
        model.toggle(repos)
        model.choose(other: "/elsewhere", for: "/a")

        XCTAssertEqual(model.ticked(repos), 3)
    }

    func testInlineSpansKeepTextThatIsNotMarkup() {
        let spans = CardInline.spans("needs `a:b` in **ads** at 2*3 [x](http://y) `open")

        XCTAssertEqual(spans.map(\.text), ["needs ", "a:b", " in ", "ads", " at 2*3 [x](http://y) `open"])
        XCTAssertEqual(spans.map(\.style), [.plain, .code, .plain, .strong, .plain])
    }
}
