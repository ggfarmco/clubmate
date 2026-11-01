import Foundation

public struct LongMessage: Equatable, Sendable {
    public static let previewLines = 12
    static let maxLines = 40
    static let maxCharacters = 4000
    static let previewCharacters = 1200

    public let preview: String
    public let lines: Int

    public init?(_ text: String) {
        let lines = text.reversed().drop { $0.isNewline }.reduce(1) { $1 == "\n" ? $0 + 1 : $0 }
        guard lines > Self.maxLines || text.count > Self.maxCharacters else { return nil }
        self.lines = lines
        preview = String(text.split(separator: "\n", maxSplits: Self.previewLines, omittingEmptySubsequences: false).prefix(Self.previewLines).joined(separator: "\n").prefix(Self.previewCharacters))
    }
}
