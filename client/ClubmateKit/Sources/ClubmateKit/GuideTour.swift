import Foundation

import ClubmateProtocol

public enum GuideTour {
    public static let paneWidth = 960.0
    public static let tourType = "guide.tour"
    public static let continueAction = "continue"

    public static func isTour(_ aside: SDUINode?) -> Bool {
        aside?.type == tourType
    }

    public static func asideVisible(shown: Bool, aside: SDUINode?, main: Double) -> Bool {
        shown && (!isTour(aside) || main >= paneWidth)
    }

    public static func paneShown(shown: Bool, aside: SDUINode?, main: Double) -> Bool {
        isTour(aside) && asideVisible(shown: shown, aside: aside, main: main)
    }

    public static func stepAction(in actions: [SDUIStepAction], keyCode: UInt16, command: Bool, control: Bool, option: Bool, shift: Bool) -> SDUIStepAction? {
        actions.first { action in
            action.enabled && action.id != continueAction
                && action.shortcut.flatMap(KeyCombo.init)?.matches(keyCode: keyCode, command: command, control: control, option: option, shift: shift) == true
        }
    }
}

public struct TourSelection: Equatable, Sendable {
    public private(set) var selected: String?
    public private(set) var opened: String?

    public init(selected: String? = nil) {
        self.selected = selected
    }

    public func at(_ ids: [String], current: String?) -> String? {
        if let selected, ids.contains(selected) { return selected }
        return [current].compactMap { $0 }.first { ids.contains($0) } ?? ids.first
    }

    public mutating func move(_ delta: Int, in ids: [String], current: String?) {
        guard let from = at(ids, current: current), let index = ids.firstIndex(of: from) else { return }
        let next = ids[min(max(index + delta, 0), ids.count - 1)]
        if next != selected { opened = nil }
        selected = next
    }

    public mutating func select(_ id: String) {
        if id != selected { opened = nil }
        selected = id
    }

    public mutating func toggleOffers(for id: String?) {
        guard let id else { return }
        selected = id
        opened = opened == id ? nil : id
    }

    public mutating func closeOffers() {
        opened = nil
    }
}
