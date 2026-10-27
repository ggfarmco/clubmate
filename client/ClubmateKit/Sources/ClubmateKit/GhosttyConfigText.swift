import Foundation

public struct GhosttyConfigText: Sendable {
    private let entries: [(key: String, value: String)]

    public init(_ texts: [String]) {
        entries = texts.flatMap(Self.parse)
    }

    public func last(_ key: String) -> String? {
        entries.last { $0.key == key }.flatMap { $0.value.isEmpty ? nil : $0.value }
    }

    public func first(_ key: String) -> String? {
        let values = entries.filter { $0.key == key }.map(\.value)
        let reset = values.lastIndex(of: "").map { $0 + 1 } ?? 0
        return values[reset...].first
    }

    public static func themeName(_ value: String, dark: Bool) -> String {
        let prefix = dark ? "dark:" : "light:"
        let parts = value.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
        guard parts.contains(where: { $0.hasPrefix("dark:") || $0.hasPrefix("light:") }) else { return value }
        return parts.first { $0.hasPrefix(prefix) }.map { String($0.dropFirst(prefix.count)) } ?? ""
    }

    public static func hexColor(_ value: String) -> UInt32? {
        var hex = value.hasPrefix("#") ? String(value.dropFirst()) : value
        if hex.count == 3 { hex = hex.map { "\($0)\($0)" }.joined() }
        guard hex.count == 6 else { return nil }
        return UInt32(hex, radix: 16)
    }

    private static func parse(_ text: String) -> [(key: String, value: String)] {
        text.split(whereSeparator: \.isNewline).compactMap { line in
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard !trimmed.hasPrefix("#"), let eq = trimmed.firstIndex(of: "=") else { return nil }
            let key = trimmed[..<eq].trimmingCharacters(in: .whitespaces)
            var value = trimmed[trimmed.index(after: eq)...].trimmingCharacters(in: .whitespaces)
            if value.count >= 2, value.hasPrefix("\""), value.hasSuffix("\"") {
                value = String(value.dropFirst().dropLast())
            }
            return (key, value)
        }
    }
}
