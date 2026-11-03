import Foundation

public enum GhosttyConfigFile {
    public static let importHeader = "# Imported from "

    static let colourKeys: Set<String> = [
        "theme", "background", "foreground", "palette", "cursor-color", "cursor-text", "selection-background", "selection-foreground",
    ]

    public static func setting(_ key: String, to value: String?, in text: String) -> String {
        var lines = text.isEmpty ? [] : text.components(separatedBy: "\n")
        if lines.last == "" { lines.removeLast() }
        let at = lines.firstIndex { Self.key(of: $0) == key }
        lines.removeAll { Self.key(of: $0) == key }
        if let value {
            lines.insert("\(key) = \(value)", at: min(at ?? lines.count, lines.count))
        }
        return lines.isEmpty ? "" : lines.joined(separator: "\n") + "\n"
    }

    public static func imported(_ source: URL, text: String, on date: Date) -> String {
        let day = date.formatted(.iso8601.year().month().day())
        let directory = source.deletingLastPathComponent()
        var out = ["\(importHeader)\(source.path(percentEncoded: false)) on \(day)"]
        for line in text.components(separatedBy: "\n") {
            guard let key = key(of: line) else {
                out.append(line)
                continue
            }
            if colourKeys.contains(key) {
                out.append("# \(line.trimmingCharacters(in: .whitespaces))  (Clubmate theme)")
            } else if key == "config-file" {
                out.append("config-file = \(absolute(value(of: line), in: directory))")
            } else {
                out.append(line)
            }
        }
        while out.last?.isEmpty == true { out.removeLast() }
        return out.joined(separator: "\n") + "\n"
    }

    public static func appending(_ block: String, to text: String) -> String {
        guard !text.isEmpty else { return block }
        return (text.hasSuffix("\n") ? text : text + "\n") + "\n" + block
    }

    private static func key(of line: String) -> String? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.hasPrefix("#"), let eq = trimmed.firstIndex(of: "=") else { return nil }
        return trimmed[..<eq].trimmingCharacters(in: .whitespaces)
    }

    private static func value(of line: String) -> String {
        guard let eq = line.firstIndex(of: "=") else { return "" }
        return line[line.index(after: eq)...].trimmingCharacters(in: .whitespaces)
    }

    private static func absolute(_ value: String, in directory: URL) -> String {
        var path = value
        let optional = path.hasPrefix("?")
        if optional { path.removeFirst() }
        if path.count >= 2, path.hasPrefix("\""), path.hasSuffix("\"") { path = String(path.dropFirst().dropLast()) }
        if !path.hasPrefix("/"), !path.hasPrefix("~") {
            path = directory.appending(path: path).standardizedFileURL.path(percentEncoded: false)
        }
        return (optional ? "?" : "") + "\"\(path)\""
    }
}
