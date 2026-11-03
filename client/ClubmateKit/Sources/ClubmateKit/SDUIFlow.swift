import Foundation

import ClubmateProtocol

public struct SDUIFlow: Sendable, Equatable {
    public enum EdgeState: String, Sendable {
        case taken
        case last
        case never
        case plain
        case selected
        case dim
        case ghost
        case held

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
            self.init(id: id, label: fields["label"]?.stringValue ?? id, count: max(0, SDUIFlow.int(fields["count"]) ?? 0), state: EdgeState(fields["state"]?.stringValue), end: fields["end"]?.boolValue ?? false)
        }
    }

    public struct Card: Sendable, Equatable {
        public let id: String
        public let label: String
        public let state: String
        public let tone: String?
        public let icon: String?
        public let attention: Bool
        public let pending: Bool
        public let ghost: Bool
        public let start: Bool
        public let tooltip: String?
        public let kind: String?
        public let command: String?
        public let since: Date?
        public let timeout: Int
        public let tag: String?
        public let outs: [Out]

        public init(id: String, label: String, state: String, tone: String? = nil, icon: String? = nil, attention: Bool = false, pending: Bool = false, ghost: Bool = false, start: Bool = false, tooltip: String? = nil, kind: String? = nil, command: String? = nil, since: Date? = nil, timeout: Int = 0, tag: String? = nil, outs: [Out]) {
            self.id = id
            self.label = label
            self.state = state
            self.tone = tone
            self.icon = icon
            self.attention = attention
            self.pending = pending
            self.ghost = ghost
            self.start = start
            self.tooltip = tooltip
            self.kind = kind
            self.command = command
            self.since = since
            self.timeout = timeout
            self.tag = tag
            self.outs = outs
        }

        public var isStep: Bool { kind == "check" || kind == "action" }

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
                ghost: fields["ghost"]?.boolValue ?? false,
                start: fields["start"]?.boolValue ?? false,
                tooltip: fields["tooltip"]?.stringValue,
                kind: fields["kind"]?.stringValue,
                command: fields["command"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 },
                since: SDUIRunstrip.date(fields["since"]),
                timeout: max(0, SDUIFlow.int(fields["timeout"]) ?? 0),
                tag: fields["tag"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 },
                outs: fields["outs"]?.arrayValue?.compactMap(Out.init) ?? []
            )
        }
    }

    public struct Marker: Sendable, Equatable {
        public enum Kind: String, Sendable {
            case start
            case end
            case schedule
            case inbox
            case session
            case runs
        }

        public let id: String
        public let kind: Kind
        public let label: String?
        public let reached: Bool

        public init(id: String, kind: Kind, label: String? = nil, reached: Bool = false) {
            self.id = id
            self.kind = kind
            self.label = label
            self.reached = reached
        }

        init?(_ value: JSONValue) {
            guard let fields = value.objectValue, let id = fields["id"]?.stringValue, !id.isEmpty,
                  let kind = fields["kind"]?.stringValue.flatMap(Kind.init(rawValue:)) else { return nil }
            self.init(id: id, kind: kind, label: fields["label"]?.stringValue, reached: fields["reached"]?.boolValue ?? false)
        }
    }

    public struct Edge: Sendable, Equatable {
        public let from: String
        public let out: String?
        public let to: String
        public let state: EdgeState
        public let ref: String?
        public let tooltip: String?
        public let label: String?

        public init(from: String, out: String?, to: String, state: EdgeState, ref: String? = nil, tooltip: String? = nil, label: String? = nil) {
            self.from = from
            self.out = out
            self.to = to
            self.state = state
            self.ref = ref
            self.tooltip = tooltip
            self.label = label
        }

        init?(_ value: JSONValue) {
            guard let fields = value.objectValue, let from = fields["from"]?.stringValue, let to = fields["to"]?.stringValue else { return nil }
            self.init(
                from: from,
                out: fields["out"]?.stringValue,
                to: to,
                state: EdgeState(fields["state"]?.stringValue),
                ref: fields["ref"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 },
                tooltip: fields["tooltip"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 },
                label: fields["label"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
            )
        }

        public var pointable: Bool { ref != nil || tooltip != nil }
    }

    public let cards: [Card]
    public let markers: [Marker]
    public let edges: [Edge]
    public let reached: Bool

    public init(cards: [Card], markers: [Marker] = [], edges: [Edge], reached: Bool = false) {
        self.cards = cards
        self.markers = markers
        self.edges = edges
        self.reached = reached
    }

    static func int(_ value: JSONValue?) -> Int? {
        value?.numberValue.flatMap { Int(exactly: $0.rounded(.towardZero)) }
    }
}

public struct SDUILanes: Sendable, Equatable {
    public struct Actor: Sendable, Equatable {
        public let id: String
        public let label: String
        public let icon: String?
        public let tone: String?
        public let attention: Bool
        public let pending: Bool

        public init(id: String, label: String, icon: String? = nil, tone: String? = nil, attention: Bool = false, pending: Bool = false) {
            self.id = id
            self.label = label
            self.icon = icon
            self.tone = tone
            self.attention = attention
            self.pending = pending
        }
    }

    public struct Move: Sendable, Equatable {
        public let actor: String
        public let cause: Int
        public let label: String?
        public let tooltip: String
        public let out: String?
        public let current: Bool
        public let user: Bool
        public let last: Bool
        public let end: String?
        public let tone: String?
        public let ref: String?
        public let bar: String?
        public let round: Int

        public init(actor: String, cause: Int = -1, label: String? = nil, tooltip: String = "", out: String? = nil, current: Bool = false, user: Bool = false, last: Bool = false, end: String? = nil, tone: String? = nil, ref: String? = nil, bar: String? = nil, round: Int = 0) {
            self.bar = bar
            self.round = round
            self.actor = actor
            self.cause = cause
            self.label = label
            self.tooltip = tooltip
            self.out = out
            self.current = current
            self.user = user
            self.last = last
            self.end = end
            self.tone = tone
            self.ref = ref
        }
    }

    public let actors: [Actor]
    public let moves: [Move]
    public let title: String?
    public let collapsed: Bool
    public let count: Int?

    public init(actors: [Actor], moves: [Move], title: String? = nil, collapsed: Bool = false, count: Int? = nil) {
        var seen = Set<String>()
        self.actors = actors.filter { seen.insert($0.id).inserted }
        self.moves = moves
        self.title = title
        self.collapsed = collapsed
        self.count = count
    }

    static func cause(_ value: JSONValue?, index: Int) -> Int {
        SDUIFlow.int(value).flatMap { (0..<index).contains($0) ? $0 : nil } ?? -1
    }
}

public struct SDUIRunstrip: Sendable, Equatable {
    public enum Status: String, Sendable {
        case ok
        case failed
        case missed
        case running
        case ignored
    }

    public struct Run: Sendable, Equatable {
        public let at: Date
        public let status: Status
        public let tooltip: String?
        public let ref: String?

        public init(at: Date, status: Status, tooltip: String? = nil, ref: String? = nil) {
            self.at = at
            self.status = status
            self.tooltip = tooltip
            self.ref = ref
        }
    }

    public struct Span: Sendable, Equatable {
        public let from: Date
        public let to: Date?

        public init(from: Date, to: Date? = nil) {
            self.from = from
            self.to = to
        }
    }

    public let runs: [Run]
    public let now: Date?
    public let next: Date?
    public let paused: [Span]
    public let selected: String?
    public let title: String?
    public let count: Int?

    public init(runs: [Run], now: Date? = nil, next: Date? = nil, paused: [Span] = [], selected: String? = nil, title: String? = nil, count: Int? = nil) {
        self.runs = runs.sorted { $0.at < $1.at }
        self.now = now
        self.next = next
        self.paused = paused
        self.selected = selected
        self.title = title
        self.count = count
    }

    public var legend: [(status: Status, count: Int)] {
        [Status.ok, .failed, .missed, .running, .ignored].compactMap { status in
            let n = runs.count { $0.status == status }
            return n > 0 ? (status, n) : nil
        }
    }

    public var range: ClosedRange<Date>? {
        let times = runs.map(\.at) + [now, next].compactMap { $0 }
        guard let first = runs.first?.at ?? times.min() else { return nil }
        let last = max(times.max() ?? first, first)
        return first...last
    }

    public func x(_ date: Date, width: CGFloat, inset: CGFloat) -> CGFloat {
        guard let range else { return inset }
        let span = range.upperBound.timeIntervalSince(range.lowerBound)
        let usable = max(0, width - 2 * inset)
        guard span > 0 else { return inset + usable / 2 }
        let t = min(max(date.timeIntervalSince(range.lowerBound) / span, 0), 1)
        return inset + usable * t
    }

    public func step(from ref: String?, by delta: Int) -> String? {
        let refs = runs.compactMap(\.ref)
        guard !refs.isEmpty else { return nil }
        guard let ref, let index = refs.firstIndex(of: ref) else { return refs.last }
        return refs[min(max(index + delta, 0), refs.count - 1)]
    }

    static func date(_ value: JSONValue?) -> Date? {
        guard let text = value?.stringValue else { return nil }
        let plain = ISO8601DateFormatter()
        if let date = plain.date(from: text) { return date }
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fractional.date(from: text)
    }
}

extension SDUINode {
    public func flow() -> SDUIFlow? {
        guard let cards = props["cards"]?.arrayValue else { return nil }
        return SDUIFlow(
            cards: cards.compactMap(SDUIFlow.Card.init),
            markers: props["markers"]?.arrayValue?.compactMap(SDUIFlow.Marker.init) ?? [],
            edges: props["edges"]?.arrayValue?.compactMap(SDUIFlow.Edge.init) ?? []
        )
    }

    public func lanes() -> SDUILanes? {
        guard let actors = props["actors"]?.arrayValue else { return nil }
        let moves = props["moves"]?.arrayValue ?? []
        return SDUILanes(
            actors: actors.compactMap { value in
                guard let fields = value.objectValue, let id = fields["id"]?.stringValue, !id.isEmpty else { return nil }
                return SDUILanes.Actor(id: id, label: fields["label"]?.stringValue ?? id, icon: fields["icon"]?.stringValue, tone: fields["tone"]?.stringValue)
            },
            moves: moves.enumerated().map { index, value in
                let fields = value.objectValue ?? [:]
                return SDUILanes.Move(
                    actor: fields["actor"]?.stringValue ?? "",
                    cause: SDUILanes.cause(fields["cause"], index: index),
                    label: fields["label"]?.stringValue,
                    tooltip: fields["tooltip"]?.stringValue ?? "",
                    current: fields["current"]?.boolValue ?? false,
                    user: fields["user"]?.boolValue ?? false,
                    last: fields["last"]?.boolValue ?? false,
                    tone: fields["tone"]?.stringValue,
                    ref: fields["ref"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
                )
            },
            title: string("title").flatMap { $0.isEmpty ? nil : $0 },
            collapsed: bool("collapsed") ?? false,
            count: SDUIFlow.int(props["count"]).flatMap { $0 >= 0 ? $0 : nil }
        )
    }

    public func runstrip() -> SDUIRunstrip? {
        guard let runs = props["runs"]?.arrayValue else { return nil }
        return SDUIRunstrip(
            runs: runs.compactMap { value in
                guard let fields = value.objectValue, let at = SDUIRunstrip.date(fields["at"]),
                      let status = fields["status"]?.stringValue.flatMap(SDUIRunstrip.Status.init(rawValue:)) else { return nil }
                return SDUIRunstrip.Run(at: at, status: status, tooltip: fields["tooltip"]?.stringValue, ref: fields["ref"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 })
            },
            now: SDUIRunstrip.date(props["now"]),
            next: SDUIRunstrip.date(props["next"]),
            paused: props["paused"]?.arrayValue?.compactMap { value in
                guard let fields = value.objectValue, let from = SDUIRunstrip.date(fields["from"]) else { return nil }
                return SDUIRunstrip.Span(from: from, to: SDUIRunstrip.date(fields["to"]))
            } ?? [],
            selected: string("selected").flatMap { $0.isEmpty ? nil : $0 },
            title: string("title").flatMap { $0.isEmpty ? nil : $0 },
            count: SDUIFlow.int(props["count"]).flatMap { $0 >= 0 ? $0 : nil }
        )
    }
}
