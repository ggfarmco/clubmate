import Foundation

public struct RequestBanner: Sendable, Equatable {
    public let item: NeedsYouItem
    public let more: Int
    public let keys: Set<String>

    public init?(_ items: [NeedsYouItem], current: String?, dismissed: Set<String> = []) {
        let shown = items.filter { !$0.offline && $0.id != current && !dismissed.contains($0.requestKey) }
        guard let newest = shown.last else { return nil }
        item = newest
        more = shown.count - 1
        keys = Set(shown.map(\.requestKey))
    }

    public var moreText: String? {
        more > 0 ? "\(more) more" : nil
    }
}

extension NeedsYouItem {
    public static func id(computer: String, session: String) -> String {
        computer + "|" + session
    }

    public var requestKey: String {
        let session = self.session
        if !session.pending.isEmpty { return id + "|" + session.pending.joined(separator: ",") }
        if let summary = session.pendingSummary { return id + "|" + String(summary.at.timeIntervalSince1970) }
        return id + "|" + (session.askedQuestion ?? "")
    }
}
