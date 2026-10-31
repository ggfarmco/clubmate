import Foundation

public struct MarkdownListItem: Sendable, Hashable {
    public let marker: String
    public let depth: Int
    public let text: String

    public init(marker: String, depth: Int, text: String) {
        self.marker = marker
        self.depth = depth
        self.text = text
    }
}

public enum MarkdownBlock: Sendable, Hashable {
    case heading(level: Int, text: String)
    case paragraph(String)
    case list([MarkdownListItem])
    case quote(String)
    case code(language: String, text: String)
}

public enum Markdown {
    public static func blocks(_ source: String) -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var paragraph: [String] = []
        var items: [MarkdownListItem] = []
        var quote: [String] = []
        var fence: (marker: String, language: String, lines: [String])?

        func flush() {
            if !paragraph.isEmpty { blocks.append(.paragraph(paragraph.joined(separator: "\n"))) }
            if !items.isEmpty { blocks.append(.list(items)) }
            if !quote.isEmpty { blocks.append(.quote(quote.joined(separator: "\n"))) }
            paragraph = []
            items = []
            quote = []
        }

        for line in source.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            if let open = fence {
                if line.trimmingCharacters(in: .whitespaces).hasPrefix(open.marker) {
                    blocks.append(.code(language: open.language, text: open.lines.joined(separator: "\n")))
                    fence = nil
                } else {
                    fence?.lines.append(line)
                }
                continue
            }
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if !quote.isEmpty, !trimmed.hasPrefix(">") { flush() }
            if let marker = fenceMarker(trimmed) {
                flush()
                fence = (marker, String(trimmed.dropFirst(marker.count)).trimmingCharacters(in: .whitespaces), [])
            } else if trimmed.isEmpty {
                flush()
            } else if let heading = heading(trimmed) {
                flush()
                blocks.append(heading)
            } else if trimmed.hasPrefix(">") {
                if quote.isEmpty { flush() }
                quote.append(trimmed.dropFirst().trimmingCharacters(in: .whitespaces))
            } else if let item = listItem(line) {
                if !paragraph.isEmpty {
                    blocks.append(.paragraph(paragraph.joined(separator: "\n")))
                    paragraph = []
                }
                items.append(item)
            } else if !items.isEmpty, line.first?.isWhitespace == true, let last = items.popLast() {
                items.append(MarkdownListItem(marker: last.marker, depth: last.depth, text: last.text + "\n" + trimmed))
            } else {
                if !items.isEmpty {
                    blocks.append(.list(items))
                    items = []
                }
                paragraph.append(line)
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

    private static func fenceMarker(_ line: String) -> String? {
        for marker in ["```", "~~~"] where line.hasPrefix(marker) {
            return marker
        }
        return nil
    }

    private static func heading(_ line: String) -> MarkdownBlock? {
        let hashes = line.prefix { $0 == "#" }.count
        guard (1...6).contains(hashes) else { return nil }
        let rest = line.dropFirst(hashes)
        guard rest.isEmpty || rest.first == " " else { return nil }
        return .heading(level: hashes, text: rest.trimmingCharacters(in: .whitespaces))
    }

    private static func listItem(_ line: String) -> MarkdownListItem? {
        let indent = line.prefix { $0 == " " || $0 == "\t" }
        let width = indent.reduce(0) { $0 + ($1 == "\t" ? 4 : 1) }
        let body = line.dropFirst(indent.count)
        let depth = width / 2
        if let first = body.first, "-*+".contains(first), body.dropFirst().first == " " {
            return MarkdownListItem(marker: "•", depth: depth, text: body.dropFirst(2).trimmingCharacters(in: .whitespaces))
        }
        let digits = body.prefix { $0.isASCII && $0.isNumber }
        guard !digits.isEmpty, digits.count <= 9 else { return nil }
        let after = body.dropFirst(digits.count)
        guard let close = after.first, close == "." || close == ")", after.dropFirst().first == " " else { return nil }
        return MarkdownListItem(marker: digits + ".", depth: depth, text: after.dropFirst(2).trimmingCharacters(in: .whitespaces))
    }
}

public enum MarkdownLinkPolicy {
    private static let schemes: Set<String> = ["http", "https", "mailto"]

    public static func allows(_ url: URL) -> Bool {
        url.scheme.map { schemes.contains($0.lowercased()) } ?? false
    }
}
