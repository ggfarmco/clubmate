import Foundation

public struct RequestBanner: Sendable, Equatable {
    public let item: NeedsYouItem
    public let more: Int

    public init?(_ items: [NeedsYouItem], current: String?) {
        let shown = items.filter { !$0.offline && $0.id != current }
        guard let newest = shown.last else { return nil }
        item = newest
        more = shown.count - 1
    }

    public var moreText: String? {
        more > 0 ? "\(more) more" : nil
    }
}

extension NeedsYouItem {
    public static func id(computer: String, session: String) -> String {
        computer + "|" + session
    }
}
