import AppKit
import SwiftUI

import ClubmateKit

struct SDUISheet: Identifiable, Equatable {
    let node: SDUINode

    var id: String { node.id ?? "sheet" }
}

extension SDUINode {
    var rootSheet: SDUISheet? {
        children.last { $0.type == "sheet" }.map(SDUISheet.init)
    }

    var withoutSheets: SDUINode {
        var copy = self
        copy.children.removeAll { $0.type == "sheet" }
        return copy
    }

    var sheetCancelButton: SDUINode? {
        guard let cancel = string("cancel") else { return nil }
        func find(_ nodes: [SDUINode]) -> SDUINode? {
            for node in nodes {
                if node.type == "button", node.string("action") == cancel { return node }
                if let found = find(node.children) { return found }
            }
            return nil
        }
        return find(children)
    }
}

struct SheetNodeView: View {
    let node: SDUINode
    var presented = false

    @State private var bodyHeight: CGFloat = 0
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    private static let width: CGFloat = 560
    private static let maxBodyHeight: CGFloat = 520

    private var actions: SDUINode? {
        guard let last = node.children.last, last.type == "stack", last.string("direction") == "horizontal",
              !last.children.isEmpty, last.children.allSatisfy({ $0.type == "button" }) else { return nil }
        return last
    }

    var body: some View {
        let actions = actions
        let content = actions == nil ? node.children : Array(node.children.dropLast())
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            title
            ScrollView {
                VStack(alignment: .leading, spacing: theme.spacing.l) {
                    ForEach(Array(content.enumerated()), id: \.offset) { _, child in
                        SDUINodeView(node: child).equatable()
                    }
                }
                .environment(\.sheetInset, true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { bodyHeight = $0 }
            }
            .scrollIndicators(.automatic)
            .frame(height: min(max(bodyHeight, 1), Self.maxBodyHeight))
            if let actions {
                HStack(spacing: theme.spacing.m) {
                    Spacer(minLength: 0)
                    ForEach(Array(actions.children.enumerated()), id: \.offset) { _, button in
                        let cancel = button.string("action") == node.string("cancel")
                        SDUINodeView(node: button)
                            .environment(\.buttonKeyHint, cancel ? "⎋" : nil)
                            .environment(\.buttonIsDefault, cancel)
                    }
                }
            }
        }
        .font(theme.ui)
        .padding(.vertical, theme.gutter * 2)
        .padding(.horizontal, theme.gutter)
        .frame(width: presented ? Self.width : nil, alignment: .leading)
        .frame(maxWidth: presented ? nil : Self.width, alignment: .leading)
        .background {
            if !presented {
                RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.surface3)
                    .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.border, lineWidth: 1))
            }
        }
        .presentationBackground(theme.surface3)
        .interactiveDismissDisabled()
        .keyMonitor(active: presented) { event in
            guard event.modifierFlags.intersection([.command, .control, .option]).isEmpty,
                  event.keyCode == 53 || event.keyCode == 36 || event.keyCode == 76 else { return false }
            if event.keyCode != 53, event.window?.firstResponder is NSTextView { return false }
            guard let cancel = node.sheetCancelButton else { return false }
            dispatch(cancel, .click)
            return true
        }
    }

    private var title: some View {
        HStack(spacing: theme.spacing.m) {
            if let icon = node.string("icon") {
                Image(systemName: SymbolName.resolve(icon))
                    .foregroundStyle(theme.tone(node.string("tone")) ?? theme.text2)
            }
            Text(node.string("title") ?? "")
                .font(theme.heading)
                .foregroundStyle(theme.foreground)
                .lineLimit(2)
        }
        .padding(.horizontal, theme.gutter)
    }
}

private struct SheetInsetKey: EnvironmentKey {
    static let defaultValue = false
}

extension EnvironmentValues {
    var sheetInset: Bool {
        get { self[SheetInsetKey.self] }
        set { self[SheetInsetKey.self] = newValue }
    }
}
