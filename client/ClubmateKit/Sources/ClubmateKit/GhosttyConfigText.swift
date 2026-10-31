import Foundation

public struct GhosttyConfigText: Sendable {
    private let entries: [(key: String, value: String)]

    public init(_ texts: [String]) {
        entries = texts.flatMap(Self.parse)
    }

    public func first(_ key: String) -> String? {
        let values = entries.filter { $0.key == key }.map(\.value)
        let reset = values.lastIndex(of: "").map { $0 + 1 } ?? 0
        return values[reset...].first
    }

    public func last(_ key: String) -> String? {
        entries.last { $0.key == key }.flatMap { $0.value.isEmpty ? nil : $0.value }
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
