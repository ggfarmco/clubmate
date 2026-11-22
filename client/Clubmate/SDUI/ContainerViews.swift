import Observation
import SwiftUI

import ClubmateKit

struct StackNodeView: View {
    let node: SDUINode

    @Environment(\.formKeyWidth) private var keyWidth
    @Environment(\.sduiPopover) private var popover
    @Environment(\.formSheet) private var formSheet
    @Environment(\.theme) private var theme
    @Environment(\.sduiGutter) private var gutter
    @Environment(\.sduiInsetButtons) private var insetButtons
    @Environment(\.barNote) private var barNote

    private var footer: Bool {
        (popover != nil || formSheet) && node.string("direction") == "horizontal" && !node.children.isEmpty && node.children.allSatisfy { $0.type == "button" }
    }

    private var gap: CGFloat {
        guard case .number(let value) = node.props["gap"] else { return 0 }
        return CGFloat(value)
    }

    private var alignment: HorizontalAlignment {
        switch node.string("align") {
        case "center": .center
        case "trailing": .trailing
        default: .leading
        }
    }

    private var propertyKey: SDUINode? {
        guard keyWidth != nil, node.string("direction") == "horizontal", node.children.count > 1, let first = node.children.first, first.type == "text", first.string("style") == "muted" else { return nil }
        return first
    }

    private static func filled(_ child: SDUINode) -> Bool {
        child.type == "button" && ["primary", "secondary", "danger"].contains(child.string("variant") ?? "")
    }

    private static func priority(_ child: SDUINode) -> Double {
        if child.type == "text", ["strong", "title"].contains(child.string("style")) { return 1 }
        return child.string("short") == nil && child.props["lines"]?.numberValue != 1 ? 0 : -1
    }

    var body: some View {
        switch node.string("inset") {
        case "bar": bar
        case "foot":
            stack
                .environment(\.sduiGutter, false)
                .padding(.top, theme.spacing.l + theme.spacing.xs)
                .padding(.horizontal, theme.spacing.xl)
                .padding(.bottom, theme.spacing.xl)
                .frame(maxWidth: .infinity, alignment: .leading)
        default: stack
        }
    }

    private var bar: some View {
        VStack(alignment: .leading, spacing: 0) {
            stack
                .environment(\.sduiGutter, false)
                .padding(.leading, theme.spacing.xl)
                .padding(.trailing, theme.spacing.m)
                .frame(maxWidth: .infinity, minHeight: theme.control + theme.spacing.m, maxHeight: theme.control + theme.spacing.m, alignment: .leading)
            if let barNote, node.bool("notes") == true {
                BarNoteView(note: barNote)
            }
        }
    }

    @ViewBuilder private var stack: some View {
        if let keyWidth, let key = propertyKey {
            HStack(alignment: .firstTextBaseline, spacing: gap) {
                SDUINodeView(node: key)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .help(key.string("text") ?? "")
                    .frame(width: keyWidth, alignment: .leading)
                ForEach(node.identifiedChildren.dropFirst()) { child in
                    SDUINodeView(node: child.node).equatable()
                }
            }
            .environment(\.formKeyWidth, nil)
        } else if footer {
            HStack(spacing: theme.spacing.m) {
                Spacer(minLength: 0)
                ForEach(node.identifiedChildren) { child in
                    SDUINodeView(node: child.node)
                        .equatable()
                        .environment(\.buttonKeyHint, formSheet && child.node.string("action") == FormSheetHost.cancelAction ? FormSheetHost.cancelHint : nil)
                }
            }
            .frame(maxWidth: .infinity)
        } else if node.string("direction") == "horizontal" {
            HStack(alignment: .center, spacing: gap) {
                ForEach(node.identifiedChildren) { child in
                    if child.node.type == "spacer" {
                        Spacer(minLength: 0)
                    } else if child.node.type == "divider" {
                        VerticalRule(node: child.node)
                    } else {
                        SDUINodeView(node: child.node)
                            .equatable()
                            .layoutPriority(node.string("inset") == "bar" && child.node.type == "button" ? 2 : Self.priority(child.node))
                    }
                }
            }
        } else {
            VStack(alignment: alignment, spacing: gap) {
                ForEach(node.identifiedChildren) { child in
                    SDUINodeView(node: child.node)
                        .equatable()
                        .padding(.horizontal, gutter && insetButtons && node.string("inset") == nil && Self.filled(child.node) ? theme.gutter : 0)
                        .layoutPriority(child.node.bool("dock") == true ? 1 : 0)
                }
            }
        }
    }
}

private struct VerticalRule: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        if TestMode.current {
            rule.modifier(SDUIProbe(node: node))
        } else {
            rule
        }
    }

    private var rule: some View {
        Rectangle()
            .fill(theme.border)
            .frame(width: 1, height: theme.spacing.xxl)
    }
}

struct SplitNodeView: View {
    let node: SDUINode

    var body: some View {
        if node.string("direction") == "vertical" {
            VSplitView { panes }
        } else {
            HSplitView { panes }
        }
    }

    private var panes: some View {
        ForEach(node.identifiedChildren) { child in
            SDUINodeView(node: child.node)
                .equatable()
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
    }
}

struct ScrollNodeView: View {
    let node: SDUINode

    var body: some View {
        if node.bool("fit") == true {
            FitScrollView(node: node)
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    SDUIChildren(node: node)
                        .frame(maxWidth: .infinity, alignment: .topLeading)
                }
                .environment(\.scrollProxy, proxy)
            }
        }
    }
}

private struct FitScrollView: View {
    let node: SDUINode

    @State private var height: CGFloat = 0

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                SDUIChildren(node: node)
                    .frame(maxWidth: .infinity, alignment: .topLeading)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
            }
            .frame(maxHeight: height)
            .clipped()
            .environment(\.scrollProxy, proxy)
        }
    }
}

struct OwnScroll<Content: View>: View {
    @ViewBuilder let content: Content

    @Environment(\.scrollProxy) private var scroll

    var body: some View {
        if scroll == nil {
            ScrollViewReader { proxy in
                ScrollView {
                    content
                        .frame(maxWidth: .infinity, alignment: .topLeading)
                }
                .clipped()
                .environment(\.scrollProxy, proxy)
            }
        } else {
            content
        }
    }
}

private struct ScrollProxyKey: EnvironmentKey {
    static var defaultValue: ScrollViewProxy? { nil }
}

extension EnvironmentValues {
    var scrollProxy: ScrollViewProxy? {
        get { self[ScrollProxyKey.self] }
        set { self[ScrollProxyKey.self] = newValue }
    }
}

struct FormNodeView: View {
    let node: SDUINode

    @State private var local = FormState()
    @Environment(\.composerDrafts) private var drafts
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme
    @Environment(\.sduiPopover) private var popover

    @State private var width: CGFloat = 0
    @State private var mount = UUID()

    private var key: String? { node.id.map { dispatch.slot + "|" + $0 } }

    private var state: FormState { key.flatMap { drafts?.form(for: $0) } ?? local }

    var body: some View {
        VStack(alignment: .leading, spacing: theme.gutter) {
            SDUIChildren(node: node)
        }
        .environment(\.formKeyWidth, theme.uiSize * (width < 480 ? 7.5 : 10) + theme.gutter * 2)
        .environment(\.formDocked, node.bool("dock") == true)
        .environment(\.formNarrow, width <= ChoiceNodeView.foldWidth)
        .environment(\.formAccordion, node.bool("accordion") == true || width <= ChoiceNodeView.foldWidth)
        .padding(.vertical, theme.spacing.l)
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .environment(state)
        .onChange(of: node.bool("autosave") == true, initial: true) { _, on in
            state.autosave = on
        }
        .onChange(of: node.props["needsAny"]?.arrayValue?.compactMap(\.stringValue) ?? [], initial: true) { _, ids in
            state.needsAny = ids
        }
        .onChange(of: UIForm.questionIDs(in: node), initial: true) { _, ids in
            state.questions = ids
        }
        .onChange(of: state.submissions) {
            guard state.sends(mount) else { return }
            send()
            popover?.dismiss()
        }
        .onAppear {
            state.mount(mount)
            popover?.submit = { [state] in state.submit() }
        }
        .onDisappear {
            if state.dirty, state.sends(mount) { send() }
            state.unmount(mount)
            if let key { drafts?.left(form: key) }
        }
        .onChange(of: UIForm.fieldIDs(in: node)) { old, ids in
            if state.dirty, state.sends(mount), !old.isSubset(of: ids) {
                send(.object(state.values.filter { old.contains($0.key) }))
            }
            state.values = state.values.filter { ids.contains($0.key) }
        }
    }

    private func send(_ values: JSONValue? = nil) {
        state.sent()
        dispatch(node, .submit(values ?? UIForm.collect(state.values, in: node))) { [state] _ in
            state.answered()
        }
    }
}

private struct FormNarrowKey: EnvironmentKey {
    static let defaultValue = false
}

private struct FormAccordionKey: EnvironmentKey {
    static let defaultValue = false
}

private struct FormKeyWidthKey: EnvironmentKey {
    static let defaultValue: CGFloat? = nil
}

extension EnvironmentValues {
    var formKeyWidth: CGFloat? {
        get { self[FormKeyWidthKey.self] }
        set { self[FormKeyWidthKey.self] = newValue }
    }

    var formNarrow: Bool {
        get { self[FormNarrowKey.self] }
        set { self[FormNarrowKey.self] = newValue }
    }

    var formAccordion: Bool {
        get { self[FormAccordionKey.self] }
        set { self[FormAccordionKey.self] = newValue }
    }

    @Entry var formDocked = false
}
