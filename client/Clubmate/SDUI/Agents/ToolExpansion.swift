import Observation

import ClubmateKit

@MainActor @Observable final class ToolExpansion {
    static let shared = ToolExpansion()

    private(set) var state = ToolExpansionState()
    private(set) var opened: String?

    func toggle(card id: String) {
        opened = state.toggle(card: id) ? id : nil
    }

    func set(card id: String, expanded: Bool) {
        if state.isExpanded(card: id) != expanded { toggle(card: id) }
    }

    func toggle(group ids: [String]) {
        opened = state.toggle(group: ids) ? ids.first : nil
    }

    func prune(_ transcript: SDUINode) {
        var next = state
        next.prune(transcript)
        if next != state { state = next }
    }
}
