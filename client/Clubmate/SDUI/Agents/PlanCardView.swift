import SwiftUI

import ClubmateKit

struct PlanNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            CardHeading(text: "Plan")
            ForEach(Array(node.planEntries().enumerated()), id: \.offset) { _, entry in
                HStack(alignment: .firstTextBaseline, spacing: theme.spacing.l) {
                    Image(systemName: icon(entry.status))
                        .foregroundStyle(entry.status == .inProgress ? theme.accent : theme.muted)
                    Text(entry.content)
                        .strikethrough(entry.status == .completed)
                        .fontWeight(entry.status == .inProgress ? .semibold : .regular)
                        .foregroundStyle(entry.status == .completed ? theme.muted : theme.foreground)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .agentCard()
    }

    private func icon(_ status: PlanStatus) -> String {
        switch status {
        case .completed: "checkmark.circle.fill"
        case .inProgress: "play.circle"
        case .pending: "circle"
        }
    }
}

private struct CardHeading: View {
    let text: String

    @Environment(\.theme) private var theme

    var body: some View {
        Text(text.uppercased())
            .font(theme.uiSmall.weight(.semibold))
            .foregroundStyle(theme.muted)
    }
}
