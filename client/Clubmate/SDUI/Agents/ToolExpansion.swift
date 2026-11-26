import Observation
import SwiftUI

import ClubmateKit

@MainActor @Observable final class ToolExpansion {
    static let shared = ToolExpansion()

    private(set) var state = ToolExpansionState()
    private(set) var opened: String?

    func toggle(card id: String) {
        Motion.disclose { opened = state.toggle(card: id) ? id : nil }
    }

    func set(card id: String, expanded: Bool) {
        if state.isExpanded(card: id) != expanded { toggle(card: id) }
    }

    func toggle(group ids: [String]) {
        Motion.disclose { opened = state.toggle(group: ids) ? ids.first : nil }
    }

    func prune(_ transcript: SDUINode) {
        var next = state
        next.prune(transcript)
        if next != state { state = next }
    }
}

@MainActor enum TranscriptFolding {
    static var folds: TranscriptFolds {
        TranscriptFolds(tools: ToolExpansion.shared.state, messages: MessageExpansion.shared.expanded, thoughts: MessageExpansion.shared.thoughts)
    }

    static func set(_ block: TranscriptBlock, expanded: Bool, in transcript: SDUINode) {
        switch block.kind {
        case .actions:
            var ids: [String] = []
            if case let .actions(_, cards)? = transcript.transcriptItems().first(where: { $0.id == block.id }) { ids = cards.map(\.id) }
            if ToolExpansion.shared.state.isExpanded(group: ids) != expanded { ToolExpansion.shared.toggle(group: ids) }
        case .action:
            ToolExpansion.shared.set(card: block.id, expanded: expanded)
        case .thought:
            MessageExpansion.shared.set(thought: block.id, expanded: expanded)
        case .prompt, .plan, .answer:
            if MessageExpansion.shared.expanded.contains(block.id) != expanded { MessageExpansion.shared.toggle(block.id) }
        case .card, .earlier:
            break
        }
    }
}

@MainActor @Observable final class TranscriptBar {
    static let shared = TranscriptBar()

    var block: String?
}

struct BlockBar: View {
    let id: String

    @Environment(\.theme) private var theme

    var body: some View {
        theme.accent
            .frame(width: 2)
            .padding(.vertical, -theme.spacing.xs)
            .offset(x: -theme.gutter)
            .allowsHitTesting(false)
            .testNode(Self.probe, role: "bar", label: id)
    }

    static let probe = "transcript.bar"
}

extension View {
    func blockBar(_ id: String?) -> some View {
        overlay(alignment: .leading) {
            if let id, TranscriptBar.shared.block == id { BlockBar(id: id) }
        }
    }
}
