import SwiftUI

import ClubmateKit

struct PlanNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        let entries = node.planEntries()
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            Text(caption(entries))
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
            ForEach(Array(entries.enumerated()), id: \.offset) { _, entry in
                HStack(alignment: .firstTextBaseline, spacing: theme.spacing.l) {
                    Image(systemName: icon(entry.status))
                        .foregroundStyle(iconStyle(entry.status))
                    Text(entry.content)
                        .fontWeight(entry.status == .inProgress ? .semibold : .regular)
                        .foregroundStyle(textStyle(entry.status))
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .agentCard()
    }

    private func caption(_ entries: [SDUIPlanEntry]) -> String {
        guard !entries.isEmpty else { return "Plan" }
        return "Plan · \(entries.count { $0.status == .completed }) of \(entries.count)"
    }

    private func icon(_ status: PlanStatus) -> String {
        switch status {
        case .completed: "checkmark"
        case .inProgress: "play.circle"
        case .pending: "circle"
        }
    }

    private func iconStyle(_ status: PlanStatus) -> Color {
        switch status {
        case .completed: theme.muted
        case .inProgress: theme.accentInk
        case .pending: theme.faint
        }
    }

    private func textStyle(_ status: PlanStatus) -> Color {
        switch status {
        case .completed: theme.muted
        case .inProgress: theme.foreground
        case .pending: theme.text2
        }
    }
}
