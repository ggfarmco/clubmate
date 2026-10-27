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
        ScrollView {
            SDUIChildren(node: node)
                .frame(maxWidth: .infinity, alignment: .topLeading)
        }
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
            dispatch(node, .submit(.object(state.values)))
        }
        .onChange(of: fieldIDs) { _, ids in
            state.values = state.values.filter { ids.contains($0.key) }
        }
    }

    private var fieldIDs: Set<String> {
        var ids: Set<String> = []
        func visit(_ children: [SDUINode]) {
            for child in children where child.type != "form" {
                if FormNodeView.fieldTypes.contains(child.type), let id = child.id { ids.insert(id) }
                visit(child.children)
            }
        }
        visit(node.children)
        return ids
    }

    private static let fieldTypes: Set<String> = ["textField", "toggle", "select"]
}
