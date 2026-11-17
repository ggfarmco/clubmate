import Foundation

public enum AsideState: String, FallbackEnum {
    case open
    case thinking
    case asks
    case sent
    case other

    public static let fallback = Self.other
}

public struct AsideAnchor: Codable, Sendable, Hashable {
    public var elicitation: String?
    public var field: Int?
    public var open: String?
    public var option: String?
    public var question: String?
    public var message: String?
    public var quote: String?

    public init(elicitation: String? = nil, field: Int? = nil, open: String? = nil, option: String? = nil, question: String? = nil, message: String? = nil, quote: String? = nil) {
        self.elicitation = elicitation
        self.field = field
        self.open = open
        self.option = option
        self.question = question
        self.message = message
        self.quote = quote
    }

    public var pairs: [(key: String, value: String)] {
        [("elicitation", elicitation), ("field", field.map(String.init)), ("open", open), ("option", option), ("question", question), ("message", message), ("quote", quote)]
            .compactMap { key, value in value.map { (key, $0) } }
    }
}

public struct AsideInfo: Codable, Sendable, Hashable {
    public let state: AsideState
    public let label: String
    public let detail: String
    public let anchor: AsideAnchor

    public init(state: AsideState, label: String = "", detail: String = "", anchor: AsideAnchor = AsideAnchor()) {
        self.state = state
        self.label = label
        self.detail = detail
        self.anchor = anchor
    }

    enum CodingKeys: String, CodingKey {
        case state, label, detail, anchor
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        state = try c.decodeIfPresent(AsideState.self, forKey: .state) ?? .open
        label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
        detail = try c.decodeIfPresent(String.self, forKey: .detail) ?? ""
        anchor = (try? c.decodeIfPresent(AsideAnchor.self, forKey: .anchor)) ?? AsideAnchor()
    }
}

public struct ComposerDraft: Codable, Sendable, Hashable {
    public let text: String
    public let rev: Int

    public init(text: String, rev: Int) {
        self.text = text
        self.rev = rev
    }
}
