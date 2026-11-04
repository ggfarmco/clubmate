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

    func testWalkthroughGeometryIsPinned() throws {
        let layout = GraphLayout(graph: try graph(Self.walkthrough), metrics: GraphMetrics())
        let f = { (v: CGFloat) in String(format: "%.1f", v) }
        let cards = layout.cards.sorted { $0.key < $1.key }.map { "\($0.key) \(f($0.value.minX)),\(f($0.value.minY)),\(f($0.value.width)),\(f($0.value.height))" }
        let routes = layout.routes.map { "\($0.edge):" + $0.points.map { "\(f($0.x)),\(f($0.y))" }.joined(separator: " ") }
        XCTAssertEqual(([f(layout.size.width) + "x" + f(layout.size.height)] + cards + routes).joined(separator: "\n"), """
        990.0x279.3
        quizzer 390.0,72.0,210.0,147.3
        reconciler 720.0,72.0,210.0,95.3
        slicer 60.0,72.0,210.0,121.3
        0:270.0,145.7 330.0,145.7 330.0,98.0 390.0,98.0
        1:270.0,171.7 306.0,171.7 288.0,255.3 342.0,255.3 444.0,255.3 546.0,255.3 648.0,255.3 702.0,255.3 684.0,98.0 720.0,98.0
        2:600.0,145.7 636.0,145.7 624.0,36.0 576.0,36.0 408.0,36.0 240.0,36.0 72.0,36.0 24.0,36.0 24.0,98.0 60.0,98.0
        3:600.0,171.7 648.0,171.7 636.0,24.0 576.0,24.0 408.0,24.0 240.0,24.0 72.0,24.0 12.0,24.0 12.0,98.0 60.0,98.0
        4:600.0,197.7 660.0,197.7 660.0,98.0 720.0,98.0
        """)
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

    func testMarkersLineUpWithTheirPortWhenThereIsRoom() throws {
        let card = SDUIFlow.Card(id: "job", label: "Job", state: "", outs: [SDUIFlow.Out(id: "ok", label: "ok", state: .taken), SDUIFlow.Out(id: "failed", label: "failed", state: .never)])
        let flow = SDUIFlow(cards: [card], markers: [SDUIFlow.Marker(id: "runs", kind: .runs), SDUIFlow.Marker(id: "inbox", kind: .inbox)], edges: [
            SDUIFlow.Edge(from: "job", out: "ok", to: "runs", state: .taken),
            SDUIFlow.Edge(from: "job", out: "failed", to: "inbox", state: .never),
        ])
        let m = GraphMetrics()
        let layout = GraphLayout(flow: flow, metrics: m, markers: ["runs": CGSize(width: 80, height: 18), "inbox": CGSize(width: 80, height: 28)])
        let runs = try XCTUnwrap(layout.markers["runs"]), inbox = try XCTUnwrap(layout.markers["inbox"])
        XCTAssertEqual(runs.midY, layout.port(node: "job", out: "ok").y, accuracy: 0.01)
        XCTAssertEqual(inbox.minY, runs.maxY + m.nodeGap, accuracy: 0.01)
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
