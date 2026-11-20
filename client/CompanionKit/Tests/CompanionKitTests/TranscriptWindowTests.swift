import Foundation
import XCTest

import CompanionKit

final class TranscriptWindowTests: XCTestCase {
    private func rows(_ count: Int) throws -> [TranscriptRow] {
        let entries = (1...count).map { #"{"kind":"agent","id":"a\#($0)","turn":\#($0),"text":"reply \#($0)"}"# }
        return try screenRows("[" + entries.joined(separator: ",") + "]")
    }

    private func screenRows(_ json: String) throws -> [TranscriptRow] {
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        return SessionScreen(entries: entries, session: AgentSession(id: "s", status: .done)).rows
    }

    func testPagesEarlierAHundredAtATime() throws {
        let all = try rows(300)
        var window = TranscriptWindow()
        window.update(count: all.count, atBottom: true)
        XCTAssertEqual(window.skip(all), 240)
        window.showEarlier(all)
        XCTAssertEqual(window.skip(all), 140)
        window.showEarlier(all)
        window.showEarlier(all)
        window.showEarlier(all)
        XCTAssertEqual(window.skip(all), 0)
    }

    func testRewindowsOnlyAtTheBottomOnceAPageGrewBelow() throws {
        var window = TranscriptWindow()
        window.update(count: 10, atBottom: true)
        let grown = try rows(110)
        window.update(count: grown.count, atBottom: false)
        XCTAssertEqual(window.skip(grown), 0)
        window.update(count: grown.count, atBottom: true)
        XCTAssertEqual(window.skip(grown), 50)
    }

    func testTheBottomCountsTheTopInset() {
        XCTAssertTrue(TranscriptWindow.atBottom(offset: 8264, insetTop: 184, container: 573, content: 9022))
        XCTAssertFalse(TranscriptWindow.atBottom(offset: 8100, insetTop: 184, container: 573, content: 9022))
        XCTAssertTrue(TranscriptWindow.atBottom(offset: -733, insetTop: 733, container: 573, content: 24))
    }

    func testAShorterRefetchFallsBackToTheLastRows() throws {
        var window = TranscriptWindow()
        window.update(count: 300, atBottom: true)
        let shorter = try rows(100)
        XCTAssertEqual(window.skip(shorter), 40)
    }

    func testKeepsAPlanWithTheToolsAtTheEdge() throws {
        var entries = (1...60).map { #"{"kind":"agent","id":"a\#($0)","turn":1,"text":"reply \#($0)"}"# }
        entries.insert(#"{"kind":"plan","id":"plan-1","turn":1,"plan":{"entries":[{"content":"Read","status":"pending"}]}}"#, at: 1)
        entries.insert(#"{"kind":"tool","id":"t1","turn":1,"tool":{"kind":"read","status":"completed","title":"Read a.go"}}"#, at: 2)
        let all = try screenRows("[" + entries.joined(separator: ",") + "]")
        var window = TranscriptWindow()
        window.update(count: all.count, atBottom: true)
        XCTAssertEqual(all.count, 62)
        XCTAssertEqual(window.skip(all), 1)
    }
}
