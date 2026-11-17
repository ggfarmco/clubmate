import Foundation

import ClubmateProtocol

public enum AsideCommand {
    public static let ask = "agents.aside"
    public static let summary = "agents.aside-summary"
    public static let send = "agents.aside-send"
    public static let pick = "agents.aside-pick"
    public static let keep = "agents.aside-keep"
    public static let busy = "agents.busy"
}

public enum AsideTarget: String, Sendable, CaseIterable, Identifiable {
    case answer
    case draft

    public var id: String { rawValue }
}

public enum AsidePresentation: Sendable, Equatable {
    case sheet(String)
    case full(String)
    case kept(String)

    public var aside: String {
        switch self {
        case .sheet(let id), .full(let id), .kept(let id): id
        }
    }
}

public struct AsidePrefill: Decodable, Sendable, Equatable {
    public var elicitation: String?
    public var field: String?
    public var open: String?
    public var value: String
    public var note: String

    public init(elicitation: String? = nil, field: String? = nil, open: String? = nil, value: String, note: String = "") {
        self.elicitation = elicitation
        self.field = field
        self.open = open
        self.value = value
        self.note = note
    }

    enum CodingKeys: String, CodingKey {
        case elicitation, field, open, value, note
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        elicitation = try c.decodeIfPresent(String.self, forKey: .elicitation)
        field = try c.decodeIfPresent(String.self, forKey: .field)
        open = try c.decodeIfPresent(String.self, forKey: .open)
        value = try c.decodeIfPresent(String.self, forKey: .value) ?? ""
        note = try c.decodeIfPresent(String.self, forKey: .note) ?? ""
    }

    var key: String {
        [elicitation ?? "", field ?? "", open ?? ""].joined(separator: "|")
    }
}

public struct OptionAside: Decodable, Sendable, Equatable {
    public var elicitation: String?
    public var field: String?
    public var open: String?
    public var option: String
    public var id: String
    public var state: AsideState

    public init(elicitation: String? = nil, field: String? = nil, open: String? = nil, option: String, id: String, state: AsideState) {
        self.elicitation = elicitation
        self.field = field
        self.open = open
        self.option = option
        self.id = id
        self.state = state
    }
}

public struct PinnedQuestion: Sendable, Equatable {
    public struct Option: Sendable, Equatable, Identifiable {
        public let value: String
        public let label: String
        public let description: String
        public let on: Bool
        public let aside: Bool

        public var id: String { value }
    }

    public let id: String
    public let title: String
    public let text: String
    public let multi: Bool
    public let options: [Option]
    public let other: Bool
    public let otherOn: Bool

    init?(node: JSONValue) {
        guard node["type"]?.stringValue == "choice", let props = node["props"], let options = props["options"]?.arrayValue else { return nil }
        id = node["id"]?.stringValue ?? ""
        title = props["title"]?.stringValue ?? ""
        text = props["text"]?.stringValue ?? ""
        multi = props["style"]?.stringValue == "check"
        self.options = options.map { option in
            Option(
                value: option["value"]?.stringValue ?? "",
                label: option["label"]?.stringValue ?? "",
                description: option["description"]?.stringValue ?? "",
                on: option["on"]?.boolValue ?? false,
                aside: option["aside"]?["id"]?.stringValue != nil
            )
        }
        other = props["other"] != nil && props["other"] != .null
        otherOn = props["other"]?["on"]?.boolValue ?? false
    }
}

public struct AsidePanel: Decodable, Sendable, Equatable {
    public var question = 0
    public var option = 0
    public var header = ""
    public var text = ""
    public var label = ""
    public var description = ""
    public var answer = false
    public var multi = false

    public init(question: Int = 0, option: Int = 0, header: String = "", text: String = "", label: String = "", description: String = "", answer: Bool = false, multi: Bool = false) {
        self.question = question
        self.option = option
        self.header = header
        self.text = text
        self.label = label
        self.description = description
        self.answer = answer
        self.multi = multi
    }

    enum CodingKeys: String, CodingKey {
        case question, option, header, text, label, description, answer, multi
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        question = try c.decodeIfPresent(Int.self, forKey: .question) ?? 0
        option = try c.decodeIfPresent(Int.self, forKey: .option) ?? 0
        header = try c.decodeIfPresent(String.self, forKey: .header) ?? ""
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
        description = try c.decodeIfPresent(String.self, forKey: .description) ?? ""
        answer = try c.decodeIfPresent(Bool.self, forKey: .answer) ?? false
        multi = try c.decodeIfPresent(Bool.self, forKey: .multi) ?? false
    }

    public var isOption: Bool { option > 0 }

    public var place: String {
        isOption ? "question \(question) · option \(option)" : "a passage"
    }
}

extension AgentSession {
    public var isAside: Bool { aside != nil }
}

extension ComposerDraft {
    public func apply(local: String, seen: Int?) -> String? {
        guard let seen else { return local.isEmpty ? text : nil }
        return rev > seen ? text : nil
    }
}

extension ElicitationDraft {
    public func merging(_ prefill: AsidePrefill, fields: [Elicitation.Field]) -> ElicitationDraft {
        let questions = Elicitation.questions(fields)
        guard let index = questions.firstIndex(where: { $0.field.key == prefill.field }) else { return self }
        let question = questions[index]
        var draft = self
        var picks: Set<String> = question.field.type == "multi" ? draft.picks[question.field.key, default: []] : []
        picks.insert(prefill.value)
        draft.picks[question.field.key] = picks
        if let other = question.other, !prefill.note.isEmpty {
            draft.texts[other.key] = prefill.note
            draft.opened.insert(other.key)
        }
        draft.page = index
        return draft
    }
}

extension OpenDraft {
    public func merging(_ prefill: AsidePrefill, item: OpenItem) -> OpenDraft {
        var draft = self
        var picks: Set<String> = item.multiSelect ? draft.picks[item.id, default: []] : []
        if !prefill.value.isEmpty { picks.insert(prefill.value) }
        draft.picks[item.id] = picks.isEmpty ? nil : picks
        if !prefill.note.isEmpty {
            draft.others[item.id] = prefill.note
            draft.opened.insert(item.id)
        }
        draft.section = item.id
        return draft
    }
}

extension AgentTranscript {
    public func optionAside(elicitation: String, field: String, option: String) -> OptionAside? {
        optionAsides.first { $0.elicitation == elicitation && $0.field == field && $0.option == option }
    }

    public func optionAside(open item: String, option: String) -> OptionAside? {
        optionAsides.first { $0.open == item && $0.option == option }
    }

    public var agentMessages: [TranscriptEntry] {
        entries.filter { if case .agent = $0.content { true } else { false } }
    }
}
