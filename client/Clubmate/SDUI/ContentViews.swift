import AppKit
import SwiftUI

import ClubmateKit

enum SymbolName {
    static func resolve(_ name: String?, fallback: String = "questionmark.square") -> String {
        guard let name, NSImage(systemSymbolName: name, accessibilityDescription: nil) != nil else {
            return fallback
        }
        return name
    }
}

struct TextNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    private var lines: Int? {
        node.props["lines"]?.numberValue.flatMap { Int(exactly: $0) }.flatMap { $0 >= 1 ? min($0, 50) : nil }
    }

    var body: some View {
        let text = node.string("text") ?? ""
        if node.string("style") == "block" {
            Text(text)
                .font(theme.ui)
                .foregroundStyle(color)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, theme.gutter)
                .padding(.vertical, theme.spacing.m)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.background))
                .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(theme.border, lineWidth: 1))
        } else if node.string("style") == "title" {
            Text(text)
                .font(theme.title)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .truncationMode(.label)
                .help(text)
                .layoutPriority(1)
        } else if node.string("style") == "strong" {
            Text(text)
                .font(theme.strong)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .truncationMode(.label)
                .help(text)
                .layoutPriority(1)
        } else if let short = node.string("short") {
            ViewThatFits(in: .horizontal) {
                Text(text).fixedSize()
                Text(short).fixedSize()
                Color.clear.frame(width: 0, height: 0)
            }
            .font(theme.ui)
            .foregroundStyle(color)
            .help(text)
        } else if lines == 1 {
            Text(text)
                .font(theme.ui)
                .foregroundStyle(color)
                .lineLimit(1)
                .truncationMode(.tail)
                .help(text)
        } else if let lines {
            ClampedText(text: text, lines: lines, color: color)
        } else {
            Text(text)
                .font(theme.ui)
                .foregroundStyle(color)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var color: Color {
        theme.tone(node.string("tone")) ?? (node.string("style") == "muted" ? theme.muted : theme.foreground)
    }
}

private struct ClampedText: View {
    let text: String
    let lines: Int
    let color: Color

    @State private var expanded = false
    @State private var clamped: CGFloat = 0
    @State private var full: CGFloat = 0
    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            Text(text)
                .font(theme.ui)
                .foregroundStyle(color)
                .lineLimit(expanded ? nil : lines)
                .fixedSize(horizontal: false, vertical: true)
                .help(expanded ? "" : text)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { clamped = $0 }
                .background(alignment: .topLeading) {
                    Text(text)
                        .font(theme.ui)
                        .fixedSize(horizontal: false, vertical: true)
                        .hidden()
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { full = $0 }
                }
            if expanded || full > clamped + 0.5 {
                Button { expanded.toggle() } label: {
                    Text(expanded ? "Show less" : "Show all")
                        .font(theme.uiSmall)
                        .foregroundStyle(hovering ? theme.foreground : theme.text2)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .onHover { hovering = $0 }
            }
        }
    }
}

struct DividerNodeView: View {
    @Environment(\.theme) private var theme

    var body: some View {
        Rectangle()
            .fill(theme.border)
            .frame(height: 1)
            .frame(maxWidth: .infinity)
    }
}

struct BadgeNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        BadgeLabel(text: node.string("text") ?? "", tone: theme.tone(node.string("tone")), outline: node.bool("outline") == true)
    }
}

struct BadgeLabel: View {
    let text: String
    let tone: Color?
    var outline = false

    @Environment(\.theme) private var theme

    var body: some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(tone ?? theme.text2)
            .padding(theme.spacing.badge)
            .background(Capsule().fill(outline ? .clear : tone.map(theme.toneBg) ?? theme.hover))
            .overlay {
                if outline {
                    Capsule().strokeBorder(tone.map(theme.toneBorder) ?? theme.borderStrong, lineWidth: 1)
                }
            }
    }
}

struct ProgressNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        let total = node.props["total"]?.numberValue ?? 0
        let fraction = total > 0 ? min(1, max(0, (node.props["value"]?.numberValue ?? 0) / total)) : 0
        GeometryReader { proxy in
            ZStack(alignment: .leading) {
                Capsule().fill(theme.hover)
                Capsule().fill(theme.tone(node.string("tone")) ?? theme.accent)
                    .frame(width: proxy.size.width * fraction)
            }
        }
        .frame(minWidth: 40, maxWidth: .infinity)
        .frame(height: 4)
        .accessibilityElement()
        .accessibilityValue("\((node.props["value"]?.numberValue ?? 0).formatted()) of \(total.formatted())")
    }
}

struct IconNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        Image(systemName: SymbolName.resolve(node.string("name")))
            .font(theme.ui)
            .foregroundStyle(theme.tone(node.string("tone")) ?? theme.foreground)
    }
}

struct MarkdownNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        Text(theme.inlineMarkdown(node.string("text") ?? ""))
            .safeLinks()
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .tint(theme.accent)
            .textSelection(.enabled)
    }
}

struct CrumbNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.m) {
            ForEach(Array(parts.enumerated()), id: \.offset) { _, part in
                Text(part)
                    .foregroundStyle(theme.muted)
                    .truncationMode(.label)
                    .help(part)
                Text("/")
                    .foregroundStyle(theme.faint)
                    .fixedSize()
            }
            if let icon = node.string("icon") {
                Image(systemName: SymbolName.resolve(icon))
                    .foregroundStyle(theme.muted)
            }
            Text(node.string("leaf") ?? "")
                .foregroundStyle(theme.foreground)
                .truncationMode(.label)
                .layoutPriority(1)
            Spacer(minLength: theme.spacing.m)
            if let note = node.string("note") {
                Text(note)
                    .foregroundStyle(theme.muted)
                    .fixedSize()
            }
            ForEach(Array(node.children.enumerated()), id: \.offset) { _, child in
                SDUINodeView(node: child)
            }
        }
        .font(theme.uiSmall)
        .lineLimit(1)
        .padding(.horizontal, theme.gutter)
        .frame(height: theme.control + theme.spacing.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
    }

    private var parts: [String] {
        node.props["parts"]?.arrayValue?.compactMap(\.stringValue) ?? []
    }
}

struct HeaderNodeView: View {
    let node: SDUINode

    @State private var width: CGFloat = 0
    @Environment(\.theme) private var theme
    @Environment(\.headerTrailingInset) private var trailingInset
    @Environment(FormState.self) private var form: FormState?

    private static let inlineCaptionWidth: CGFloat = 640

    var body: some View {
        let title = node.string("title") ?? ""
        Group {
            if node.headerField != nil || node.bool("inline") == true {
                ViewThatFits(in: .horizontal) {
                    oneLine(title)
                    stacked(title)
                }
            } else {
                stacked(title)
            }
        }
        .padding(.horizontal, theme.gutter)
        .padding(.vertical, theme.spacing.l)
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
    }

    private func oneLine(_ title: String) -> some View {
        HStack(spacing: theme.spacing.m) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                Text(title)
                    .font(theme.heading)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .fixedSize()
                if let meta = node.string("meta") {
                    Text(meta)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                        .fixedSize()
                }
            }
            Spacer(minLength: theme.spacing.xl)
            if let field = node.headerField {
                SDUINodeView(node: field.node)
                    .equatable()
                    .environment(\.sduiGutter, false)
                    .frame(width: Self.filterWidth)
            }
            ForEach(node.headerBar) { child in
                SDUINodeView(node: child.node).equatable()
                    .fixedSize()
            }
            let menu = node.headerButtons.filter { $0.node.bool("overflow") == true }
            if !menu.isEmpty {
                HeaderMoreMenu(id: "\(node.id ?? "header").more", actions: menu.map(\.node))
            }
        }
        .padding(.trailing, trailingInset)
    }

    private func stacked(_ title: String) -> some View {
        let session = node.string("status") != nil
        let inline = width >= Self.inlineCaptionWidth
        return VStack(alignment: .leading, spacing: theme.spacing.l) {
            if !session, node.string("field") == nil || form == nil, !node.headerBadges().isEmpty {
                ViewThatFits(in: .horizontal) {
                    HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                        titleLine(title)
                            .fixedSize()
                        badges
                        Spacer(minLength: 0)
                    }
                    VStack(alignment: .leading, spacing: theme.spacing.s) {
                        titleLine(title)
                        HStack(spacing: theme.spacing.m) { badges }
                    }
                }
                .padding(.trailing, trailingInset)
            } else {
                titleRow(title, session: session, inline: inline)
            }
            if !inline, let caption = sessionCaption {
                captionText(caption)
            }
            if let meta = node.string("meta") {
                Text(meta)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if node.headerField != nil || !node.headerButtons.isEmpty {
                if width > 0 && width < 480 {
                    overflowRow
                } else {
                    ViewThatFits(in: .horizontal) {
                        HStack(spacing: theme.spacing.m) {
                            filter
                            let bar = node.headerBar
                            ForEach(bar.filter { $0.node.string("variant") != "danger" }) { child in
                                SDUINodeView(node: child.node).equatable()
                            }
                            let danger = bar.filter { $0.node.string("variant") == "danger" }
                            let menu = node.headerButtons.filter { $0.node.bool("overflow") == true }
                            if !danger.isEmpty || !menu.isEmpty {
                                Spacer(minLength: theme.spacing.m)
                            }
                            ForEach(danger) { child in
                                SDUINodeView(node: child.node).equatable()
                            }
                            if !menu.isEmpty {
                                HeaderMoreMenu(id: "\(node.id ?? "header").more", actions: menu.map(\.node))
                            }
                        }
                        overflowRow
                    }
                }
            }
        }
    }

    private var sessionCaption: String? {
        let parts = ["agent", "branch", "mode"].compactMap { node.string($0) }.filter { !$0.isEmpty }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    private func captionText(_ caption: String) -> some View {
        Text(caption)
            .font(theme.uiSmall)
            .foregroundStyle(theme.muted)
            .lineLimit(1)
            .truncationMode(.middle)
            .help(caption)
    }

    private func titleRow(_ title: String, session: Bool, inline: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
            if let icon = node.string("icon") {
                Image(systemName: SymbolName.resolve(icon))
                    .foregroundStyle(theme.tone(node.headerStatusTone) ?? theme.muted)
            }
            if let field = node.string("field"), form != nil {
                HeaderTitleField(title: title, field: field, error: node.string("error").flatMap { $0.isEmpty ? nil : $0 })
                    .layoutPriority(1)
            } else {
                Text(title)
                    .font(theme.heading)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(2)
                    .truncationMode(.label)
                    .fixedSize(horizontal: false, vertical: true)
                    .help(title)
                    .layoutPriority(1)
            }
            if inline, let caption = sessionCaption {
                captionText(caption)
            }
            if !session { badges }
            Spacer(minLength: 0)
            if session {
                if let pill = node.headerPill {
                    HeaderPillView(node: pill.node)
                }
                badges
                stop(compact: !inline)
            }
        }
        .padding(.trailing, trailingInset)
    }

    private func titleLine(_ title: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
            if let icon = node.string("icon") {
                Image(systemName: SymbolName.resolve(icon))
                    .foregroundStyle(theme.tone(node.headerStatusTone) ?? theme.muted)
            }
            Text(title)
                .font(theme.heading)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .truncationMode(.label)
                .help(title)
        }
    }

    private var badges: some View {
        ForEach(Array(node.headerBadges().enumerated()), id: \.offset) { _, badge in
            BadgeLabel(text: badge.text, tone: theme.tone(badge.tone), outline: badge.outline)
                .fixedSize()
        }
    }

    @ViewBuilder
    private func stop(compact: Bool) -> some View {
        if let stop = node.headerStop {
            SDUINodeView(node: compact ? Self.iconOnly(stop.node) : stop.node)
                .equatable()
                .environment(\.buttonKeyHint, Self.stopHint)
                .fixedSize()
        }
    }

    private static func iconOnly(_ button: SDUINode) -> SDUINode {
        var props = button.props
        props["iconOnly"] = .bool(true)
        return SDUINode(type: button.type, id: button.id, props: props)
    }

    private static let stopHint = "⎋"

    @ViewBuilder
    private var filter: some View {
        if let field = node.headerField {
            SDUINodeView(node: field.node)
                .equatable()
                .environment(\.sduiGutter, false)
                .frame(maxWidth: Self.filterWidth - theme.gutter * 2)
                .padding(.trailing, theme.gutter)
        }
    }

    private static let filterWidth: CGFloat = 220

    private var overflowRow: some View {
        let actions = node.headerOverflow()
        return HStack(alignment: .top, spacing: theme.spacing.m) {
            filter
            FlowLayout(spacing: theme.spacing.m) {
                ForEach(actions.shown) { child in
                    SDUINodeView(node: child.node)
                }
            }
            .layoutPriority(1)
            if !actions.more.isEmpty {
                Spacer(minLength: 0)
                HeaderMoreMenu(id: "\(node.id ?? "header").more", actions: actions.more.map(\.node))
            }
        }
    }
}

private struct HeaderTitleField: View {
    let title: String
    let field: String
    let error: String?

    @State private var editing = false
    @State private var text = ""
    @State private var committed: String?
    @State private var hovering = false
    @State private var keyboardFocus = false
    @FocusState private var focused: Bool
    @FocusState private var fieldFocused: Bool
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    private static let placeholder = "Title"

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            if editing {
                TextField("", text: $text, prompt: Text(Self.placeholder).foregroundStyle(theme.muted))
                    .textFieldStyle(.plain)
                    .font(theme.heading)
                    .foregroundStyle(theme.foreground)
                    .padding(.horizontal, theme.spacing.m)
                    .frame(maxWidth: .infinity, minHeight: theme.control)
                    .fieldChrome(focused: fieldFocused, error: error)
                    .focused($fieldFocused)
                    .onSubmit { commit(refocus: true) }
                    .onExitCommand(perform: cancel)
                    .onChange(of: fieldFocused) { _, now in
                        if !now { commit(refocus: false) }
                    }
                    .onAppear { DispatchQueue.main.async { fieldFocused = true } }
            } else {
                Button(action: edit) { label }
                    .buttonStyle(.plain)
                    .focusable()
                    .focusEffectDisabled()
                    .focused($focused)
                    .onChange(of: focused) { _, now in
                        keyboardFocus = now && !ButtonNodeView.mouseEvents.contains(NSApp.currentEvent?.type ?? .keyDown)
                    }
                    .onKeyPress(keys: [.return]) { _ in
                        edit()
                        return .handled
                    }
                    .onHover { hovering = $0 }
                    .accessibilityLabel(shown)
                    .accessibilityHint("Edit title")
                if let error {
                    Text(error)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.danger)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .help(error)
                }
            }
        }
        .onChange(of: title, initial: true) { _, value in
            committed = nil
            if !editing { form?.values[field] = .string(value) }
        }
    }

    private var shown: String { committed ?? title }

    private var label: some View {
        Text(shown.isEmpty ? Self.placeholder : shown)
            .font(theme.heading)
            .foregroundStyle(shown.isEmpty ? theme.muted : theme.foreground)
            .lineLimit(2)
            .truncationMode(.label)
            .fixedSize(horizontal: false, vertical: true)
            .help(shown)
            .background {
                RoundedRectangle(cornerRadius: theme.radius.s)
                    .fill(hovering ? theme.hover : .clear)
                    .padding(.horizontal, -theme.spacing.xs)
            }
            .overlay {
                if keyboardFocus && focused {
                    RoundedRectangle(cornerRadius: theme.radius.s + ButtonNodeView.ringGap)
                        .stroke(theme.accent, lineWidth: theme.focusRing)
                        .padding(.horizontal, -theme.spacing.xs - ButtonNodeView.ringGap)
                        .padding(.vertical, -ButtonNodeView.ringGap)
                }
            }
            .contentShape(Rectangle())
    }

    private func edit() {
        text = shown
        editing = true
    }

    private func cancel() {
        editing = false
        refocus()
    }

    private func refocus() {
        DispatchQueue.main.async { focused = true }
    }

    private func commit(refocus: Bool) {
        guard editing else { return }
        editing = false
        if refocus { self.refocus() }
        guard text != shown else { return }
        committed = text
        form?.values[field] = .string(text)
        if form?.autosave == true {
            form?.edited()
        } else if refocus {
            form?.submit()
        }
    }
}

private struct HeaderMoreMenu: View {
    let id: String
    let actions: [SDUINode]

    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    var body: some View {
        let presses = actions.map { ButtonPress(node: $0, dispatch: dispatch, form: form) }
        Menu {
            ForEach(Array(presses.enumerated()), id: \.offset) { _, press in
                Button(role: press.node.string("variant") == "danger" ? .destructive : nil, action: press.click) {
                    Text(title(press))
                }
                .disabled(press.disabled)
            }
        } label: {
            Image(systemName: "ellipsis")
                .font(theme.ui)
                .foregroundStyle(theme.text2)
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
        .frame(height: theme.control)
        .environment(\.colorScheme, theme.isDark ? .dark : .light)
        .help("More actions")
        .background {
            ForEach(Array(presses.enumerated()), id: \.offset) { _, press in
                Color.clear.keyMonitor(active: press.combo != nil && !press.disabled, overTerminals: press.combo?.passesTerminals == true, handle: press.handle)
            }
        }
        .testMenu(id, presses.map { LayoutMenuItem(title: title($0), enabled: !$0.disabled, destructive: $0.node.string("variant") == "danger") })
    }

    private func title(_ press: ButtonPress) -> String {
        let label = press.loading ?? press.node.string("label") ?? ""
        return press.combo.map { "\(label)    \($0.label)" } ?? label
    }
}

struct EmptyNodeView: View {
    let node: SDUINode

    init(node: SDUINode) {
        self.node = node
    }

    init(text: String) {
        node = SDUINode(type: "empty", props: ["text": .string(text)])
    }

    @Environment(\.theme) private var theme

    private var center: Bool { node.bool("center") == true }

    var body: some View {
        VStack(alignment: center ? .center : .leading, spacing: theme.spacing.m) {
            if let icon = node.string("icon") {
                Image(systemName: SymbolName.resolve(icon))
                    .font(.system(size: 16))
                    .foregroundStyle(theme.muted)
            }
            if let title = node.string("title") {
                Text(title)
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
            }
            Text(node.string("text") ?? "")
                .font(theme.ui)
                .foregroundStyle(theme.muted)
                .multilineTextAlignment(center ? .center : .leading)
                .fixedSize(horizontal: false, vertical: true)
            if let hint = node.string("hint") {
                Text(hint)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.text2)
                    .textSelection(.enabled)
                    .padding(.horizontal, theme.spacing.m)
                    .padding(.vertical, theme.spacing.xs)
                    .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(theme.surface2))
            }
            SDUIChildren(node: node)
        }
        .frame(maxWidth: center ? Self.centredWidth : nil)
        .padding(theme.gutter * 2)
        .padding(.top, center ? theme.spacing.section * 2 : 0)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: center ? .top : .topLeading)
    }

    static let centredWidth: CGFloat = 560
}

struct UnsupportedNodeView: View {
    let type: String

    @Environment(\.theme) private var theme

    var body: some View {
        Text("unsupported: \(type)")
            .font(theme.uiSmall)
            .foregroundStyle(theme.muted)
    }
}

private struct HeaderTrailingInsetKey: EnvironmentKey {
    static let defaultValue: CGFloat = 0
}

private struct ButtonKeyHintKey: EnvironmentKey {
    static let defaultValue: String? = nil
}

private struct ButtonIsDefaultKey: EnvironmentKey {
    static let defaultValue = false
}

extension EnvironmentValues {
    var headerTrailingInset: CGFloat {
        get { self[HeaderTrailingInsetKey.self] }
        set { self[HeaderTrailingInsetKey.self] = newValue }
    }

    var buttonKeyHint: String? {
        get { self[ButtonKeyHintKey.self] }
        set { self[ButtonKeyHintKey.self] = newValue }
    }

    var buttonIsDefault: Bool {
        get { self[ButtonIsDefaultKey.self] }
        set { self[ButtonIsDefaultKey.self] = newValue }
    }
}
