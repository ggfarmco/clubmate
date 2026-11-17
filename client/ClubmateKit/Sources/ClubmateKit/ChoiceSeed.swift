public struct ChoiceSeed: Equatable, Sendable {
    public var picks: [String]
    public var other: String?

    public init(picks: [String], other: String?) {
        self.picks = picks
        self.other = other
    }

    public static func resolve(draft: ChoiceSeed?, server: ChoiceSeed, seen: ChoiceSeed?) -> ChoiceSeed {
        guard let draft, seen == nil || seen == server else { return server }
        return draft
    }
}
