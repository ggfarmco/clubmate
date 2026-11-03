import Foundation
import XCTest

import ClubmateProtocol

import ClubmateKit

final class GraphLayoutTests: XCTestCase {
    private static let walkthrough = """
    {"type": "graph", "id": "g", "props": {
      "nodes": [
        {"id": "slicer", "label": "Slicer", "state": "idle · 3 turns", "start": true, "outs": [
          {"id": "step", "label": "step", "count": 3, "state": "last"},
          {"id": "complete", "label": "complete", "state": "never"}]},
        {"id": "quizzer", "label": "Quizzer", "state": "waiting for you", "attention": true, "outs": [
          {"id": "understood", "label": "understood", "count": 1, "state": "taken"},
          {"id": "gaps", "label": "gaps", "count": 1, "state": "taken"},
          {"id": "finish", "label": "finish", "state": "never"}]},
        {"id": "reconciler", "label": "Reconciler", "state": "not started", "pending": true, "outs": [
          {"id": "reported", "label": "reported", "state": "never", "end": true}]}],
      "edges": [
        {"from": "slicer", "out": "step", "to": "quizzer", "state": "last"},
        {"from": "slicer", "out": "complete", "to": "reconciler", "state": "never"},
        {"from": "quizzer", "out": "understood", "to": "slicer", "state": "taken"},
        {"from": "quizzer", "out": "gaps", "to": "slicer", "state": "taken"},
        {"from": "quizzer", "out": "finish", "to": "reconciler", "state": "never"}],
      "moves": [
        {"node": "slicer", "cause": -1, "tooltip": "move 1 · Slicer · 23:20"},
        {"node": "quizzer", "cause": 0, "out": "step", "tooltip": "move 2 · Quizzer · 23:22", "current": true, "last": true}]
    }}
    """

    private func graph(_ json: String) throws -> SDUIGraph {
        try XCTUnwrap(JSONDecoder().decode(SDUINode.self, from: Data(json.utf8)).graph())
    }

    private func kinds(_ layout: GraphLayout) -> [Int: GraphRoute.Kind] {
        Dictionary(uniqueKeysWithValues: layout.routes.map { ($0.edge, $0.kind) })
    }

    func testPRWalkthroughLayers() throws {
        let layout = GraphLayout(graph: try graph(Self.walkthrough), metrics: GraphMetrics())
        XCTAssertEqual(layout.layers, [["slicer"], ["quizzer"], ["reconciler"]])
        XCTAssertEqual(kinds(layout), [0: .forward, 1: .skip, 2: .back, 3: .back, 4: .forward])
    }

    func testPlainStateDecodes() throws {
        let g = try graph(#"{"type": "graph", "id": "g", "props": {"nodes": [{"id": "a", "label": "A", "state": "", "outs": [{"id": "x", "label": "x", "state": "plain"}]}], "edges": [{"from": "a", "out": "x", "to": "a", "state": "plain"}]}}"#)
        XCTAssertEqual([g.nodes[0].outs[0].state, g.edges[0].state], [.plain, .plain])
    }

    func testLimitsDoNotOverlap() {
        let targets = [[1, 2, 3, 7], [4, 5, 7, 0], [4, 5, 6, 0], [4, 5, 6, 0], [7, 7, 0, 1], [7, 0, 1, 0], [7, 0, 2, 0], [0, 0, 0, 0]]
        let label = String(repeating: "x", count: 20)
        let nodes = targets.indices.map { i in
            SDUIGraph.Node(id: "n\(i)", label: label, state: label, start: i == 0, outs: (0..<4).map { SDUIGraph.Out(id: "o\($0)", label: label, state: .taken) })
        }
        let edges = targets.enumerated().flatMap { i, row in row.enumerated().map { SDUIGraph.Edge(from: "n\(i)", out: "o\($0.offset)", to: "n\($0.element)", state: .taken) } }
        let layout = GraphLayout(graph: SDUIGraph(nodes: nodes, edges: edges), metrics: GraphMetrics())

        XCTAssertEqual(layout.routes.count, 32)
        XCTAssertEqual(layout.layers, [["n0"], ["n1", "n2", "n3"], ["n4", "n5", "n6"], ["n7"]])
        let cards = Array(layout.cards.values)
        XCTAssertEqual(cards.count, 8)
        for a in cards.indices {
            for b in cards.indices where a < b {
                XCTAssertFalse(cards[a].intersects(cards[b]))
            }
        }
        let tracked = layout.routes.filter { $0.kind != .forward }
        XCTAssertGreaterThanOrEqual(tracked.filter { $0.kind == .skip }.count, 2)
        XCTAssertGreaterThanOrEqual(tracked.filter { $0.kind == .back }.count, 2)
        let tracks = tracked.map { $0.points[3].y }
        XCTAssertEqual(Set(tracks).count, tracks.count)
        XCTAssertTrue(tracked.allSatisfy { $0.points[3].y == $0.points[6].y })
    }

    func testDanglingReferencesAreSkipped() throws {
        let json = """
        {"type": "graph", "id": "g", "props": {
          "nodes": [{"id": "a", "label": "A", "state": "", "start": true, "outs": [{"id": "x", "label": "x", "state": "taken"}]}],
          "edges": [{"from": "a", "out": "x", "to": "ghost", "state": "taken"}, {"from": "a", "out": "nope", "to": "a", "state": "taken"}, {"from": "a", "out": "x", "to": "a", "state": "last"}],
          "moves": [{"node": "a", "cause": 7, "tooltip": ""}, {"node": "a", "cause": 1e300, "tooltip": ""}]
        }}
        """
        let graph = try graph(json)
        let layout = GraphLayout(graph: graph, metrics: GraphMetrics())
        XCTAssertEqual(graph.moves.map(\.cause), [-1, -1])
        XCTAssertEqual(kinds(layout), [2: .back])
        XCTAssertEqual(layout.port(node: "ghost", out: "x"), .zero)
    }
}
