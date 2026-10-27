import AppKit
import SwiftUI

import ClubmateKit

struct TabsNodeView: View {
    let node: SDUINode

    @State private var selected = ""
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: 0) {
            ForEach(node.items()) { item in
                TabButton(item: item, selected: item.id == selected) {
                    selected = item.id
                    dispatch(node, .select(item.id))
                }
            }
            Spacer(minLength: 0)
        }
        .frame(height: theme.rowHeight + 4)
        .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
        .onChange(of: node.string("selected"), initial: true) { _, value in
            selected = value ?? ""
        }
    }
}

private struct TabButton: View {
    let item: SDUIItem
    let selected: Bool
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: 5) {
                if let icon = item.icon {
                    Image(systemName: SymbolName.resolve(icon))
                }
                Text(item.label)
                if let badge = item.badge {
                    BadgeLabel(text: badge, tone: theme.muted)
                }
            }
            .font(theme.ui)
            .foregroundStyle(selected || hovering ? theme.foreground : theme.muted)
            .padding(.horizontal, theme.gutter * 1.5)
            .frame(maxHeight: .infinity)
            .background(selected ? theme.foreground.opacity(0.06) : .clear, ignoresSafeAreaEdges: [])
            .overlay(alignment: .bottom) {
                if selected { theme.accent.frame(height: 1) }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}

struct ListNodeView: View {
    let node: SDUINode

    @State private var selected: String?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(node.items()) { item in
                ItemRow(item: item, selected: item.id == selected) {
                    selected = item.id
                    focused = true
                    dispatch(node, .select(item.id))
                }
            }
        }
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .onChange(of: node.string("selected"), initial: true) { _, value in
            selected = value
        }
    }
}

struct TreeNodeView: View {
    let node: SDUINode

    @State private var selected: String?
    @State private var cursor: String?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.scrollProxy) private var scroll
    @Environment(\.focusRouter) private var router

    private struct Row: Identifiable {
        let item: SDUITreeItem
        let depth: Int

        var id: String { item.id }
    }

    private var rows: [Row] {
        var rows: [Row] = []
        func visit(_ items: [SDUITreeItem], _ depth: Int) {
            for item in items {
                rows.append(Row(item: item, depth: depth))
                if item.expanded { visit(item.children, depth + 1) }
            }
        }
        visit(node.treeItems(), 0)
        return rows
    }

    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(rows) { row in
                ItemRow(item: row.item.item, selected: row.id == selected, cursor: focused && row.id == cursor, muted: row.item.muted, indent: CGFloat(row.depth) * 12) {
                    cursor = row.id
                    focused = true
                    select(row.id)
                } leading: {
                    Disclosure(item: row.item) { expanded in
                        dispatch(node, expanded ? .collapse(row.id) : .expand(row.id))
                    }
                }
                .id(row.id)
            }
        }
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .keyMonitor(active: focused, handle: handle)
        .onChange(of: node.string("selected"), initial: true) { _, value in
            guard value != selected else { return }
            selected = value
            if let value { scroll?.scrollTo(value, anchor: .center) }
        }
        .onChange(of: router?.pending, initial: true) {
            guard dispatch.slot.hasSuffix(".sidebar"), router?.take(dispatch.slot) == true else { return }
            cursor = selected ?? rows.first?.id
            focused = true
        }
    }

    private func select(_ id: String) {
        selected = id
        dispatch(node, .select(id))
    }

    private func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        guard let action = TreeKeys.action(keyCode: event.keyCode, control: flags.contains(.control), command: flags.contains(.command)) else { return false }
        switch TreeCursor.resolve(action, cursor: cursor, in: TreeCursor.rows(node.treeItems())) {
        case .cursor(let id):
            cursor = id
            scroll?.scrollTo(id)
        case .select(let id): select(id)
        case .expand(let id): dispatch(node, .expand(id))
        case .collapse(let id): dispatch(node, .collapse(id))
        case .focusMain: router?.requestMain(from: dispatch.slot)
        case nil: break
        }
        return true
    }
}

private struct Disclosure: View {
    let item: SDUITreeItem
    let toggle: (Bool) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        if item.hasChildren {
            Button { toggle(item.expanded) } label: {
                Image(systemName: item.expanded ? "chevron.down" : "chevron.right")
                    .font(.system(size: 8, weight: .semibold))
                    .foregroundStyle(theme.muted)
                    .frame(width: 12, height: theme.rowHeight)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        } else {
            Color.clear.frame(width: 12)
        }
    }
}

struct ItemRow<Leading: View>: View {
    let item: SDUIItem
    let selected: Bool
    var cursor = false
    var muted = false
    var indent: CGFloat = 0
    let action: () -> Void
    @ViewBuilder let leading: () -> Leading

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: 6) {
            leading()
            if let icon = item.icon {
                Image(systemName: SymbolName.resolve(icon))
                    .foregroundStyle(theme.muted)
                    .frame(width: 14)
            }
            Text(item.label)
                .foregroundStyle(muted ? theme.muted : theme.foreground)
                .lineLimit(1)
            Spacer(minLength: theme.gutter)
            if let detail = item.detail {
                Text(detail)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            if let badge = item.badge {
                BadgeLabel(text: badge, tone: theme.accent)
            }
        }
        .font(theme.ui)
        .padding(.leading, theme.gutter + indent)
        .padding(.trailing, theme.gutter)
        .frame(height: theme.rowHeight)
        .background(background, ignoresSafeAreaEdges: [])
        .overlay {
            if cursor { Rectangle().strokeBorder(theme.accent, lineWidth: 1) }
        }
        .contentShape(Rectangle())
        .onTapGesture(perform: action)
        .onHover { hovering = $0 }
    }

    private var background: Color {
        if selected { return theme.accent.opacity(0.25) }
        return hovering ? theme.foreground.opacity(0.05) : .clear
    }
}

extension ItemRow where Leading == EmptyView {
    init(item: SDUIItem, selected: Bool, action: @escaping () -> Void) {
        self.init(item: item, selected: selected, action: action) { EmptyView() }
    }
}
