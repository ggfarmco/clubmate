public enum Popups {
    public static let name = "popup"

    public static func slots(_ sections: [SectionInfo]) -> [String] {
        sections.filter { $0.slots.contains(name) }.map { "\($0.id).\(name)" }
    }

    public static func current(_ views: [String: SlotView], order: [String]) -> String? {
        order.last { views[$0].map(isOpen) ?? false }
    }

    static func isOpen(_ view: SlotView) -> Bool {
        view.tree.type != "empty"
    }
}

public struct PopupPresentation: Equatable, Sendable {
    public enum Change: Equatable, Sendable {
        case opened(String)
        case switched(String)
        case closed(String)
    }

    public private(set) var slot: String?

    public init() {}

    public var presented: Bool { slot != nil }

    public mutating func update(_ current: String?) -> Change? {
        guard current != slot else { return nil }
        let previous = slot
        slot = current
        switch (previous, current) {
        case (nil, let current?): return .opened(current)
        case (let previous?, nil): return .closed(previous)
        case (_, let current?): return .switched(current)
        default: return nil
        }
    }
}
