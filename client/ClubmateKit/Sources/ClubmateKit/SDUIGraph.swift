import Foundation

import ClubmateProtocol

public struct SDUIGraph: Sendable, Equatable {
    public typealias EdgeState = SDUIFlow.EdgeState
    public typealias Out = SDUIFlow.Out
    public typealias Node = SDUIFlow.Card
    public typealias Edge = SDUIFlow.Edge

    public struct Move: Sendable, Equatable {
        public let node: String
        public let cause: Int
        public let label: String?
        public let tooltip: String
        public let out: String?
        public let current: Bool
        public let user: Bool
        public let last: Bool
        public let end: String?

        public init(node: String, cause: Int = -1, label: String? = nil, tooltip: String = "", out: String? = nil, current: Bool = false, user: Bool = false, last: Bool = false, end: String? = nil) {
            self.node = node
            self.cause = cause
            self.label = label
            self.tooltip = tooltip
            self.out = out
            self.current = current
            self.user = user
            self.last = last
            self.end = end
        }

        init(_ value: JSONValue, index: Int) {
            let fields = value.objectValue ?? [:]
            self.init(
                node: fields["node"]?.stringValue ?? "",
                cause: SDUILanes.cause(fields["cause"], index: index),
                label: fields["label"]?.stringValue,
                tooltip: fields["tooltip"]?.stringValue ?? "",
                out: fields["out"]?.stringValue,
                current: fields["current"]?.boolValue ?? false,
                user: fields["user"]?.boolValue ?? false,
                last: fields["last"]?.boolValue ?? false,
                end: fields["end"]?.stringValue
            )
        }
    }

    public let nodes: [Node]
    public let edges: [Edge]
    public let moves: [Move]
    public let reached: Bool

    public init(nodes: [Node], edges: [Edge], moves: [Move] = [], reached: Bool = false) {
        self.nodes = nodes
        self.edges = edges
        self.moves = moves
        self.reached = reached
    }

    public var flow: SDUIFlow {
        SDUIFlow(cards: nodes, edges: edges.filter { $0.out != nil }, reached: reached)
    }

    public var lanes: SDUILanes {
        let actors = nodes.map { SDUILanes.Actor(id: $0.id, label: $0.label, icon: $0.icon, tone: $0.tone, attention: $0.attention, pending: $0.pending) }
        return SDUILanes(actors: actors, moves: moves.enumerated().map { index, move in
            let end = move.end.map { end in nodes.first { $0.id == move.node }?.outs.first { $0.id == end }?.label ?? end }
            return SDUILanes.Move(actor: move.node, cause: move.cause, label: move.label, tooltip: move.tooltip, out: move.out, current: move.current, user: move.user, last: move.last, end: end, ref: String(index))
        }, title: "History", collapsed: true)
    }
}

extension SDUINode {
    public func graph() -> SDUIGraph? {
        guard let nodes = props["nodes"]?.arrayValue else { return nil }
        let moves = props["moves"]?.arrayValue ?? []
        return SDUIGraph(
            nodes: nodes.compactMap(SDUIGraph.Node.init),
            edges: props["edges"]?.arrayValue?.compactMap(SDUIGraph.Edge.init) ?? [],
            moves: moves.enumerated().map { SDUIGraph.Move($0.element, index: $0.offset) },
            reached: bool("reached") ?? false
        )
    }
}
