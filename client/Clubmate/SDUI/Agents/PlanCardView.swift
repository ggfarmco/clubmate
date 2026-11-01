import SwiftUI

import ClubmateKit

struct PlanNodeView: View {
    let node: SDUINode

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        let entries = node.planEntries()
        let fold = PlanFold(entries.map(\.status))
        let showingDone = node.id.map { MessageExpansion.shared.expanded.contains($0) } ?? true
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            HStack(spacing: theme.spacing.m) {
                Text(caption(fold))
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                Spacer(minLength: 0)
                if fold.folds, let id = node.id {
                    Button { MessageExpansion.shared.toggle(id) } label: {
                        Text(showingDone ? "Hide done" : "Show done")
                            .font(theme.uiSmall)
                            .foregroundStyle(hovering ? theme.foreground : theme.text2)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .onHover { hovering = $0 }
                }
            }
            ForEach(Array(entries.enumerated()).filter { fold.shows($0.element.status, showingDone: showingDone) }, id: \.offset) { _, entry in
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

    private func caption(_ fold: PlanFold) -> String {
        fold.total == 0 ? "Plan" : "Plan · \(fold.done) of \(fold.total)"
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
