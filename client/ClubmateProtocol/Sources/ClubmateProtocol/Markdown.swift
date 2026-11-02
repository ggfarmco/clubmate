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

public enum MarkdownAlign: Sendable, Hashable {
    case none
    case left
    case center
    case right
}

public enum MarkdownBlock: Sendable, Hashable {
    case heading(level: Int, text: String)
    case paragraph(String)
    case list([MarkdownListItem])
    case quote(String)
    case table(header: [String], align: [MarkdownAlign], rows: [[String]])
    case code(language: String, text: String)
    case mermaid(String)
}

public enum Markdown {
    public static func blocks(_ source: String) -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var paragraph: [String] = []
        var items: [MarkdownListItem] = []
        var quote: [String] = []
        var table: (header: [String], align: [MarkdownAlign], rows: [[String]])?
        var fence: (marker: String, language: String, lines: [String])?

        func flush() {
            if !paragraph.isEmpty { blocks.append(.paragraph(paragraph.joined(separator: "\n"))) }
            if !items.isEmpty { blocks.append(.list(items)) }
            if !quote.isEmpty { blocks.append(.quote(quote.joined(separator: "\n"))) }
            if let open = table { blocks.append(.table(header: open.header, align: open.align, rows: open.rows)) }
            paragraph = []
            items = []
            quote = []
            table = nil
        }

        let lines = source.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        var index = 0
        while index < lines.count {
            let line = lines[index]
            index += 1
            if let open = fence {
                if line.trimmingCharacters(in: .whitespaces).hasPrefix(open.marker) {
                    let text = open.lines.joined(separator: "\n")
                    blocks.append(open.language.lowercased() == "mermaid" ? .mermaid(text) : .code(language: open.language, text: text))
                    fence = nil
                } else {
                    fence?.lines.append(line)
                }
                continue
            }
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if !quote.isEmpty, !trimmed.hasPrefix(">") { flush() }
            if let open = table {
                if trimmed.contains("|") {
                    table?.rows.append(fit(cells(trimmed), open.header.count))
                    continue
                }
                flush()
            }
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
            } else if trimmed.contains("|"), index < lines.count, let align = delimiter(lines[index]), cells(trimmed).count == align.count {
                flush()
                table = (cells(trimmed), align, [])
                index += 1
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

    public static func inline(_ text: String, docLinks: [String: String]) -> AttributedString {
        var content = inline(text)
        guard !docLinks.isEmpty else { return content }
        var linked: [(Range<AttributedString.Index>, URL)] = []
        for run in content.runs {
            let code = run.inlinePresentationIntent?.contains(.code) == true ? String(content[run.range].characters) : nil
            guard let key = run.link?.relativeString ?? code, let path = docLinks[key], let url = MarkdownDocLink.url(path) else { continue }
            linked.append((run.range, url))
        }
        for (range, url) in linked {
            content[range].link = url
        }
        return content
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

    private static func cells(_ row: String) -> [String] {
        var body = Substring(row)
        if body.hasPrefix("|") { body = body.dropFirst() }
        if body.hasSuffix("|"), !body.hasSuffix("\\|") { body = body.dropLast() }
        var cells: [String] = []
        var cell = ""
        var code = false
        var escaped = false
        for char in body {
            if escaped {
                cell.append(char == "|" ? "|" : "\\\(char)")
                escaped = false
            } else if char == "\\" {
                escaped = true
            } else if char == "`" {
                code.toggle()
                cell.append(char)
            } else if char == "|", !code {
                cells.append(cell.trimmingCharacters(in: .whitespaces))
                cell = ""
            } else {
                cell.append(char)
            }
        }
        if escaped { cell.append("\\") }
        cells.append(cell.trimmingCharacters(in: .whitespaces))
        return cells
    }

    private static func fit(_ row: [String], _ count: Int) -> [String] {
        Array(row.prefix(count)) + Array(repeating: "", count: max(0, count - row.count))
    }

    private static func delimiter(_ line: String) -> [MarkdownAlign]? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard trimmed.contains("-") else { return nil }
        var align: [MarkdownAlign] = []
        for cell in cells(trimmed) {
            let left = cell.hasPrefix(":")
            let right = cell.hasSuffix(":") && cell.count > 1
            let dashes = cell.dropFirst(left ? 1 : 0).dropLast(right ? 1 : 0)
            guard !dashes.isEmpty, dashes.allSatisfy({ $0 == "-" }) else { return nil }
            align.append(left && right ? .center : left ? .left : right ? .right : .none)
        }
        return align
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

public enum MarkdownDocLink {
    private static let scheme = "clubmate-doc"

    public static func url(_ target: String) -> URL? {
        let parts = target.split(separator: "#", maxSplits: 1, omittingEmptySubsequences: false)
        var components = URLComponents()
        components.scheme = scheme
        components.path = String(parts[0])
        components.fragment = parts.count > 1 ? String(parts[1]) : nil
        return components.url
    }

    public static func target(_ url: URL) -> String? {
        guard url.scheme?.lowercased() == scheme, let components = URLComponents(url: url, resolvingAgainstBaseURL: false), !components.path.isEmpty else { return nil }
        return components.fragment.map { "\(components.path)#\($0)" } ?? components.path
    }
}
