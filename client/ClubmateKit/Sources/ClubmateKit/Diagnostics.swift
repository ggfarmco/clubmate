import Foundation

public struct DiagnosticsFile: Decodable, Sendable, Equatable {
    public let name: String
    public let bytes: Int64
}

public struct DiagnosticsResult: Decodable, Sendable {
    public let path: String
    public let files: [DiagnosticsFile]
}

public enum Diagnostics {
    public static let title = "Collect Diagnostics"
    public static let excluded = "Terminal output, environment variables and plugin storage are not included."
    public static let settingsName = "settings.json"
    public static let windowName = "window.png"
    public static let screenshotOption = "Include a screenshot of the window (may show terminal and editor contents)"
    public static let extraLimit = 10 << 20
    static let redacted = "<redacted>"
    static let secretMarkers = ["token", "secret", "password", "key"]

    public static func archiveName(_ date: Date, timeZone: TimeZone = .current) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = timeZone
        formatter.dateFormat = "yyyyMMdd-HHmmss"
        return "clubmate-diagnostics-\(formatter.string(from: date)).zip"
    }

    public static func expectedFiles(screenshot: Bool) -> [String] {
        ["info.json", "logs/core.log", "logs/plugins/<plugin>.log", "plugins.json", "workspaces.json", "sessions.json", "ui/<workspace>.json", settingsName]
            + (screenshot ? [windowName] : [])
            + ["README.txt"]
    }

    public static func summary(screenshot: Bool) -> String {
        (expectedFiles(screenshot: screenshot) + ["", excluded]).joined(separator: "\n")
    }

    public static func redactedSettings(_ domain: [String: Any]) -> Data {
        let safe = jsonSafe(domain)
        return (try? JSONSerialization.data(withJSONObject: safe, options: [.prettyPrinted, .sortedKeys])) ?? Data("{}".utf8)
    }

    private static func jsonSafe(_ value: Any) -> Any {
        switch value {
        case let fields as [String: Any]:
            return Dictionary(uniqueKeysWithValues: fields.map { key, value in
                (key, isSecret(key) ? redacted : jsonSafe(value))
            })
        case let items as [Any]:
            return items.map(jsonSafe)
        case let date as Date:
            return date.ISO8601Format()
        case let data as Data:
            return "<\(data.count) bytes>"
        case is String, is NSNumber:
            return value
        default:
            return "\(value)"
        }
    }

    private static func isSecret(_ key: String) -> Bool {
        let lower = key.lowercased()
        return secretMarkers.contains { lower.contains($0) }
    }
}
