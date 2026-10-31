public struct DiffLine: Sendable, Hashable {
    public enum Kind: Sendable, Hashable {
        case added
        case removed
        case hunk
        case context
    }

    public let text: String
    public let kind: Kind

    public init(text: String, kind: Kind) {
        self.text = text
        self.kind = kind
    }

    public static func parse(_ text: String) -> [DiffLine] {
        var lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        if lines.last == "" { lines.removeLast() }
        return lines.map { DiffLine(text: $0, kind: kind(of: $0)) }
    }

    private static func kind(of line: String) -> Kind {
        if line.hasPrefix("@@") { return .hunk }
        if line.hasPrefix("+"), !line.hasPrefix("+++") { return .added }
        if line.hasPrefix("-"), !line.hasPrefix("---") { return .removed }
        return .context
    }
}
