import Foundation
import XCTest

import CompanionKit

final class TranscriptCursorRowsTests: XCTestCase {
    private func rows() throws -> [TranscriptRow] {
        let tool = { (id: String) in #"{"kind":"tool","id":"\#(id)","turn":1,"tool":{"title":"Edit \#(id).go","kind":"edit","status":"completed"}}"# }
        let json = "[" + [
            #"{"kind":"user","id":"p1","turn":1,"text":"Fix it"}"#,
            #"{"kind":"thought","id":"th","turn":1,"text":"hmm"}"#,
            tool("t1"), tool("t2"), tool("t3"),
            #"{"kind":"agent","id":"a1","turn":1,"text":"Done."}"#,
        ].joined(separator: ",") + "]"
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        return SessionScreen(entries: entries, session: AgentSession(id: "s", status: .done)).rows
    }

    func testCompactGroupsFoldAndListedGroupsShowTheirCalls() throws {
        let rows = try rows()
        let closed = TranscriptCursorRows.blocks(rows, open: { _, initial in initial }, listed: nil, docked: true)
        XCTAssertEqual(closed.map(\.kind), [.prompt, .thought, .actions, .answer, .card])
        let group = closed[2].id
        let listed = TranscriptCursorRows.blocks(rows, open: { _, initial in initial }, listed: 2)
        XCTAssertEqual(listed.filter { $0.parent == group }.count, 2)
        XCTAssertEqual(listed.first { $0.id == group }?.folds, true)
        let open = TranscriptCursorRows.blocks(rows, open: { id, _ in id == group }, listed: nil)
        XCTAssertEqual(open.filter { $0.parent == group }.map(\.kind), [.action, .action, .action])
        XCTAssertEqual(TranscriptCursorRows.row(of: open[3].id, in: rows), group)
        XCTAssertEqual(TranscriptCursorRows.ids(rows), Set(open.map(\.id)))
    }

    func testCopyAndKeys() throws {
        let rows = try rows()
        let blocks = TranscriptCursorRows.blocks(rows, open: { _, initial in initial }, listed: nil)
        XCTAssertEqual(TranscriptCursorRows.markdown(blocks[3].id, in: rows), "Done.")
        XCTAssertEqual(TranscriptCursorRows.markdown(blocks[0].id, in: rows), "Fix it")
        XCTAssertEqual(TranscriptPress.resolve("j", shift: false), .step(.move(1)))
        XCTAssertEqual(TranscriptPress.resolve("", arrow: .left, shift: false), .step(.fold))
        XCTAssertEqual(TranscriptPress.resolve("G", shift: true), .step(.last))
        XCTAssertEqual(TranscriptPress.resolve("g", shift: true), .step(.last))
        XCTAssertEqual(TranscriptPress.resolve("y", shift: false), .answer("y"))
        XCTAssertEqual(TranscriptLeader.resolve("g"), .first)
        XCTAssertEqual(TranscriptLeader.resolve("3"), .jump(3))
        XCTAssertEqual(TranscriptLeader.resolve("N"), .needs(-1))
    }
}
