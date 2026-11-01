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

public struct DiffFile: Sendable, Hashable {
    public struct Line: Sendable, Hashable {
        public let text: String
        public let kind: DiffLine.Kind
        public let number: Int?
    }

    public let path: String?
    public private(set) var lines: [Line] = []
    public private(set) var added = 0
    public private(set) var removed = 0

    public var numberWidth: Int {
        lines.compactMap(\.number).max().map { String($0).count } ?? 0
    }

    public static func parse(_ text: String) -> [DiffFile] {
        var files: [DiffFile] = []
        var hunk: Hunk?
        var source: String?
        for line in DiffLine.parse(text).map(\.text) {
            if files.isEmpty { files.append(DiffFile(path: nil)) }
            if var body = hunk, body.open {
                files[files.count - 1].append(body.take(line))
                hunk = body
                continue
            }
            if let from = source {
                source = nil
                if line.hasPrefix("+++ ") {
                    let target = String(line.dropFirst(4))
                    let path = target == "/dev/null" ? strip(from, "a/") : strip(target, "b/")
                    if files.count == 1, files[0].lines.isEmpty { files.removeAll() }
                    files.append(DiffFile(path: path))
                    continue
                }
                files[files.count - 1].append(Line(text: "--- " + from, kind: .context, number: nil))
            }
            if line.hasPrefix("--- ") {
                source = String(line.dropFirst(4))
            } else if line.hasPrefix("@@") {
                hunk = Hunk(line)
                files[files.count - 1].append(Line(text: line, kind: .hunk, number: nil))
            } else {
                files[files.count - 1].append(Line(text: line, kind: .context, number: nil))
            }
        }
        if let from = source { files[files.count - 1].append(Line(text: "--- " + from, kind: .context, number: nil)) }
        return files.filter { $0.path != nil || !$0.lines.isEmpty }
    }

    private mutating func append(_ line: Line) {
        lines.append(line)
        if line.kind == .added { added += 1 }
        if line.kind == .removed { removed += 1 }
    }

    private static func strip(_ path: String, _ prefix: String) -> String {
        path.hasPrefix(prefix) ? String(path.dropFirst(prefix.count)) : path
    }

    private struct Hunk {
        var old: Int
        var new: Int
        var oldLeft: Int
        var newLeft: Int

        static let limit = 10_000_000

        var open: Bool { oldLeft > 0 || newLeft > 0 }

        init(_ header: String) {
            let fields = header.split(separator: " ")
            let range = { (sign: Character) -> (Int, Int) in
                guard let field = fields.first(where: { $0.first == sign }) else { return (0, 0) }
                let parts = field.dropFirst().split(separator: ",", omittingEmptySubsequences: false)
                let number = { (text: Substring?) in min(max(Int(text ?? "") ?? 0, 0), Self.limit) }
                return (number(parts.first), parts.count > 1 ? number(parts[1]) : 1)
            }
            (old, oldLeft) = range("-")
            (new, newLeft) = range("+")
        }

        mutating func take(_ line: String) -> Line {
            switch line.first {
            case "+":
                defer { new &+= 1; newLeft &-= 1 }
                return Line(text: String(line.dropFirst()), kind: .added, number: new)
            case "-":
                defer { old &+= 1; oldLeft &-= 1 }
                return Line(text: String(line.dropFirst()), kind: .removed, number: old)
            case "\\":
                return Line(text: line, kind: .context, number: nil)
            default:
                defer { old &+= 1; new &+= 1; oldLeft &-= 1; newLeft &-= 1 }
                return Line(text: line.hasPrefix(" ") ? String(line.dropFirst()) : line, kind: .context, number: new)
            }
        }
    }
}
