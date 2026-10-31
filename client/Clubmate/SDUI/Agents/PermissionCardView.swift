import SwiftUI

import ClubmateKit

struct PermissionCardNodeView: View {
    let node: SDUINode

    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        let outcome = node.string("outcome")
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.l) {
                Image(systemName: "hand.raised.fill")
                    .foregroundStyle(theme.warning)
                Text(node.string("text") ?? "")
                    .foregroundStyle(theme.foreground)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: theme.spacing.m) {
                ForEach(node.permissionOptions()) { option in
                    CardButton(
                        label: option.label,
                        icon: outcome == option.id ? "checkmark" : nil,
                        tone: tone(option, outcome: outcome),
                        disabled: outcome != nil
                    ) {
                        dispatch(node, .permission(request: node.string("request") ?? "", option: option.id))
                    }
                }
                if PermissionOutcomeKind(outcome) == .cancelled {
                    Text("Cancelled")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
            }
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8).fill(theme.warning.opacity(outcome == nil ? 0.10 : 0.04)))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(theme.warning.opacity(outcome == nil ? 0.6 : 0.25), lineWidth: 1))
    }

    private func tone(_ option: SDUIPermissionOption, outcome: String?) -> CardButton.Tone {
        if let outcome { return outcome == option.id ? .primary : .plain }
        if option.kind == .allowOnce { return .primary }
        return option.kind.rejects ? .danger : .plain
    }
}
