import Observation
import SwiftUI

import ClubmateKit

struct StackNodeView: View {
    let node: SDUINode

    private var gap: CGFloat {
        guard case .number(let value) = node.props["gap"] else { return 0 }
        return CGFloat(value)
    }

    var body: some View {
        if node.string("direction") == "horizontal" {
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
    var values: [String: JSONValue] = [:]
    private(set) var submissions = 0

    func submit() {
        submissions += 1
    }
}

struct FormNodeView: View {
    let node: SDUINode

    @State private var state = FormState()
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.gutter) {
            SDUIChildren(node: node)
        }
        .environment(state)
        .onChange(of: state.submissions) {
            dispatch(node, .submit(UIForm.collect(state.values, in: node)))
        }
        .onChange(of: UIForm.fieldIDs(in: node)) { _, ids in
            state.values = state.values.filter { ids.contains($0.key) }
        }
    }
}
