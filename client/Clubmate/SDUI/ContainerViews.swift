import Observation
import SwiftUI

import ClubmateKit

struct StackNodeView: View {
    let node: SDUINode

    @Environment(\.formKeyWidth) private var keyWidth
    @Environment(\.sduiPopover) private var popover
    @Environment(\.formSheet) private var formSheet
    @Environment(\.theme) private var theme

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

    var body: some View {
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
                    } else {
                        SDUINodeView(node: child.node).equatable()
                    }
                }
            }
        } else {
            VStack(alignment: alignment, spacing: gap) { SDUIChildren(node: node) }
        }
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
        ScrollViewReader { proxy in
            ScrollView {
                SDUIChildren(node: node)
                    .frame(maxWidth: .infinity, alignment: .topLeading)
            }
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

    private var key: String? { node.id.map { dispatch.slot + "|" + $0 } }

    private var state: FormState { key.flatMap { drafts?.form(for: $0) } ?? local }

    var body: some View {
        VStack(alignment: .leading, spacing: theme.gutter) {
            SDUIChildren(node: node)
        }
        .environment(\.formKeyWidth, theme.uiSize * (width < 480 ? 7.5 : 10) + theme.gutter * 2)
        .padding(.vertical, theme.spacing.l)
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .environment(state)
        .onChange(of: node.bool("autosave") == true, initial: true) { _, on in
            state.autosave = on
        }
        .onChange(of: state.submissions) {
            send()
            popover?.dismiss()
        }
        .onAppear {
            popover?.submit = { [state] in state.submit() }
        }
        .onDisappear {
            if state.dirty { send() }
            if let key { drafts?.left(form: key) }
        }
        .onChange(of: UIForm.fieldIDs(in: node)) { old, ids in
            if state.dirty, !old.isSubset(of: ids) {
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

private struct FormKeyWidthKey: EnvironmentKey {
    static let defaultValue: CGFloat? = nil
}

extension EnvironmentValues {
    var formKeyWidth: CGFloat? {
        get { self[FormKeyWidthKey.self] }
        set { self[FormKeyWidthKey.self] = newValue }
    }
}
