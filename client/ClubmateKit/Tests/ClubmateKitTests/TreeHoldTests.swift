import Foundation
import XCTest

import ClubmateKit

final class TreeHoldTests: XCTestCase {
    private func tree(_ json: String, hold: String? = nil, selected: String? = nil) throws -> SDUINode {
        var node = try JSONDecoder().decode(SDUINode.self, from: Data(#"{"type": "tree", "id": "sessions", "props": {"items": \#(json)}}"#.utf8))
        if let hold { node.props["hold"] = .string(hold) }
        if let selected { node.props["selected"] = .string(selected) }
        return node
    }

    func testHeldTreeKeepsItsRowsAndTakesNewContent() throws {
        let shown = try tree(#"""
        [{"id": "wt:main", "label": "main", "action": true, "hasChildren": true, "expanded": true, "detail": "2",
          "children": [{"id": "a", "label": "A", "detail": "running"}, {"id": "b", "label": "B"}]}]
        """#)
        let incoming = try tree(#"""
        [{"id": "wt:inbox:needs", "label": "Needs you", "action": true, "hasChildren": true, "expanded": true, "detail": "1",
          "children": [{"id": "mirror:a", "label": "A", "mirrorOf": "a"}]},
         {"id": "wt:main", "label": "main", "action": true, "hasChildren": true, "expanded": true, "detail": "3",
          "children": [{"id": "a", "label": "A", "detail": "asks you", "tone": "warning"}, {"id": "b", "label": "B"}, {"id": "c", "label": "C"}]}]
        """#)

        let held = TreeHoldMerge.merge(shown: shown, incoming: incoming)

        let rows = held.node.treeItems().flatMap(\.visibleItems)
        XCTAssertEqual(rows.map(\.id), ["wt:main", "a", "b"])
        XCTAssertEqual(rows.map(\.detail), ["2", "asks you", nil])
        XCTAssertEqual(held.node.treeItems()[0].children[0].tone, "warning")
        XCTAssertEqual(held.pending, 2)
        XCTAssertTrue(TreeHoldMerge.shows("b", in: held.node))
        XCTAssertFalse(TreeHoldMerge.shows("c", in: held.node))
        XCTAssertTrue(TreeHoldMerge.shows("a", in: incoming))
        XCTAssertEqual(TreeHoldMerge.merge(shown: incoming, incoming: incoming).pending, 0)
    }

    func testGateHoldsWhileTheSelectionStaysOffTheTreeAndLandsWhenItMovesThere() throws {
        var gate = TreeGate()
        XCTAssertFalse(gate.receive(try tree(#"[{"id": "a", "label": "A"}]"#, hold: "all", selected: "graph:nightly")))
        gate.arm()

        XCTAssertFalse(gate.receive(try tree(#"[{"id": "a", "label": "A"}, {"id": "b", "label": "B"}]"#, hold: "all", selected: "graph:nightly")))
        XCTAssertEqual(gate.pending, 1)
        XCTAssertTrue(gate.receive(try tree(#"[{"id": "a", "label": "A"}, {"id": "b", "label": "B"}]"#, hold: "all", selected: "b")))
    }

    func testGateDropsItsBaseWhenTheTreeIsNoLongerTheHeldOne() throws {
        var gate = TreeGate()
        _ = gate.receive(try tree(#"[{"id": "a", "label": "A"}]"#, hold: "all"))
        gate.arm()

        _ = gate.receive(try tree(#"[]"#))
        let running = try tree(#"[{"id": "r", "label": "R"}]"#, hold: "running")
        XCTAssertFalse(gate.receive(running))
        XCTAssertEqual(gate.display(running), running)
        XCTAssertEqual(gate.pending, 0)

        let other = try tree(#"[{"id": "r", "label": "R"}, {"id": "s", "label": "S"}]"#, hold: "all")
        _ = gate.receive(other)
        XCTAssertEqual(gate.display(other), other)
    }
}
