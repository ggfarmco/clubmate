import AppKit
import SwiftUI

import ClubmateKit

private let cardSettle = Duration.milliseconds(150)

@MainActor
private func cardMayTakeKeys(_ drafts: ComposerDrafts?, node: SDUINode) -> Bool {
    guard let responder = NSApp.promptWindow?.firstResponder else { return false }
    if responder is TerminalSurfaceView || CodeWebView.owns(responder) { return false }
    return !(responder is NSText) || drafts?.focusedEmpty(session: ComposerDrafts.session(of: node.id ?? "")) == true
}

struct ConnectRepoCardView: View {
    let node: SDUINode

    @State private var fallback = ConnectCardModel()
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme
    @Environment(\.composerDrafts) private var drafts

    private static let spaceKey: UInt16 = 49
    private static let downKey: UInt16 = 125
    private static let upKey: UInt16 = 126

    private var model: ConnectCardModel {
        drafts?.connectCard(for: node.id ?? "") ?? fallback
    }

    private var repos: [SDUIConnectRepo] { node.connectRepos() }
    private var single: Bool { repos.count == 1 }
    private var cursorKey: String? { repos.indices.contains(model.cursor) ? repos[model.cursor].key : nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            CardRowsScroll(cursor: cursorKey) {
                if single { singleBody } else { rows }
            }
            footer
        }
        .font(theme.ui)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.toneBg(theme.warning)))
        .clipShape(RoundedRectangle(cornerRadius: theme.radius.l))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.toneBorder(theme.warning), lineWidth: 1))
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .reportsFocus(.connectCard, focused)
        .onTapGesture { focused = true }
        .padding(.horizontal, theme.gutter * 2)
        .padding(.bottom, theme.spacing.l)
        .task(id: node.string("request")) {
            try? await Task.sleep(for: cardSettle)
            if cardMayTakeKeys(drafts, node: node) { focused = true }
        }
        .keyMonitor(active: true, handle: handle)
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
            Image(systemName: "folder.badge.plus")
                .foregroundStyle(theme.warning)
            Text(node.string("title") ?? "")
                .font(theme.strong)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .fixedSize()
            Text(node.string("caption") ?? "")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.label)
                .help(node.string("caption") ?? "")
        }
        .padding(theme.gutter)
    }

    private var rows: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(model.visible(repos)) { repo in
                ConnectRepoRow(repo: repo, ticked: !model.unticked.contains(repo.key), cursor: focused && repo.key == cursorKey, other: model.others[repo.key]) {
                    select(repo)
                    model.toggle(repos)
                } pickOther: {
                    select(repo)
                    pickOther()
                }
                .id(repo.key)
            }
            if let more = node.string("more"), !model.unfolded {
                Divider().overlay(theme.border)
                Button { model.unfolded = true } label: {
                    HStack(spacing: theme.spacing.s) {
                        Text(more)
                        Image(systemName: "chevron.down")
                            .font(theme.glyph)
                    }
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.text2)
                    .padding(.horizontal, theme.gutter)
                    .padding(.vertical, theme.spacing.m)
                }
                .buttonStyle(.plain)
            }
        }
    }

    @ViewBuilder
    private var singleBody: some View {
        if let repo = repos.first {
            VStack(alignment: .leading, spacing: theme.spacing.l) {
                VStack(alignment: .leading, spacing: theme.spacing.s) {
                    HStack(spacing: theme.spacing.m) {
                        Image(systemName: "folder")
                        Text(repo.name)
                            .font(theme.title)
                            .lineLimit(1)
                            .truncationMode(.label)
                            .help(repo.name)
                    }
                    .foregroundStyle(theme.foreground)
                    if let path = model.others[repo.key].map(Self.tilde) ?? repo.path {
                        Text(path)
                            .font(theme.mono)
                            .foregroundStyle(theme.foreground)
                            .lineLimit(1)
                            .truncationMode(.path)
                            .help(path)
                            .padding(.horizontal, theme.spacing.s)
                            .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(theme.hover))
                    }
                    singleFacts(repo)
                }
                if let reason = repo.reason {
                    VStack(alignment: .leading, spacing: theme.spacing.s) {
                        Text("Why")
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.muted)
                        InlineCardText(text: "“" + reason + "”", color: theme.text2)
                    }
                }
                VStack(alignment: .leading, spacing: theme.spacing.xs) {
                    ForEach(node.connectEffects()) { effect in
                        EffectRow(effect: effect)
                    }
                }
            }
            .padding(.horizontal, theme.gutter)
            .frame(maxWidth: .infinity, alignment: .leading)
            .id(repo.key)
        }
    }

    @ViewBuilder
    private func singleFacts(_ repo: SDUIConnectRepo) -> some View {
        let clone = repo.clone && model.others[repo.key] == nil
        if repo.warn {
            if let remote = repo.remote { factLine(remote) }
            ConnectRepoRow.warning(repo.state ?? "", theme: theme)
        } else if clone || repo.clone {
            if clone {
                Text(repo.state ?? "")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.text2)
                    .fixedSize(horizontal: false, vertical: true)
            }
        } else {
            factLine([repo.remote, repo.state].compactMap { $0 }.joined(separator: " · "))
        }
    }

    private func factLine(_ text: String) -> some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(theme.muted)
            .lineLimit(1)
            .truncationMode(.label)
            .help(text)
    }

    private var footer: some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            if let effect = node.string("effect") {
                InlineCardText(text: effect, color: theme.text2, font: theme.uiSmall)
            }
            ViewThatFits(in: .horizontal) {
                HStack(spacing: theme.spacing.m) {
                    actions
                    hint(wraps: false)
                }
                VStack(alignment: .leading, spacing: theme.spacing.s) {
                    HStack(spacing: theme.spacing.m) { actions }
                    hint(wraps: true)
                }
            }
            if let declined = node.string("declined") {
                Text(declined)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(theme.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) {
            if !single { theme.border.frame(height: 1) }
        }
    }

    @ViewBuilder
    private var actions: some View {
        CardButton(label: connectLabel, shortcut: "⌥1", tone: .primary, disabled: model.ticked(repos) == 0, action: connect)
        if single {
            CardButton(label: "Another folder…", shortcut: "⌥2", action: pickOther)
        }
        CardButton(label: "Don't connect", shortcut: "⌥3", action: decline)
    }

    @ViewBuilder
    private func hint(wraps: Bool) -> some View {
        if let hint = node.string("hint") {
            Text(hint)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(wraps ? nil : 1)
                .fixedSize(horizontal: !wraps, vertical: true)
        }
    }

    private var connectLabel: String {
        let label = node.string("connect") ?? ""
        return single ? label : "\(label) \(model.ticked(repos))"
    }

    private func select(_ repo: SDUIConnectRepo) {
        focused = true
        if let at = repos.firstIndex(of: repo) { model.cursor = at }
    }

    private func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection([.command, .control, .shift, .option])
        if flags == .option {
            let answers = ["1": connect, "2": pickOther, "3": decline]
            guard let key = event.charactersIgnoringModifiers, let answer = answers[key] else { return false }
            if !event.isARepeat { answer() }
            return true
        }
        guard focused, flags.isEmpty else { return false }
        switch event.keyCode {
        case Self.spaceKey where !single:
            if !event.isARepeat { model.toggle(repos) }
        case Self.downKey where !single:
            model.move(1, in: repos)
        case Self.upKey where !single:
            model.move(-1, in: repos)
        default:
            return false
        }
        return true
    }

    private func connect() {
        guard model.ticked(repos) > 0 else { return }
        send(model.answers(repos))
    }

    private func decline() {
        send(model.declined(repos))
    }

    private func send(_ answers: [JSONValue]) {
        dispatch(node, .named("connectAnswer", .object(["request": .string(node.string("request") ?? ""), "answers": .array(answers)])))
    }

    private func pickOther() {
        guard let repo = cursorKey.flatMap({ key in repos.first { $0.key == key } }) else { return }
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.canCreateDirectories = repo.clone
        panel.showsHiddenFiles = true
        panel.prompt = "Choose"
        guard !TestMode.current, panel.runModal() == .OK, let url = panel.url else { return }
        model.choose(other: repo.clone ? url.appending(path: repo.name).path : url.path, for: repo.key)
    }

    private static func tilde(_ path: String) -> String {
        (path as NSString).abbreviatingWithTildeInPath
    }
}

struct LandRequestCardView: View {
    let node: SDUINode

    @State private var confirming: SDUIPermissionOption?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme
    @Environment(\.composerDrafts) private var drafts

    private var options: [SDUIPermissionOption] { node.permissionOptions() }

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                Image(systemName: "arrow.triangle.branch")
                    .foregroundStyle(theme.warning)
                Text(node.string("title") ?? "")
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .fixedSize()
                Text(node.string("caption") ?? "")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(node.string("caption") ?? "")
            }
            HStack(spacing: theme.spacing.m) {
                Image(systemName: "folder")
                Text(node.string("name") ?? "")
                    .font(theme.title)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(node.string("name") ?? "")
            }
            .foregroundStyle(theme.foreground)
            if let detail = node.string("detail") {
                InlineCardText(text: detail, color: theme.text2)
            }
            if let rule = node.string("rule") {
                caption(rule)
            }
            ViewThatFits(in: .horizontal) {
                HStack(spacing: theme.spacing.m) {
                    buttons { !$0.kind.rejects }
                    Spacer(minLength: theme.gutter)
                    buttons { $0.kind.rejects }
                }
                FlowLayout(spacing: theme.spacing.m) {
                    buttons { _ in true }
                }
            }
            .popover(isPresented: confirmation, arrowEdge: .top) {
                if let option = confirming {
                    LeaveConfirm(message: node.string("confirm") ?? "", label: Self.plain(option.label), shortcut: shortcut(option), option: option, options: options) {
                        confirming = nil
                    } confirm: {
                        answer(option)
                    }
                }
            }
            if let hint = node.string("hint") {
                caption(hint)
            }
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.toneBg(theme.warning)))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.toneBorder(theme.warning), lineWidth: 1))
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .reportsFocus(.landCard, focused)
        .onTapGesture { focused = true }
        .padding(.horizontal, theme.gutter * 2)
        .padding(.bottom, theme.spacing.l)
        .task(id: node.string("request")) {
            try? await Task.sleep(for: cardSettle)
            if cardMayTakeKeys(drafts, node: node) { focused = true }
        }
        .onChange(of: confirming) { _, option in
            guard let drafts, let id = node.id else { return }
            drafts.confirm(session: ComposerDrafts.session(of: id), option != nil)
        }
        .onDisappear {
            guard let drafts, let id = node.id else { return }
            drafts.confirm(session: ComposerDrafts.session(of: id), false)
        }
        .keyMonitor(active: true) { event in
            if let option = confirming {
                return LeaveConfirm.handle(event, option: option, options: options) { confirming = nil } confirm: { answer(option) }
            }
            let flags = event.modifierFlags
            guard let index = PermissionKeys.option(characters: event.charactersIgnoringModifiers, option: flags.contains(.option), otherModifiers: !flags.intersection([.command, .control, .shift]).isEmpty, options: options) else { return false }
            guard !event.isARepeat else { return true }
            press(options[index])
            return true
        }
    }

    private var confirmation: Binding<Bool> {
        Binding(get: { confirming != nil }, set: { shown in
            if !shown { confirming = nil }
        })
    }

    private func shortcut(_ option: SDUIPermissionOption) -> String? {
        options.firstIndex(of: option).flatMap { PermissionKeys.label($0, in: options) }
    }

    private static func plain(_ label: String) -> String {
        label.hasSuffix("…") ? String(label.dropLast()) : label
    }

    private func caption(_ text: String) -> some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(theme.muted)
            .fixedSize(horizontal: false, vertical: true)
    }

    private func buttons(where keep: @escaping (SDUIPermissionOption) -> Bool) -> some View {
        ForEach(Array(options.enumerated()).filter { keep($0.element) }, id: \.element.id) { index, option in
            CardButton(label: option.label, shortcut: PermissionKeys.label(index, in: options), tone: tone(option)) {
                press(option)
            }
        }
    }

    private func tone(_ option: SDUIPermissionOption) -> CardButton.Tone {
        if option.kind == .allowOnce { return .primary }
        return option.kind.rejects ? .ghost : .plain
    }

    private func press(_ option: SDUIPermissionOption) {
        guard option.kind.rejects, node.string("confirm") != nil, confirming != option else { return answer(option) }
        confirming = option
    }

    private func answer(_ option: SDUIPermissionOption) {
        confirming = nil
        dispatch(node, .named("landAnswer", .object(["request": .string(node.string("request") ?? ""), "action": .string(option.id)])))
    }
}

private struct LeaveConfirm: View {
    let message: String
    let label: String
    let shortcut: String?
    let option: SDUIPermissionOption
    let options: [SDUIPermissionOption]
    let cancel: () -> Void
    let confirm: () -> Void

    @Environment(\.theme) private var theme

    private static let width: CGFloat = 420
    private static let escapeKey: UInt16 = 53
    private static let returnKey: UInt16 = 36

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            Text(message)
                .foregroundStyle(theme.foreground)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: theme.spacing.m) {
                Spacer(minLength: theme.gutter)
                CardButton(label: "Cancel", shortcut: "⎋", isDefault: true, action: cancel)
                CardButton(label: label, shortcut: shortcut, tone: .danger, action: confirm)
            }
        }
        .font(theme.ui)
        .padding(theme.gutter * 2)
        .frame(width: Self.width, alignment: .leading)
        .presentationBackground(theme.surface3)
        .keyMonitor(active: true) { event in
            Self.handle(event, option: option, options: options, cancel: cancel, confirm: confirm)
        }
    }

    @MainActor static func handle(_ event: NSEvent, option: SDUIPermissionOption, options: [SDUIPermissionOption], cancel: () -> Void, confirm: () -> Void) -> Bool {
        if event.keyCode == escapeKey || event.keyCode == returnKey {
            cancel()
            return true
        }
        let flags = event.modifierFlags
        guard let typed = PermissionKeys.option(characters: event.charactersIgnoringModifiers, option: flags.contains(.option), otherModifiers: !flags.intersection([.command, .control, .shift]).isEmpty, options: options) else { return false }
        if options[typed].id == option.id, !event.isARepeat { confirm() }
        return true
    }
}

private struct CardRowsScroll<Content: View>: View {
    let cursor: String?
    @ViewBuilder let content: Content

    @State private var height: CGFloat = 0
    @Environment(\.theme) private var theme

    private static var minRows: CGFloat { 5 }
    private static var maxRows: CGFloat { 20 }

    var body: some View {
        ScrollViewReader { reader in
            ScrollView {
                content
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
            }
            .scrollIndicators(.never)
            .frame(minHeight: min(height, theme.rowHeight * Self.minRows), maxHeight: min(height, theme.rowHeight * Self.maxRows))
            .clipped()
            .onChange(of: cursor) { _, key in
                if let key { reader.scrollTo(key) }
            }
        }
    }
}

private struct RepoColumns: Layout {
    let spacing: CGFloat
    let stacked: CGFloat

    private static let shares: [CGFloat] = [1.15, 1.5, 0.9]
    private static let wideEnough: CGFloat = 480

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = Self.width(proposal)
        return CGSize(width: width, height: placed(subviews, width: width).reduce(0) { max($0, $1.origin.y + $1.height) })
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for (subview, frame) in zip(subviews, placed(subviews, width: bounds.width)) {
            subview.place(at: CGPoint(x: bounds.minX + frame.minX, y: bounds.minY + frame.minY), proposal: ProposedViewSize(width: frame.width, height: frame.height))
        }
    }

    private static func width(_ proposal: ProposedViewSize) -> CGFloat {
        guard let width = proposal.width, width.isFinite else { return wideEnough }
        return width
    }

    private func placed(_ subviews: Subviews, width: CGFloat) -> [CGRect] {
        width >= Self.wideEnough ? columns(subviews, width: width) : stack(subviews, width: width)
    }

    private func columns(_ subviews: Subviews, width: CGFloat) -> [CGRect] {
        let room = max(0, width - spacing * CGFloat(subviews.count - 1))
        let total = Self.shares.prefix(subviews.count).reduce(0, +)
        var x: CGFloat = 0
        return subviews.indices.map { i in
            let column = room * Self.shares[i] / total
            defer { x += column + spacing }
            return CGRect(x: x, y: 0, width: column, height: subviews[i].sizeThatFits(ProposedViewSize(width: column, height: nil)).height)
        }
    }

    private func stack(_ subviews: Subviews, width: CGFloat) -> [CGRect] {
        var y: CGFloat = 0
        return subviews.map { subview in
            let height = subview.sizeThatFits(ProposedViewSize(width: width, height: nil)).height
            defer { if height > 0 { y += height + stacked } }
            return CGRect(x: 0, y: y, width: width, height: height)
        }
    }
}

private struct ConnectRepoRow: View {
    let repo: SDUIConnectRepo
    let ticked: Bool
    let cursor: Bool
    let other: String?
    let toggle: () -> Void
    let pickOther: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(alignment: .top, spacing: theme.gutter) {
            checkbox
            VStack(alignment: .leading, spacing: theme.spacing.s) {
                RepoColumns(spacing: theme.gutter, stacked: theme.spacing.s) {
                    identity
                    quote
                    landing
                }
                brings
            }
            .opacity(ticked ? 1 : 0.55)
        }
        .padding(theme.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) { theme.border.frame(height: 1) }
        .overlay(alignment: .leading) {
            if cursor { theme.accent.frame(width: 2) }
        }
    }

    private var checkbox: some View {
        Button(action: toggle) {
            RoundedRectangle(cornerRadius: theme.radius.s)
                .fill(ticked ? theme.accent : Color.clear)
                .overlay(RoundedRectangle(cornerRadius: theme.radius.s).stroke(ticked ? theme.accent : theme.borderStrong, lineWidth: 1))
                .overlay {
                    if ticked {
                        Image(systemName: "checkmark")
                            .font(theme.glyph)
                            .foregroundStyle(theme.onAccent)
                    }
                }
                .frame(width: theme.spacing.xl, height: theme.spacing.xl)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(repo.name)
    }

    private var identity: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            HStack(spacing: theme.spacing.s) {
                Image(systemName: "folder")
                Text(repo.name)
                    .font(theme.strong)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(repo.name)
            }
            .foregroundStyle(theme.foreground)
            if let path = other.map(tilde) ?? repo.path {
                line(path, theme.muted, truncation: .path)
            }
            if let remote = repo.remote, !repo.clone {
                line(remote, theme.faint)
            }
            if repo.warn {
                Self.warning(repo.state ?? "", theme: theme)
            } else if repo.clone {
                if other == nil, let state = repo.state {
                    Text(state)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.text2)
                        .fixedSize(horizontal: false, vertical: true)
                }
            } else if let state = repo.state {
                line(state, theme.muted)
            }
            Button(action: pickOther) {
                HStack(spacing: theme.spacing.s) {
                    Text("other folder…").underline()
                    if cursor { Text("⌥2").opacity(0.7) }
                }
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
            }
            .buttonStyle(.plain)
        }
    }

    private func tilde(_ path: String) -> String {
        (path as NSString).abbreviatingWithTildeInPath
    }

    private func line(_ text: String, _ color: Color, truncation: Text.TruncationMode = .label) -> some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(color)
            .lineLimit(1)
            .truncationMode(truncation)
            .help(text)
    }

    static func warning(_ text: String, theme: Theme) -> some View {
        HStack(spacing: theme.spacing.s) {
            Image(systemName: "exclamationmark.circle")
            Text(text)
                .lineLimit(1)
                .truncationMode(.label)
                .help(text)
        }
        .font(theme.uiSmall)
        .foregroundStyle(theme.warning)
    }

    @ViewBuilder
    private var quote: some View {
        if let reason = repo.reason {
            InlineCardText(text: "“" + reason + "”", color: theme.text2)
        } else {
            Color.clear.frame(height: 0)
        }
    }

    @ViewBuilder
    private var landing: some View {
        if let rule = repo.landsBy {
            VStack(alignment: .leading, spacing: theme.spacing.xs) {
                line("lands by", theme.faint)
                line(rule, theme.text2)
                if let source = repo.source {
                    line(source, theme.faint)
                }
            }
        } else {
            Color.clear.frame(height: 0)
        }
    }

    @ViewBuilder
    private var brings: some View {
        if let text = repo.brings {
            Text(bringsLine(text))
                .font(theme.uiSmall)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func bringsLine(_ text: String) -> AttributedString {
        var out = AttributedString("brings ")
        out.foregroundColor = theme.faint
        out += InlineCardText.attributed(text, color: theme.text2, font: theme.uiSmall, codeFill: theme.hover)
        if let trust = repo.trust {
            var note = AttributedString(" · " + trust)
            note.foregroundColor = theme.muted
            out += note
        }
        return out
    }
}

private struct EffectRow: View {
    let effect: SDUIConnectEffect

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
            Text("·")
                .foregroundStyle(theme.faint)
            Text(line)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var line: AttributedString {
        var out = InlineCardText.attributed(effect.text, color: theme.text2, font: theme.ui, codeFill: theme.hover)
        if let note = effect.note {
            var dash = AttributedString(" — ")
            dash.foregroundColor = theme.muted
            dash.font = theme.uiSmall
            out += dash
            out += InlineCardText.attributed(note, color: theme.muted, font: theme.uiSmall, codeFill: theme.hover)
        }
        return out
    }
}

private struct InlineCardText: View {
    let text: String
    let color: Color
    var font: Font?

    @Environment(\.theme) private var theme

    var body: some View {
        Text(Self.attributed(text, color: color, font: font ?? theme.ui, codeFill: theme.hover))
            .fixedSize(horizontal: false, vertical: true)
    }

    static func attributed(_ text: String, color: Color, font: Font, codeFill: Color) -> AttributedString {
        var out = AttributedString()
        for span in CardInline.spans(text) {
            var part = AttributedString(span.text)
            part.foregroundColor = color
            part.font = span.style == .strong ? font.weight(.semibold) : font
            if span.style == .code { part.backgroundColor = codeFill }
            out += part
        }
        return out
    }
}
