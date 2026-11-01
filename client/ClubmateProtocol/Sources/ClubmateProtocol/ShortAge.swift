import Foundation

public enum ShortAge {
    public static func since(_ date: Date, now: Date) -> String {
        let seconds = max(0, Int(now.timeIntervalSince(date)))
        switch seconds {
        case ..<60: return "\(seconds) s"
        case ..<3_600: return "\(seconds / 60) min"
        case ..<86_400: return "\(seconds / 3_600) h"
        default: return "\(seconds / 86_400) d"
        }
    }

    public static func thought(_ seconds: Int) -> String {
        let seconds = max(1, seconds)
        return seconds < 60 ? "Thought for \(seconds) s" : "Thought for \(seconds / 60) min"
    }
}
