import Foundation
import XCTest

@testable import ClubmateKit

final class SDUIDeltaTests: XCTestCase {
    private struct Case: Decodable {
        let name: String
        let base: SDUINode
        let tree: SDUINode
        let delta: SDUIDelta
    }

    func testGoFixturesResolveToTheNewTree() throws {
        let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let data = try Data(contentsOf: repo.appending(path: "internal/sdui/testdata/delta.json"))
        let cases = try JSONDecoder().decode([Case].self, from: data)
        XCTAssertFalse(cases.isEmpty)
        for c in cases {
            XCTAssertEqual(c.delta.resolve(against: c.base), c.tree, c.name)
        }
    }

    func testDeltaOnAnotherRevNeedsResync() throws {
        let current = SlotView(workspaceId: "prj_1", slot: "a.main", owner: "a", rev: 4, tree: SDUINode(type: "text", props: ["text": .string("x")]))
        let json = #"{"workspaceId":"prj_1","slot":"a.main","owner":"a","rev":6,"base":5,"delta":{"ref":[]}}"#
        let update = try JSONDecoder().decode(SlotUpdate.self, from: Data(json.utf8))
        XCTAssertNil(update.view(over: current))
        let next = #"{"workspaceId":"prj_1","slot":"a.main","owner":"a","rev":5,"base":4,"delta":{"ref":[]}}"#
        XCTAssertEqual(try JSONDecoder().decode(SlotUpdate.self, from: Data(next.utf8)).view(over: current)?.tree, current.tree)
    }
}
