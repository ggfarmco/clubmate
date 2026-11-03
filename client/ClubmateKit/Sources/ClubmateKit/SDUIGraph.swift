import Foundation

import ClubmateProtocol

public struct SDUIGraph: Sendable, Equatable {
    public enum EdgeState: String, Sendable {
        case taken
        case last
        case never
        case plain

        init(_ raw: String?) {
            self = raw.flatMap(EdgeState.init(rawValue:)) ?? .never
        }
    }

    public struct Out: Sendable, Equatable {
        public let id: String
        public let label: String
        public let count: Int
        public let state: EdgeState
        public let end: Bool

        public init(id: String, label: String, count: Int = 0, state: EdgeState, end: Bool = false) {
            self.id = id
            self.label = label
            self.count = count
            self.state = state
            self.end = end
        }

        init?(_ value: JSONValue) {
            guard let fields = value.objectValue, let id = fields["id"]?.stringValue else { return nil }
            self.init(id: id, label: fields["label"]?.stringValue ?? id, count: max(0, SDUIGraph.int(fields["count"]) ?? 0), state: EdgeState(fields["state"]?.stringValue), end: fields["end"]?.boolValue ?? false)
        }
    }

    public struct Node: Sendable, Equatable {
        public let id: String
        public let label: String
        public let state: String
        public let tone: String?
        public let icon: String?
        public let attention: Bool
        public let pending: Bool
        public let start: Bool
        public let tooltip: String?
        public let outs: [Out]

        public init(id: String, label: String, state: String, tone: String? = nil, icon: String? = nil, attention: Bool = false, pending: Bool = false, start: Bool = false, tooltip: String? = nil, outs: [Out]) {
            self.id = id
            self.label = label
            self.state = state
            self.tone = tone
            self.icon = icon
            self.attention = attention
            self.pending = pending
            self.start = start
            self.tooltip = tooltip
            self.outs = outs
        }

        init?(_ value: JSONValue) {
            guard let fields = value.objectValue, let id = fields["id"]?.stringValue, !id.isEmpty else { return nil }
            self.init(
                id: id,
                label: fields["label"]?.stringValue ?? id,
                state: fields["state"]?.stringValue ?? "",
                tone: fields["tone"]?.stringValue,
                icon: fields["icon"]?.stringValue,
                attention: fields["attention"]?.boolValue ?? false,
                pending: fields["pending"]?.boolValue ?? false,
                start: fields["start"]?.boolValue ?? false,
                tooltip: fields["tooltip"]?.stringValue,
                outs: fields["outs"]?.arrayValue?.compactMap(Out.init) ?? []
            )
        }
    }

    public struct Edge: Sendable, Equatable {
        public let from: String
        public let out: String
        public let to: String
        public let state: EdgeState

        public init(from: String, out: String, to: String, state: EdgeState) {
            self.from = from
            self.out = out
            self.to = to
            self.state = state
        }

        init?(_ value: JSONValue) {
            guard let fields = value.objectValue, let from = fields["from"]?.stringValue, let out = fields["out"]?.stringValue, let to = fields["to"]?.stringValue else { return nil }
            self.init(from: from, out: out, to: to, state: EdgeState(fields["state"]?.stringValue))
        }
    }

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
            let cause = SDUIGraph.int(fields["cause"]).flatMap { (0..<index).contains($0) ? $0 : nil } ?? -1
            self.init(
                node: fields["node"]?.stringValue ?? "",
                cause: cause,
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

    fileprivate static func int(_ value: JSONValue?) -> Int? {
        value?.numberValue.flatMap { Int(exactly: $0.rounded(.towardZero)) }
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
