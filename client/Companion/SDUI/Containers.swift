import SwiftUI

import CompanionKit

struct StackNode: View {
    let node: SDUINode
    let context: SDUIContext
    @Environment(\.sduiInset) private var inset
    @Environment(\.sduiFormRow) private var formRow

    private var gap: CGFloat? {
        node.props["gap"]?.numberValue.map { CGFloat($0) }
    }

    private var propertyKey: SDUINode? {
        guard formRow, node.children.count > 1, let first = node.children.first, first.type == "text", first.string("style") == "muted" else { return nil }
        return first
    }

    var body: some View {
        if let key = propertyKey {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                Text(key.string("text") ?? "")
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                Spacer(minLength: 8)
                ForEach(node.identifiedChildren.dropFirst()) { child in
                    SDUINodeView(node: child.node, context: context)
                }
            }
        } else if node.string("direction") == "horizontal" {
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .center, spacing: gap ?? 8) {
                    ForEach(node.identifiedChildren) { child in
                        if child.node.type == "spacer" {
                            Spacer(minLength: 0)
                        } else {
                            SDUINodeView(node: child.node, context: context)
                        }
                    }
                }
                FlowLayout(spacing: gap ?? 8) { SDUIChildren(node: node, context: context) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .environment(\.sduiInset, 0)
            .padding(.horizontal, inset)
        } else {
            VStack(alignment: .leading, spacing: gap ?? 12) { SDUIChildren(node: node, context: context) }
                .frame(maxWidth: .infinity, alignment: .leading)
                .scrollsUnlessNested(node)
        }
    }
}

private struct ScrollsUnlessNested: ViewModifier {
    let node: SDUINode
    @Environment(\.sduiInScroll) private var inScroll
    @Environment(\.sduiFramed) private var framed

    private static let scrolling: Set<String> = ["scroll", "split", "list", "tree", "table", "transcript"]

    private static func scrollsItself(_ node: SDUINode) -> Bool {
        if scrolling.contains(node.type) { return true }
        if node.type == "document", node.string("size") != "fit" { return true }
        return node.children.contains(where: scrollsItself)
    }

    func body(content: Content) -> some View {
        if inScroll || framed || Self.scrollsItself(node) {
            content.environment(\.sduiFramed, true)
        } else {
            ScrollView {
                content.padding(.vertical, 12)
            }
            .scrollDismissesKeyboard(.interactively)
            .environment(\.sduiInScroll, true)
        }
    }
}

extension View {
    func scrollsUnlessNested(_ node: SDUINode) -> some View {
        modifier(ScrollsUnlessNested(node: node))
    }
}

struct SplitNode: View {
    let node: SDUINode
    let context: SDUIContext
    @Environment(\.horizontalSizeClass) private var size
    @Environment(\.sduiInScroll) private var inScroll

    var body: some View {
        if size == .regular, node.string("direction") == "horizontal" {
            HStack(alignment: .top, spacing: 0) {
                ForEach(Array(node.identifiedChildren.enumerated()), id: \.element.id) { index, child in
                    if index > 0 { Divider() }
                    pane(child)
                }
            }
        } else {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(node.identifiedChildren.enumerated()), id: \.element.id) { index, child in
                    if index > 0 { Divider().padding(.vertical, inScroll ? 12 : 0) }
                    pane(child)
                }
            }
        }
    }

    private func pane(_ child: SDUIChild) -> some View {
        SDUINodeView(node: child.node, context: context)
            .fixedSize(horizontal: false, vertical: inScroll)
            .frame(maxWidth: .infinity, maxHeight: inScroll ? nil : .infinity, alignment: .topLeading)
    }
}

struct ScrollNode: View {
    let node: SDUINode
    let context: SDUIContext
    @Environment(\.sduiInScroll) private var inScroll

    var body: some View {
        let column = VStack(alignment: .leading, spacing: 12) { SDUIChildren(node: node, context: context) }
            .frame(maxWidth: .infinity, alignment: .topLeading)
        if inScroll {
            column
        } else {
            ScrollView {
                column.padding(.vertical, 12)
            }
            .scrollDismissesKeyboard(.interactively)
            .environment(\.sduiInScroll, true)
        }
    }
}

struct CardNode: View {
    let node: SDUINode
    let context: SDUIContext
    @Environment(\.sduiInset) private var inset

    var body: some View {
        let tone = SDUITone.fill(node.string("tone"))
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            if let icon = node.string("icon") {
                Image(systemName: icon)
                    .foregroundStyle(SDUITone.ink(node.string("tone")) ?? .secondary)
            }
            VStack(alignment: .leading, spacing: 10) { SDUIChildren(node: node, context: context) }
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .environment(\.sduiInset, 0)
        .padding(16)
        .background {
            RoundedRectangle(cornerRadius: 20).fill(Color.surface1)
            if let tone { RoundedRectangle(cornerRadius: 20).fill(tone.opacity(0.15)) }
        }
        .overlay {
            if let tone { RoundedRectangle(cornerRadius: 20).strokeBorder(tone.opacity(0.55), lineWidth: 1) }
        }
        .padding(.horizontal, inset)
    }
}

struct FormNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var state = SDUIFormState()
    @Environment(\.sduiInset) private var inset

    private struct Chunk: Identifiable {
        let id: String
        let rows: [SDUIChild]
        let grouped: Bool
    }

    private var chunks: [Chunk] {
        var chunks: [Chunk] = []
        for child in node.identifiedChildren {
            let grouped = Self.isRow(child.node)
            if grouped, let last = chunks.last, last.grouped {
                chunks[chunks.count - 1] = Chunk(id: last.id, rows: last.rows + [child], grouped: true)
            } else {
                chunks.append(Chunk(id: child.id, rows: [child], grouped: grouped))
            }
        }
        return chunks
    }

    private static func isRow(_ node: SDUINode) -> Bool {
        if UIForm.fieldTypes.contains(node.type) { return true }
        guard node.type == "stack", node.string("direction") == "horizontal" else { return false }
        if node.children.first?.type == "text", node.children.first?.string("style") == "muted" { return true }
        return node.children.contains { UIForm.fieldTypes.contains($0.type) } && !node.children.contains { $0.type == "button" }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            ForEach(chunks) { chunk in
                if chunk.grouped {
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(Array(chunk.rows.enumerated()), id: \.element.id) { index, row in
                            if index > 0 { Divider().padding(.leading, 16) }
                            SDUINodeView(node: row.node, context: context)
                                .padding(.horizontal, 16)
                                .padding(.vertical, 6)
                                .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        }
                    }
                    .environment(\.sduiInset, 0)
                    .environment(\.sduiFormRow, true)
                    .background(Color.surface1, in: .rect(cornerRadius: 22))
                    .padding(.horizontal, inset)
                } else {
                    ForEach(chunk.rows) { row in
                        SDUINodeView(node: row.node, context: context)
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .scrollsUnlessNested(node)
        .environment(\.sduiForm, state)
        .onChange(of: node, initial: true) { state.update(node, context: context) }
        .onDisappear { state.flush() }
    }
}

struct FlowLayout: Layout {
    let spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let frames = place(subviews, width: proposal.width ?? .infinity)
        let width = frames.map(\.maxX).max() ?? 0
        return CGSize(width: proposal.width ?? width, height: frames.map(\.maxY).max() ?? 0)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for (subview, frame) in zip(subviews, place(subviews, width: bounds.width)) {
            subview.place(at: CGPoint(x: bounds.minX + frame.minX, y: bounds.minY + frame.minY), proposal: ProposedViewSize(frame.size))
        }
    }

    private func place(_ subviews: Subviews, width: CGFloat) -> [CGRect] {
        var frames: [CGRect] = []
        var x: CGFloat = 0
        var y: CGFloat = 0
        var row: CGFloat = 0
        for subview in subviews {
            var size = subview.sizeThatFits(.unspecified)
            if size.width > width {
                size = subview.sizeThatFits(ProposedViewSize(width: width, height: nil))
                size.width = min(size.width, width)
            }
            if x > 0, x + size.width > width {
                x = 0
                y += row + spacing
                row = 0
            }
            frames.append(CGRect(origin: CGPoint(x: x, y: y), size: size))
            x += size.width + spacing
            row = max(row, size.height)
        }
        return frames
    }
}

enum SDUITone {
    static func ink(_ tone: String?) -> Color? {
        switch tone {
        case "accent": .accentColor
        case "success", "ok": .successInk
        case "warning", "warn": .warningInk
        case "danger": .dangerInk
        default: nil
        }
    }

    static func fill(_ tone: String?) -> Color? {
        switch tone {
        case "accent": .accentColor
        case "success", "ok": .success
        case "warning", "warn": .warning
        case "danger": .danger
        default: nil
        }
    }
}
