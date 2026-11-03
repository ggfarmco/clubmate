import AppKit
import SwiftUI

import ClubmateKit

struct PermissionCardNodeView: View {
    let node: SDUINode

    @State private var confirming: String?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme
    @Environment(\.composerDrafts) private var drafts

    var body: some View {
        if let outcome = node.string("outcome") {
            answered(outcome)
        } else {
            pending
        }
    }

    private var pending: some View {
        let options = node.permissionOptions()
        let menu = options.filter(\.menu)
        return VStack(alignment: .leading, spacing: theme.spacing.l) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.l) {
                Image(systemName: "exclamationmark.shield")
                    .foregroundStyle(theme.warning)
                heading
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let command = node.string("command") {
                CappedScroll {
                    Text(command)
                        .font(theme.mono)
                        .foregroundStyle(theme.foreground)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.horizontal, theme.gutter)
                        .padding(.vertical, theme.spacing.m)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.background))
            } else if let title = node.string("title"), let text = node.string("text"), text != title {
                Text(text)
                    .foregroundStyle(theme.foreground)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(alignment: .top, spacing: theme.spacing.m) {
                FlowLayout(spacing: theme.spacing.m) {
                    ForEach(Array(options.enumerated()).filter { !$0.element.menu }, id: \.element.id) { index, option in
                        CardButton(label: option.label, shortcut: PermissionKeys.label(index, in: options), tone: tone(option)) {
                            press(option)
                        }
                        .popover(isPresented: confirmation(option), arrowEdge: .top) {
                            AlwaysAllowConfirm(option: option, shortcut: PermissionKeys.label(index, in: options)) {
                                confirming = nil
                            } confirm: {
                                choose(option)
                            }
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if !menu.isEmpty {
                    Menu {
                        ForEach(menu) { option in
                            Button(option.label) { choose(option) }
                        }
                    } label: {
                        Image(systemName: "ellipsis")
                            .foregroundStyle(theme.text2)
                    }
                    .menuStyle(.borderlessButton)
                    .menuIndicator(.hidden)
                    .fixedSize()
                    .help("More answers")
                    .accessibilityLabel("More answers")
                    .testMenu("\(node.id ?? "permission").more", menu.map { LayoutMenuItem(title: $0.label) })
                }
            }
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.toneBg(theme.warning)))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(focused ? theme.accent : theme.toneBorder(theme.warning), lineWidth: focused ? theme.focusRing : 1))
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .onTapGesture { focused = true }
        .padding(.horizontal, theme.gutter * 2)
        .padding(.bottom, theme.spacing.l)
        .onChange(of: confirming) { _, id in
            guard let drafts, let node = node.id else { return }
            drafts.confirm(session: ComposerDrafts.session(of: node), id != nil)
        }
        .onDisappear {
            guard let drafts, let node = node.id else { return }
            drafts.confirm(session: ComposerDrafts.session(of: node), false)
        }
        .keyMonitor(active: true) { event in
            let flags = event.modifierFlags
            let composing = (event.window?.firstResponder as? NSTextInputClient)?.hasMarkedText() == true
            if let id = confirming, let option = options.first(where: { $0.id == id }) {
                return AlwaysAllowConfirm.handle(event, option: option, options: options, bare: !composing && bareKeys) { confirming = nil } confirm: { choose(option) }
            }
            guard let index = PermissionKeys.option(characters: event.charactersIgnoringModifiers, option: flags.contains(.option), otherModifiers: !flags.intersection([.command, .control, .shift]).isEmpty, bare: !composing && bareKeys, options: options) else { return false }
            press(options[index])
            return true
        }
    }

    private var heading: Text {
        guard let title = node.string("title") else {
            return Text(node.string("text") ?? "").foregroundStyle(theme.foreground)
        }
        let caption = node.string("caption").map { Text("  " + $0).font(theme.uiSmall).foregroundStyle(theme.muted) } ?? Text("")
        return Text(title).font(theme.strong).foregroundStyle(theme.foreground) + caption
    }

    private var bareKeys: Bool {
        guard !focused else { return true }
        guard let drafts, let id = node.id else { return false }
        return drafts.focusedEmpty(session: ComposerDrafts.session(of: id))
    }

    private func confirmation(_ option: SDUIPermissionOption) -> Binding<Bool> {
        Binding(get: { confirming == option.id }, set: { shown in
            if !shown, confirming == option.id { confirming = nil }
        })
    }

    private func answered(_ outcome: String) -> some View {
        let chosen = node.permissionOptions().first { $0.id == outcome }
        let cancelled = PermissionOutcomeKind(outcome) == .cancelled
        let label = cancelled ? "Cancelled" : chosen?.label ?? outcome
        let text = node.string("command") ?? node.string("text") ?? ""
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

    private func press(_ option: SDUIPermissionOption) {
        guard option.confirms else { return choose(option) }
        confirming = option.id
    }

    private func choose(_ option: SDUIPermissionOption) {
        confirming = nil
        dispatch(node, .permission(request: node.string("request") ?? "", option: option.id))
    }

    private func tone(_ option: SDUIPermissionOption) -> CardButton.Tone {
        if option.kind == .allowOnce { return .primary }
        return option.kind.rejects ? .danger : .plain
    }
}

private struct AlwaysAllowConfirm: View {
    let option: SDUIPermissionOption
    let shortcut: String?
    let cancel: () -> Void
    let confirm: () -> Void

    @Environment(\.theme) private var theme

    private static let width: CGFloat = 420

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            Text(option.rule == nil ? "Always allow?" : "Always allow this command?")
                .font(theme.strong)
                .foregroundStyle(theme.foreground)
            detail
                .foregroundStyle(theme.text2)
                .fixedSize(horizontal: false, vertical: true)
            if let rule = option.rule {
                Text(rule)
                    .font(theme.mono)
                    .foregroundStyle(theme.foreground)
                    .textSelection(.enabled)
                    .lineLimit(3)
                    .padding(.horizontal, theme.gutter)
                    .padding(.vertical, theme.spacing.m)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.background))
            }
            HStack(spacing: theme.spacing.m) {
                if let shortcut {
                    Text("Press \(shortcut) again to confirm.")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.text2)
                        .lineLimit(1)
                        .layoutPriority(-1)
                }
                Spacer(minLength: theme.gutter)
                CardButton(label: "Cancel", shortcut: "⎋", isDefault: true, action: cancel)
                CardButton(label: confirmLabel, shortcut: shortcut, action: confirm)
            }
        }
        .font(theme.ui)
        .padding(theme.gutter * 2)
        .frame(width: Self.width, alignment: .leading)
        .presentationBackground(theme.surface3)
        .keyMonitor(active: true) { event in
            Self.handle(event, option: option, options: [option], bare: true, cancel: cancel, confirm: confirm)
        }
    }

    private var detail: Text {
        guard option.rule != nil else { return Text(option.label) }
        let rest = Text(" without asking, on this Mac, until you remove the rule from the allow list.")
        guard let workspace = option.workspace else {
            guard let scope = option.scope else { return Text("Claude will run matching commands") + rest }
            return Text("Matching commands will run without asking " + scope + ", until you remove the rule from the allow list.")
        }
        let name = workspace.replacingOccurrences(of: "-", with: "-\u{2060}")
        return Text("Claude will run matching commands in ") + Text(name).font(theme.strong).foregroundStyle(theme.foreground) + rest
    }

    private var confirmLabel: String {
        option.label.hasSuffix("…") ? String(option.label.dropLast()) : option.label
    }

    @MainActor static func handle(_ event: NSEvent, option: SDUIPermissionOption, options: [SDUIPermissionOption], bare: Bool, cancel: () -> Void, confirm: () -> Void) -> Bool {
        if event.keyCode == 53 || event.keyCode == 36 {
            cancel()
            return true
        }
        let flags = event.modifierFlags
        let typed = PermissionKeys.option(characters: event.charactersIgnoringModifiers, option: flags.contains(.option), otherModifiers: !flags.intersection([.command, .control, .shift]).isEmpty, bare: bare, options: options)
        guard let typed, options[typed].id == option.id else { return false }
        if !event.isARepeat { confirm() }
        return true
    }
}
