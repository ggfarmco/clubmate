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
    @State private var anchor = FocusAnchor()
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
        VStack(alignment: .leading, spacing: presented ? theme.spacing.l : theme.spacing.xl) {
            if !(node.string("title") ?? "").isEmpty {
                title
            }
            ScrollView {
                VStack(alignment: .leading, spacing: presented ? theme.spacing.l : theme.spacing.xl) {
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
                    if let note = node.string("note") {
                        Text(note)
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.tone(node.string("noteTone")) ?? theme.muted)
                            .lineLimit(2)
                            .padding(.leading, theme.gutter)
                            .layoutPriority(-1)
                    }
                    let leading = presented ? [] : actions.children.filter { $0.string("variant") == "danger" }
                    ForEach(Array(leading.enumerated()), id: \.offset) { _, button in
                        SDUINodeView(node: button)
                    }
                    Spacer(minLength: 0)
                    ForEach(Array(actions.children.filter { presented || $0.string("variant") != "danger" }.enumerated()), id: \.offset) { _, button in
                        let cancel = button.string("action") == node.string("cancel")
                        SDUINodeView(node: button)
                            .environment(\.buttonKeyHint, cancel ? "⎋" : nil)
                            .environment(\.buttonIsDefault, cancel && presented)
                    }
                }
            }
        }
        .font(theme.ui)
        .padding(.vertical, presented ? theme.gutter * 2 : 14)
        .padding(.horizontal, presented ? theme.gutter : theme.spacing.xxl - theme.gutter)
        .frame(width: presented ? Self.width : nil, alignment: .leading)
        .frame(maxWidth: presented ? nil : .infinity, alignment: .leading)
        .background {
            if !presented {
                RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.background)
                    .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.border, lineWidth: 1))
            }
        }
        .presentationBackground(theme.surface3)
        .interactiveDismissDisabled()
        .background {
            if !presented { FocusAnchorView(anchor: anchor) }
        }
        .keyMonitor(active: true) { event in
            guard event.modifierFlags.intersection([.command, .control, .option]).isEmpty,
                  event.keyCode == 53 || presented && (event.keyCode == 36 || event.keyCode == 76) else { return false }
            if !presented, !anchor.holdsFocus(in: event.window ?? anchor.view?.window) { return false }
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

final class FocusAnchor {
    weak var view: NSView?

    @MainActor
    func holdsFocus(in window: NSWindow?) -> Bool {
        guard let view, let window, view.window === window, let focused = Self.view(of: window.firstResponder) else { return false }
        let frame = focused.convert(focused.bounds, to: view)
        return view.bounds.contains(NSPoint(x: frame.midX, y: frame.midY))
    }

    @MainActor
    private static func view(of responder: NSResponder?) -> NSView? {
        if let text = responder as? NSTextView, text.isFieldEditor, let owner = text.delegate as? NSView { return owner }
        return responder as? NSView
    }
}

private struct FocusAnchorView: NSViewRepresentable {
    let anchor: FocusAnchor

    func makeNSView(context: Context) -> NSView {
        let view = PassThroughView()
        anchor.view = view
        return view
    }

    func updateNSView(_ view: NSView, context: Context) {
        anchor.view = view
    }
}

private final class PassThroughView: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
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
