import XCTest

@testable import ClubmateKit

final class ChoiceSeedTests: XCTestCase {
    func testTheDraftWinsUntilTheServerSeedChanges() {
        let server = ChoiceSeed(picks: ["B"], other: "why B")
        let draft = ChoiceSeed(picks: ["A"], other: nil)
        XCTAssertEqual(ChoiceSeed.resolve(draft: draft, server: server, seen: nil), draft)
        XCTAssertEqual(ChoiceSeed.resolve(draft: draft, server: server, seen: server), draft)
        XCTAssertEqual(ChoiceSeed.resolve(draft: draft, server: server, seen: ChoiceSeed(picks: ["A"], other: nil)), server)
        XCTAssertEqual(ChoiceSeed.resolve(draft: nil, server: server, seen: nil), server)
    }
}
