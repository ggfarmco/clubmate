import Observation
import SwiftUI

import ClubmateKit

struct StackNodeView: View {
    let node: SDUINode

    @Environment(\.formKeyWidth) private var keyWidth

    private var gap: CGFloat {
        guard case .number(let value) = node.props["gap"] else { return 0 }
        return CGFloat(value)
    }

    private var propertyKey: SDUINode? {
        guard keyWidth != nil, node.children.count > 1, let first = node.children.first, first.type == "text", first.string("style") == "muted" else { return nil }
        return first
    }

    var body: some View {
        if let keyWidth, let key = propertyKey {
            HStack(alignment: .firstTextBaseline, spacing: gap) {
                SDUINodeView(node: key)
                    .frame(width: keyWidth, alignment: .leading)
                ForEach(node.identifiedChildren.dropFirst()) { child in
                    SDUINodeView(node: child.node)
                }
            }
            .environment(\.formKeyWidth, nil)
        } else if node.string("direction") == "horizontal" {
            HStack(alignment: .center, spacing: gap) { SDUIChildren(node: node) }
        } else {
            VStack(alignment: .leading, spacing: gap) { SDUIChildren(node: node) }
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

@MainActor
@Observable
final class FormState {
    static let typingDelay = Duration.milliseconds(600)

    var values: [String: JSONValue] = [:]
    private(set) var submissions = 0
    @ObservationIgnored var autosave = false
    @ObservationIgnored private(set) var dirty = false
    @ObservationIgnored private var pending: Task<Void, Never>?

    func submit() {
        pending?.cancel()
        pending = nil
        submissions += 1
    }

    func edited(after delay: Duration? = nil) {
        guard autosave else { return }
        dirty = true
        pending?.cancel()
        guard let delay else { return submit() }
        pending = Task { [weak self] in
            try? await Task.sleep(for: delay)
            guard !Task.isCancelled else { return }
            self?.submit()
        }
    }

    func flush() {
        if dirty { submit() }
    }

    func sent() {
        pending?.cancel()
        pending = nil
        dirty = false
    }
}

struct FormNodeView: View {
    let node: SDUINode

    @State private var state = FormState()
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    @State private var width: CGFloat = 0

    var body: some View {
        VStack(alignment: .leading, spacing: theme.gutter) {
            SDUIChildren(node: node)
        }
        .environment(\.formKeyWidth, width < 480 ? 88 : 120)
        .padding(.vertical, theme.spacing.l)
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .environment(state)
        .onChange(of: node.bool("autosave") == true, initial: true) { _, on in
            state.autosave = on
        }
        .onChange(of: state.submissions) {
            send()
        }
        .onDisappear {
            if state.dirty { send() }
        }
        .onChange(of: UIForm.fieldIDs(in: node)) { _, ids in
            state.values = state.values.filter { ids.contains($0.key) }
        }
    }

    private func send() {
        state.sent()
        dispatch(node, .submit(UIForm.collect(state.values, in: node)))
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
