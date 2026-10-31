import SwiftUI

import ClubmateKit

struct PermissionCardNodeView: View {
    let node: SDUINode

    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        if let outcome = node.string("outcome") {
            answered(outcome)
        } else {
            pending
        }
    }

    private var pending: some View {
        let options = node.permissionOptions()
        return VStack(alignment: .leading, spacing: theme.spacing.l) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.l) {
                Image(systemName: "hand.raised.fill")
                    .foregroundStyle(theme.warning)
                Text(node.string("text") ?? "")
                    .foregroundStyle(theme.foreground)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            FlowLayout(spacing: theme.spacing.m) {
                ForEach(Array(options.enumerated()), id: \.element.id) { index, option in
                    CardButton(label: option.label, shortcut: PermissionKeys.label(index, kind: option.kind), tone: tone(option)) {
                        choose(option)
                    }
                }
            }
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.toneBg(theme.warning)))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.toneBorder(theme.warning), lineWidth: 1))
        .padding(.horizontal, theme.gutter * 2)
        .padding(.bottom, theme.spacing.l)
        .keyMonitor(active: true) { event in
            let flags = event.modifierFlags
            guard let index = PermissionKeys.option(characters: event.charactersIgnoringModifiers, option: flags.contains(.option), otherModifiers: !flags.intersection([.command, .control, .shift]).isEmpty, kinds: options.map(\.kind)) else { return false }
            choose(options[index])
            return true
        }
    }

    private func answered(_ outcome: String) -> some View {
        let chosen = node.permissionOptions().first { $0.id == outcome }
        let cancelled = PermissionOutcomeKind(outcome) == .cancelled
        let label = cancelled ? "Cancelled" : chosen?.label ?? outcome
        let text = node.string("text") ?? ""
        return HStack(spacing: theme.spacing.m) {
            Image(systemName: chosen?.kind.rejects == true || cancelled ? "xmark" : "checkmark")
                .font(theme.glyph)
            Text("\(label) · \(text)")
                .lineLimit(1)
                .truncationMode(.label)
                .help(text)
        }
        .font(theme.uiSmall)
        .foregroundStyle(theme.muted)
        .padding(.horizontal, theme.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func choose(_ option: SDUIPermissionOption) {
        dispatch(node, .permission(request: node.string("request") ?? "", option: option.id))
    }

    private func tone(_ option: SDUIPermissionOption) -> CardButton.Tone {
        if option.kind == .allowOnce { return .primary }
        return option.kind.rejects ? .danger : .plain
    }
}
