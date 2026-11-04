import Foundation
import XCTest

import ClubmateProtocol

import ClubmateKit

final class SDUIFlowTests: XCTestCase {
    private func node(_ json: String) throws -> SDUINode {
        try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8))
    }

    func testCronFlowPutsMarkersBesideTheCardOnTheirOwnGaps() throws {
        let flow = try XCTUnwrap(node("""
        {"type": "flow", "id": "f", "props": {
          "cards": [{"id": "check", "label": "Check CI", "outs": [{"id": "ok", "label": "ok", "state": "taken"}, {"id": "failed", "label": "failed", "state": "last"}]}],
          "markers": [{"id": "when", "kind": "schedule", "label": "every 2 min"}, {"id": "inbox", "kind": "inbox", "label": "Inbox"}, {"id": "odd", "kind": "clock"}, {"id": "runs", "kind": "runs"}],
          "edges": [{"from": "when", "to": "check", "state": "taken"}, {"from": "check", "out": "failed", "to": "inbox", "state": "last", "ref": "trg_1", "tooltip": "failed → inbox", "label": "paused"}, {"from": "check", "to": "inbox", "state": "taken"}]
        }}
        """).flow())
        XCTAssertEqual(flow.markers.map(\.id), ["when", "inbox", "runs"])
        XCTAssertEqual(flow.markers[2].kind, .runs)
        XCTAssertEqual(flow.edges.map(\.label), [nil, "paused", nil])
        let m = GraphMetrics()
        let layout = GraphLayout(flow: SDUIFlow(cards: flow.cards, markers: Array(flow.markers.prefix(2)), edges: flow.edges), metrics: m, markers: ["when": CGSize(width: 28, height: m.headHeight), "inbox": CGSize(width: 80, height: m.headHeight)])
        XCTAssertEqual(layout.layers, [["when"], ["check"], ["inbox"]])
        XCTAssertEqual(layout.routes.map(\.edge), [0, 1])
        let when = try XCTUnwrap(layout.markers["when"]), check = try XCTUnwrap(layout.cards["check"]), inbox = try XCTUnwrap(layout.markers["inbox"])
        XCTAssertEqual(check.minX - when.maxX, m.markerGap)
        XCTAssertEqual(inbox.minX - check.maxX, m.markerGap)
        XCTAssertEqual(when.midY, layout.input(node: "check").y)
        XCTAssertEqual(layout.size.width, inbox.maxX + m.layerGap / 2)
        XCTAssertEqual(flow.edges[1].ref, "trg_1")
    }

    func testGraphStepCardDecodesAndReservesItsStrip() throws {
        let graph = try XCTUnwrap(node("""
        {"type": "graph", "id": "g", "props": {
          "nodes": [{"id": "tests", "label": "Tests", "state": "running", "kind": "check", "command": "make test", "since": "2026-11-03T16:40:00Z", "timeout": 600, "tag": "changed", "outs": [{"id": "fail", "label": "fail", "state": "taken"}]},
                    {"id": "coder", "label": "Coder", "state": "done", "kind": "agent", "outs": []}],
          "edges": [{"from": "tests", "out": "fail", "to": "coder", "state": "held", "label": "×2/2 · waits for you"}]
        }}
        """).graph())
        let tests = graph.nodes[0]
        XCTAssertEqual([tests.kind, tests.command, tests.tag], ["check", "make test", "changed"])
        XCTAssertEqual(tests.since, ISO8601DateFormatter().date(from: "2026-11-03T16:40:00Z"))
        XCTAssertEqual(tests.timeout, 600)
        XCTAssertTrue(tests.isStep)
        XCTAssertFalse(graph.nodes[1].isStep)
        XCTAssertEqual(graph.edges[0].state, .held)
        XCTAssertEqual(graph.edges[0].label, "×2/2 · waits for you")
        let m = GraphMetrics()
        let layout = GraphLayout(flow: graph.flow, metrics: m)
        let rect = try XCTUnwrap(layout.cards["tests"])
        XCTAssertEqual(rect.height, m.headHeight + m.strip(tests) + 2 * m.rowHeight / 3 + m.rowHeight)
        XCTAssertEqual(layout.port(node: "tests", out: "fail").y, rect.minY + m.top(tests) + m.rowHeight / 3 + m.rowHeight / 2)
    }

    func testEdgeAtFindsTheNearestRoute() throws {
        let flow = SDUIFlow(
            cards: [SDUIFlow.Card(id: "a", label: "A", state: "", outs: [SDUIFlow.Out(id: "x", label: "x", state: .plain)]), SDUIFlow.Card(id: "b", label: "B", state: "", outs: [])],
            edges: [SDUIFlow.Edge(from: "a", out: "x", to: "b", state: .plain, tooltip: "when A is done → prompt B")]
        )
        let layout = GraphLayout(flow: flow, metrics: GraphMetrics())
        let route = try XCTUnwrap(layout.routes.first)
        XCTAssertEqual(layout.edge(at: route.middle, tolerance: 1), 0)
        XCTAssertNil(layout.edge(at: CGPoint(x: route.middle.x, y: route.middle.y + 40), tolerance: 7))
    }

    func testGraphHistoryIsLanesWithItsEndLabelAndClickableMoves() throws {
        let graph = SDUIGraph(
            nodes: [SDUIGraph.Node(id: "coder", label: "Coder", state: "", outs: [SDUIGraph.Out(id: "done", label: "done", state: .last)]), SDUIGraph.Node(id: "reviewer", label: "Reviewer", state: "", attention: true, outs: [SDUIGraph.Out(id: "ok", label: "approved", state: .last, end: true)])],
            edges: [],
            moves: [SDUIGraph.Move(node: "coder"), SDUIGraph.Move(node: "reviewer", cause: 0, out: "done", last: true, end: "ok")]
        )
        let lanes = graph.lanes
        XCTAssertEqual(lanes.title, "History")
        XCTAssertTrue(lanes.collapsed)
        XCTAssertEqual(lanes.actors.map(\.attention), [false, true])
        XCTAssertEqual(lanes.moves.map(\.end), [nil, "approved"])
        XCTAssertEqual(lanes.moves.map(\.ref), ["0", "1"])
    }

    func testLanesNodeDropsCausesThatPointForward() throws {
        let lanes = try XCTUnwrap(node("""
        {"type": "lanes", "id": "l", "props": {"title": "Owners", "actors": [{"id": "you", "label": "You"}, {"id": "you", "label": "dup"}],
          "moves": [{"actor": "you", "cause": 1, "tooltip": "a"}, {"actor": "you", "cause": 0, "tooltip": "b", "ref": "agt_1", "tone": "danger", "time": "Nov 2 14:10"}]}}
        """).lanes())
        XCTAssertEqual(lanes.actors.map(\.label), ["You"])
        XCTAssertEqual(lanes.moves.map(\.cause), [-1, 0])
        XCTAssertEqual(lanes.moves.map(\.ref), [nil, "agt_1"])
        XCTAssertEqual(lanes.moves.map(\.time), [nil, "Nov 2 14:10"])
        XCTAssertFalse(lanes.collapsed)
    }

    func testRunstripSpansFirstRunToNextAndStepsThroughRefs() throws {
        let strip = try XCTUnwrap(node("""
        {"type": "runstrip", "id": "r", "props": {
          "runs": [{"at": "2026-11-03T16:24:00Z", "status": "failed", "ref": "b"}, {"at": "2026-11-03T16:20:00.5Z", "status": "ok", "ref": "a"}, {"at": "nope", "status": "ok"}, {"at": "2026-11-03T16:26:00Z", "status": "missed"}],
          "now": "2026-11-03T16:27:00Z", "next": "2026-11-03T16:30:00Z", "paused": [{"from": "2026-11-03T16:25:00Z"}], "selected": "b", "title": "Runs", "count": 3}}
        """).runstrip())
        XCTAssertEqual(strip.runs.map(\.ref), ["a", "b", nil])
        XCTAssertEqual(strip.legend.map(\.status), [.ok, .failed, .missed])
        XCTAssertEqual([strip.title, strip.count.map(String.init)], ["Runs", "3"])
        XCTAssertNil(strip.paused.first?.to)
        let next = try XCTUnwrap(strip.next)
        XCTAssertEqual(strip.range?.upperBound, next)
        XCTAssertEqual(strip.x(strip.runs[0].at, width: 120, inset: 10), 10)
        XCTAssertEqual(strip.x(next, width: 120, inset: 10), 110)
        XCTAssertEqual(strip.step(from: "b", by: -1), "a")
        XCTAssertEqual(strip.step(from: "b", by: 1), "b")
        XCTAssertEqual(strip.step(from: nil, by: -1), "b")
    }
}
