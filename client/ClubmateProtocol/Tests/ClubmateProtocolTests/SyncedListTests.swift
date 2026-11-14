import Foundation
import XCTest

import ClubmateProtocol

final class SyncedListTests: XCTestCase {
    private struct Row: SyncedItem, Equatable {
        static let field = "rows"
        let id: Int
        let title: String
    }

    private func reply(_ json: String) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
    }

    func testApplyDeltaKeepsOrder() throws {
        var list = SyncedList<Row>()
        try list.apply(reply(#"{"rows":[{"id":3,"title":"c"},{"id":1,"title":"a"},{"id":2,"title":"b"}],"removed":[],"order":[1,2,3],"cursor":"e.4","delta":false}"#))
        XCTAssertEqual(list.items.map(\.id), [1, 2, 3])

        try list.apply(reply(#"{"rows":[{"id":2,"title":"b2"}],"removed":[1],"cursor":"e.5","delta":true}"#))
        XCTAssertEqual(list.items, [Row(id: 2, title: "b2"), Row(id: 3, title: "c")])

        try list.apply(reply(#"{"rows":[{"id":4,"title":"d"}],"removed":[],"order":[4,3,2],"cursor":"e.6","delta":true}"#))
        XCTAssertEqual(list.items.map(\.id), [4, 3, 2])
        XCTAssertEqual(list.row(3)?["title"], .string("c"))
        XCTAssertEqual(list.args, .object(["since": .string("e.6")]))
    }

    func testNewerCursorKeepsTheCurrentEpoch() {
        XCTAssertEqual(SyncCursor.newer("e.11", current: "e.20"), "e.20")
        XCTAssertEqual(SyncCursor.newer("e.21", current: "e.20"), "e.21")
        XCTAssertEqual(SyncCursor.newer("old.99", current: "new.2"), "new.2")
    }

    func testFullReplyReplaces() throws {
        var list = try SyncedList<Row>(rows: [reply(#"{"id":1,"title":"a"}"#), reply(#"{"id":2,"title":"b"}"#)], cursor: "old.9")
        try list.apply(reply(#"{"rows":[{"id":5,"title":"e"}],"removed":[],"cursor":"new.1","delta":false}"#))
        XCTAssertEqual(list.items, [Row(id: 5, title: "e")])
        XCTAssertEqual(list.cursor, "new.1")

        let saved = try JSONDecoder().decode(SyncedList<Row>.self, from: JSONEncoder().encode(list))
        XCTAssertEqual(saved.items, list.items)
        XCTAssertEqual(saved.cursor, "new.1")
        XCTAssertThrowsError(try list.apply(reply(#"{"rows":[]}"#)))
    }
}
