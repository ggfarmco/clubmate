import Foundation

import ClubmateProtocol

public enum OpenCommand {
    public static let answer = "agents.open-answer"
    public static let later = "agents.open-later"
    public static let backlog = "agents.open-backlog"
    public static let no = "agents.open-no"
    public static let queue = "agents.open-queue"
    public static let convert = "agents.open-convert"
}

public struct OpenOption: Decodable, Sendable, Equatable, Identifiable {
    public let label: String
    public let description: String

    public var id: String { label }
}

public struct OpenItem: Decodable, Sendable, Equatable, Identifiable {
    public enum Kind: String, FallbackEnum {
        case decision
        case offer

        public static let fallback = Self.offer
    }

    public enum State: String, FallbackEnum {
        case open
        case answered
        case no
        case backlog
        case handed

        public static let fallback = Self.handed
    }

    public let id: String
    public let kind: Kind
    public let header: String
    public let question: String
    public let label: String
    public let description: String
    public let quote: String
    public let options: [OpenOption]
    public let multiSelect: Bool
    public let state: State
    public let task: Int?
    public let at: Date?
    public var number: Int

    enum CodingKeys: String, CodingKey {
        case id, kind, header, question, label, description, quote, options, multiSelect, state, task, number, at
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let text = { (key: CodingKeys) in try c.decodeIfPresent(String.self, forKey: key) ?? "" }
        id = try c.decode(String.self, forKey: .id)
        kind = try c.decodeIfPresent(Kind.self, forKey: .kind) ?? .offer
        header = try text(.header)
        question = try text(.question)
        label = try text(.label)
        description = try text(.description)
        quote = try text(.quote)
        options = (try? c.decodeIfPresent([OpenOption].self, forKey: .options)) ?? []
        multiSelect = try c.decodeIfPresent(Bool.self, forKey: .multiSelect) ?? false
        state = try c.decodeIfPresent(State.self, forKey: .state) ?? .open
        task = try c.decodeIfPresent(Int.self, forKey: .task)
        number = try c.decodeIfPresent(Int.self, forKey: .number) ?? 0
        at = (try? c.decodeIfPresent(String.self, forKey: .at)).flatMap { $0.flatMap(Date.parseRFC3339) }
    }
}

public struct OpenItems: Sendable, Equatable {
    public let items: [OpenItem]
    public let overflow: [Int]
    public let later: Bool

    public init(items: [OpenItem], overflow: [Int], later: Bool) {
        self.items = items.enumerated().map { index, item in
            var item = item
            if item.number == 0 { item.number = index + 1 }
            return item
        }
        self.overflow = overflow
        self.later = later
    }

    public var open: [OpenItem] {
        items.filter { $0.state == .open }
    }

    public var summary: String {
        let count = { (state: OpenItem.State) in self.items.count { $0.state == state } }
        let filed = items.compactMap(\.task) + overflow
        let parts = [
            count(.answered) > 0 ? "\(count(.answered)) answered" : nil,
            count(.no) > 0 ? "\(count(.no)) declined" : nil,
            filed.isEmpty ? nil : "\(filed.count) in the backlog: " + OpenCard.tasks(filed),
            count(.handed) > 0 ? "\(count(.handed)) handed to the parent" : nil,
        ]
        return parts.compactMap { $0 }.joined(separator: " · ")
    }
}

public struct OpenKinds: Sendable, Equatable {
    public let decisions: Int
    public let offers: Int

    public init(_ items: [OpenItem]) {
        decisions = items.count { $0.kind == .decision }
        offers = items.count { $0.kind == .offer }
    }

    public var text: String {
        let parts = [decisions > 0 ? (decisions == 1 ? "1 decision" : "\(decisions) decisions") : nil, offers > 0 ? (offers == 1 ? "1 offer" : "\(offers) offers") : nil]
        return parts.compactMap { $0 }.joined(separator: ", ")
    }
}

public enum OpenSection: Sendable, Equatable, Identifiable {
    case decision(OpenItem)
    case offers([OpenItem])

    public var id: String {
        switch self {
        case .decision(let item): item.id
        case .offers: "offers"
        }
    }

    public var number: Int {
        switch self {
        case .decision(let item): item.number
        case .offers(let items): items.first?.number ?? 0
        }
    }

    public var chip: String {
        switch self {
        case .decision(let item): item.header
        case .offers: "Next"
        }
    }

    public var title: String {
        switch self {
        case .decision(let item): item.question
        case .offers: "Tick what you want me to do"
        }
    }

    public var items: [OpenItem] {
        switch self {
        case .decision(let item): [item]
        case .offers(let items): items
        }
    }

    public var folded: String {
        switch self {
        case .decision(let item): item.question
        case .offers(let items): OpenKinds(items).text
        }
    }
}

public struct OpenCard: Sendable, Equatable {
    public let items: [OpenItem]
    public let sections: [OpenSection]
    public let overflow: [Int]
    public let later: Bool

    public init?(_ groups: [OpenItems]) {
        items = groups.flatMap(\.open)
        guard !items.isEmpty else { return nil }
        let offers = items.filter { $0.kind == .offer }
        sections = items.filter { $0.kind == .decision }.map(OpenSection.decision) + (offers.isEmpty ? [] : [.offers(offers)])
        overflow = groups.flatMap(\.overflow)
        later = groups.allSatisfy(\.later)
    }

    public var overflowLine: String? {
        overflow.isEmpty ? nil : "+\(overflow.count) more went to the backlog: " + Self.tasks(overflow)
    }

    static func tasks(_ ids: [Int]) -> String {
        ids.map { "#\($0)" }.joined(separator: ", ")
    }
}

public struct OpenDraft: Sendable, Equatable {
    public var picks: [String: Set<String>] = [:]
    public var others: [String: String] = [:]
    public var opened: Set<String> = []
    public var offers: Set<String> = []
    public var section: String?
    public var unfolded = false

    public init() {}

    public var isEmpty: Bool {
        self == OpenDraft()
    }

    public mutating func pick(_ option: OpenOption, in item: OpenItem) {
        var set = picks[item.id, default: []]
        if set.contains(option.label) {
            set.remove(option.label)
        } else if item.multiSelect {
            set.insert(option.label)
        } else {
            set = [option.label]
        }
        picks[item.id] = set.isEmpty ? nil : set
    }

    public mutating func toggleOther(_ item: OpenItem) {
        opened.formSymmetricDifference([item.id])
    }

    public mutating func toggleOffer(_ item: OpenItem) {
        offers.formSymmetricDifference([item.id])
    }

    public func other(_ item: OpenItem) -> String? {
        guard opened.contains(item.id), let text = others[item.id]?.trimmingCharacters(in: .whitespacesAndNewlines), !text.isEmpty else { return nil }
        return text
    }

    public func labels(_ item: OpenItem) -> [String] {
        item.options.filter { picks[item.id, default: []].contains($0.label) }.map(\.label)
    }

    public func answer(_ item: OpenItem) -> String? {
        if item.kind == .offer { return offers.contains(item.id) ? item.label : nil }
        let parts = labels(item) + (other(item).map { [$0] } ?? [])
        return parts.isEmpty ? nil : parts.joined(separator: ", ")
    }

    public func answered(_ items: [OpenItem]) -> [OpenItem] {
        items.filter { answer($0) != nil }
    }

    public func summary(_ section: OpenSection) -> String? {
        let answers = section.items.compactMap(answer)
        return answers.isEmpty ? nil : answers.joined(separator: ", ")
    }

    public mutating func clear(_ items: [OpenItem]) {
        for item in items {
            picks[item.id] = nil
            others[item.id] = nil
            opened.remove(item.id)
            offers.remove(item.id)
        }
    }

    func reply(session: String, items: [OpenItem], note: String) -> [String: JSONValue] {
        let answered = answered(items)
        var args: [String: JSONValue] = [
            "session": .string(session),
            "answers": .array(answered.filter { $0.kind == .decision }.map { item in
                var answer: [String: JSONValue] = ["item": .string(item.id)]
                let labels = labels(item)
                if item.multiSelect, !labels.isEmpty { answer["options"] = .array(labels.map(JSONValue.string)) }
                if !item.multiSelect, let label = labels.first { answer["option"] = .string(label) }
                if let other = other(item) { answer["other"] = .string(other) }
                return .object(answer)
            }),
            "offers": .array(answered.filter { $0.kind == .offer }.map { .string($0.id) }),
        ]
        if !note.isEmpty { args["note"] = .string(note) }
        return args
    }
}

public struct OpenQueueEntry: Decodable, Sendable, Equatable {
    public let session: String
    public let title: String
    public let items: [OpenItem]

    enum CodingKeys: String, CodingKey {
        case session, title, items
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        session = try c.decode(String.self, forKey: .session)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        items = OpenItems(items: (try c.decodeIfPresent([OpenItem].self, forKey: .items)) ?? [], overflow: [], later: false).items
    }
}

public struct OpenStep: Sendable, Equatable, Identifiable {
    public let session: String
    public let title: String
    public let item: OpenItem

    public var id: String { session + "/" + item.id }
}

public struct OpenPass: Sendable, Equatable {
    public private(set) var steps: [OpenStep]
    public private(set) var position = 0
    public private(set) var filed: Set<String> = []

    public init(_ queue: [OpenQueueEntry]) {
        steps = Self.steps(queue)
    }

    private static func steps(_ queue: [OpenQueueEntry]) -> [OpenStep] {
        queue.flatMap { entry in entry.items.map { OpenStep(session: entry.session, title: entry.title, item: $0) } }
    }

    public var current: OpenStep? {
        steps.indices.contains(position) ? steps[position] : nil
    }

    public var finished: Bool {
        steps.allSatisfy { filed.contains($0.id) }
    }

    public var atEnd: Bool {
        current.map { filed.contains($0.id) } ?? true
    }

    public var isLast: Bool {
        !steps[min(position + 1, steps.count)...].contains { !filed.contains($0.id) }
    }

    public var sessions: [String] {
        steps.map(\.session).reduce(into: []) { if !$0.contains($1) { $0.append($1) } }
    }

    public func items(_ session: String) -> [OpenItem] {
        steps.filter { $0.session == session && !filed.contains($0.id) }.map(\.item)
    }

    public var canGoBack: Bool {
        steps[..<min(position, steps.count)].contains { !filed.contains($0.id) }
    }

    public mutating func next() {
        if let index = steps.indices.first(where: { $0 > position && !filed.contains(steps[$0].id) }) { position = index }
    }

    public mutating func previous() {
        if let index = steps.indices.last(where: { $0 < position && !filed.contains(steps[$0].id) }) { position = index }
    }

    public mutating func file(_ step: OpenStep) {
        filed.insert(step.id)
    }

    public mutating func unfile(_ step: OpenStep) {
        filed.remove(step.id)
    }

    public mutating func update(_ queue: [OpenQueueEntry]) {
        let at = current?.id
        let known = Dictionary(Self.steps(queue).map { ($0.id, $0) }, uniquingKeysWith: { $1 })
        var kept = steps.compactMap { known[$0.id] ?? (filed.contains($0.id) ? $0 : nil) }
        let seen = Set(kept.map(\.id))
        kept += Self.steps(queue).filter { !seen.contains($0.id) }
        steps = kept
        position = kept.firstIndex { $0.id == at } ?? min(position, max(kept.count - 1, 0))
        if let current, filed.contains(current.id) { next() }
    }
}
