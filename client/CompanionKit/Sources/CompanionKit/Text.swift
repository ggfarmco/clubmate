import Foundation

public struct DiffLine: Sendable, Equatable, Hashable {
    public enum Kind: Sendable, Equatable, Hashable {
        case context
        case added
        case removed
    }

    public let kind: Kind
    public let text: String

    public init(_ kind: Kind, _ text: String) {
        self.kind = kind
        self.text = text
    }
}

public enum LineDiff {
    private static let maxCells = 250_000

    public static func lines(old: String, new: String) -> [DiffLine] {
        let a = split(old)
        let b = split(new)
        guard a.count * b.count <= maxCells else {
            return a.map { DiffLine(.removed, $0) } + b.map { DiffLine(.added, $0) }
        }
        var table = Array(repeating: Array(repeating: 0, count: b.count + 1), count: a.count + 1)
        for i in stride(from: a.count - 1, through: 0, by: -1) {
            for j in stride(from: b.count - 1, through: 0, by: -1) {
                table[i][j] = a[i] == b[j] ? table[i + 1][j + 1] + 1 : max(table[i + 1][j], table[i][j + 1])
            }
        }
        var out: [DiffLine] = []
        var i = 0
        var j = 0
        while i < a.count || j < b.count {
            if i < a.count, j < b.count, a[i] == b[j] {
                out.append(DiffLine(.context, a[i]))
                i += 1
                j += 1
            } else if i < a.count, j == b.count || table[i + 1][j] >= table[i][j + 1] {
                out.append(DiffLine(.removed, a[i]))
                i += 1
            } else {
                out.append(DiffLine(.added, b[j]))
                j += 1
            }
        }
        return out
    }

    private static func split(_ text: String) -> [String] {
        guard !text.isEmpty else { return [] }
        var lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        if lines.last == "" { lines.removeLast() }
        return lines
    }
}

public enum MarkdownBlock: Sendable, Equatable, Hashable {
    case heading(level: Int, text: String)
    case paragraph(String)
    case bullet(marker: String, depth: Int, text: String)
    case code(language: String, text: String)
    case quote(String)
}

public enum Markdown {
    public static func blocks(_ source: String) -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var paragraph: [String] = []
        var fence: (marker: String, language: String, lines: [String])?

        func flush() {
            if !paragraph.isEmpty { blocks.append(.paragraph(paragraph.joined(separator: "\n"))) }
            paragraph = []
        }

        for line in source.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if let open = fence {
                if trimmed.hasPrefix(open.marker) {
                    blocks.append(.code(language: open.language, text: open.lines.joined(separator: "\n")))
                    fence = nil
                } else {
                    fence?.lines.append(line)
                }
                continue
            }
            if let marker = ["```", "~~~"].first(where: { trimmed.hasPrefix($0) }) {
                flush()
                fence = (marker, trimmed.dropFirst(marker.count).trimmingCharacters(in: .whitespaces), [])
            } else if trimmed.isEmpty {
                flush()
            } else if let heading = heading(trimmed) {
                flush()
                blocks.append(heading)
            } else if trimmed.hasPrefix(">") {
                flush()
                blocks.append(.quote(trimmed.dropFirst().trimmingCharacters(in: .whitespaces)))
            } else if let item = bullet(line) {
                flush()
                blocks.append(item)
            } else {
                paragraph.append(trimmed)
            }
        }
        if let open = fence { blocks.append(.code(language: open.language, text: open.lines.joined(separator: "\n"))) }
        flush()
        return blocks
    }

    public static func inline(_ text: String) -> AttributedString {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        return (try? AttributedString(markdown: text, options: options)) ?? AttributedString(text)
    }

    private static func heading(_ line: String) -> MarkdownBlock? {
        let hashes = line.prefix { $0 == "#" }.count
        guard (1...6).contains(hashes) else { return nil }
        let rest = line.dropFirst(hashes)
        guard rest.first == " " else { return nil }
        return .heading(level: hashes, text: rest.trimmingCharacters(in: .whitespaces))
    }

    private static func bullet(_ line: String) -> MarkdownBlock? {
        let indent = line.prefix { $0 == " " || $0 == "\t" }
        let depth = indent.reduce(0) { $0 + ($1 == "\t" ? 4 : 1) } / 2
        let body = line.dropFirst(indent.count)
        if let first = body.first, "-*+".contains(first), body.dropFirst().first == " " {
            return .bullet(marker: "•", depth: depth, text: body.dropFirst(2).trimmingCharacters(in: .whitespaces))
        }
        let digits = body.prefix { $0.isASCII && $0.isNumber }
        guard !digits.isEmpty, digits.count <= 9 else { return nil }
        let after = body.dropFirst(digits.count)
        guard let close = after.first, close == "." || close == ")", after.dropFirst().first == " " else { return nil }
        return .bullet(marker: digits + ".", depth: depth, text: after.dropFirst(2).trimmingCharacters(in: .whitespaces))
    }
}
