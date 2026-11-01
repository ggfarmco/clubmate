import AppKit
import SwiftUI

import ClubmateKit

struct TabsNodeView: View {
    let node: SDUINode

    @State private var selected = ""
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        let items = node.items()
        ViewThatFits(in: .horizontal) {
            chips(items, compact: false)
            chips(items, compact: true)
        }
        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, theme.gutter)
        .padding(.vertical, theme.spacing.s)
        .onChange(of: node.string("selected"), initial: true) { _, value in
            selected = value ?? ""
        }
    }

    private func chips(_ items: [SDUIItem], compact: Bool) -> some View {
        HStack(spacing: theme.spacing.s) {
            ForEach(items) { item in
                TabButton(item: item, compact: compact, selected: item.id == selected) {
                    selected = item.id
                    dispatch(node, .select(item.id))
                }
            }
        }
        .fixedSize()
    }
}

struct TabButton: View {
    let item: SDUIItem
    let compact: Bool
    let selected: Bool
    let action: () -> Void

    @State private var hovering = false
    @State private var keyboardFocus = false
    @FocusState private var focused: Bool
    @Environment(\.theme) private var theme

    var body: some View {
        let ink = theme.tone(item.tone)
        let icon = compact ? item.icon.flatMap { $0.isEmpty ? nil : $0 } : nil
        Button(action: action) {
            HStack(spacing: theme.spacing.s) {
                if let icon {
                    Image(systemName: SymbolName.resolve(icon))
                } else {
                    Text(item.label)
                }
                if let badge = item.badge {
                    Text(badge).monospacedDigit()
                }
            }
            .font(theme.uiSmall)
            .lineLimit(1)
            .foregroundStyle(ink ?? (selected || hovering ? theme.foreground : theme.text2))
            .padding(.horizontal, theme.spacing.l)
            .frame(height: theme.rowHeight)
            .background(Capsule().fill(fill))
            .overlay {
                if keyboardFocus && focused {
                    Capsule()
                        .stroke(theme.accent, lineWidth: theme.focusRing)
                        .padding(-ButtonNodeView.ringGap)
                }
            }
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .focusable(interactions: .activate)
        .focusEffectDisabled()
        .focused($focused)
        .onChange(of: focused) { _, now in
            keyboardFocus = now && !ButtonNodeView.mouseEvents.contains(NSApp.currentEvent?.type ?? .keyDown)
        }
        .onKeyPress(keys: [.space, .return]) { _ in
            action()
            return .handled
        }
        .help(item.label)
        .accessibilityLabel([item.label, item.badge].compactMap { $0 }.joined(separator: " "))
        .accessibilityAddTraits(selected ? .isSelected : [])
        .onHover { hovering = $0 }
    }

    private var fill: Color {
        if let tone = theme.toneFill(item.tone) { return selected ? theme.toneBorder(tone) : theme.toneBg(tone) }
        return selected || hovering ? theme.pressed : theme.hover
    }
}

struct ListNodeView: View {
    let node: SDUINode

    @State private var selected: String?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.scrollProxy) private var scroll

    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(node.items()) { item in
                ItemRow(item: item, selected: item.id == selected, active: focused, tone: item.tone) {
                    selected = item.id
                    focused = true
                    dispatch(node, .select(item.id))
                }
                .itemMenu(item.menu) { dispatch.choose($0, item: item.id, in: node) }
                .id(item.id)
            }
        }
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .onChange(of: node.string("selected"), initial: true) { _, value in
            guard value != selected else { return }
            selected = value
            if let value { scroll?.scrollTo(value, anchor: .center) }
        }
    }
}

struct TreeNodeView: View {
    let node: SDUINode

    @State private var selected: String?
    @State private var cursor: String?
    @State private var leader = false
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.scrollProxy) private var scroll
    @Environment(\.focusRouter) private var router
    @Environment(\.sidebarSide) private var side
    @Environment(\.sidebarSlot) private var sidebarSlot
    @Environment(\.sidebarHasField) private var sidebarHasField
    @Environment(\.sidebarTabs) private var sidebarTabs

    private static let stickyLimit = 4

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
        let rows = rows
        LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(rows) { row in
                itemRow(row, selected: row.id == selected, cursor: focused && row.id == cursor) {
                    cursor = row.id
                    focused = true
                    select(row.id)
                }
                .id(row.id)
            }
        }
        .disclosureAnimation(Set(rows.filter(\.item.expanded).map(\.id)))
        .overlay(alignment: .top) {
            if scroll != nil {
                let tree = TreeCursor.rows(node.treeItems())
                StickyHeaders(rows: tree, limit: Self.stickyLimit) { id in
                    if let row = rows.first(where: { $0.id == id }) {
                        itemRow(row, selected: false, cursor: false) {
                            if let target = TreeCursor.stickyTarget(id, in: tree, limit: Self.stickyLimit) {
                                scroll?.scrollTo(target, anchor: .top)
                            }
                        }
                    }
                }
            }
        }
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .keyMonitor(active: focused, handle: handle)
        .reportsFocus(.tree(field: sidebarHasField), focused)
        .onChange(of: focused) { _, focused in
            if !focused { leader = false }
        }
        .onChange(of: node.string("selected"), initial: true) { _, value in
            guard value != selected else { return }
            selected = value
            if let value { scroll?.scrollTo(value, anchor: .center) }
        }
        .onChange(of: router?.pending, initial: true) {
            guard let sidebarSlot, router?.take(sidebarSlot) == true else { return }
            cursor = selected ?? rows.first?.id
            focused = true
        }
    }

    private func itemRow(_ row: Row, selected: Bool, cursor: Bool, action: @escaping () -> Void) -> some View {
        ItemRow(item: row.item.item, selected: selected, active: focused, cursor: cursor, muted: row.item.muted, faint: row.item.faint, tone: row.item.tone, depth: row.depth, action: action) {
            Disclosure(hasChildren: row.item.hasChildren, expanded: row.item.expanded) { expanded in
                dispatch(node, expanded ? .collapse(row.id) : .expand(row.id))
            }
        }
        .itemMenu(row.item.item.menu) { dispatch.choose($0, item: row.id, in: node) }
    }

    private func select(_ id: String) {
        selected = id
        dispatch(node, .select(id))
    }

    private func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        let plain = flags.intersection([.command, .control, .option, .shift]).isEmpty
        if leader {
            leader = false
            guard plain else { return false }
            chord(TreeChord.resolve(event.keyCode))
            return true
        }
        if plain, event.keyCode == TreeChord.leader {
            leader = true
            return true
        }
        let rows = TreeCursor.rows(node.treeItems())
        guard let action = TreeKeys.action(keyCode: event.keyCode, control: flags.contains(.control), command: flags.contains(.command), side: side, shift: flags.contains(.shift)) else { return false }
        switch TreeCursor.resolve(action, cursor: cursor, in: rows) {
        case .cursor(let id):
            if let target = TreeCursor.scrollTarget(id, from: cursor, in: rows, limit: Self.stickyLimit) { scroll?.scrollTo(target) }
            cursor = id
        case .select(let id): select(id)
        case .expand(let id): dispatch(node, .expand(id))
        case .collapse(let id): dispatch(node, .collapse(id))
        case .focusMain: router?.requestMain(from: sidebarSlot ?? dispatch.slot)
        case .cycleTabs(let delta): cycleTabs(by: delta)
        case nil: break
        }
        return true
    }

    private func cycleTabs(by delta: Int) {
        guard let tabs = sidebarTabs, let next = TabCycle.next(tabs.items().map(\.id), from: tabs.string("selected"), by: delta) else { return }
        dispatch(tabs, .select(next))
    }

    private func chord(_ chord: TreeChord?) {
        guard chord == .focusField, let sidebarSlot, sidebarHasField else { return }
        router?.request(sidebarSlot + SidebarField.suffix)
    }
}

private struct StickyHeaders<Header: View>: View {
    let rows: [TreeRow]
    let limit: Int
    @ViewBuilder let header: (String) -> Header

    @State private var offset: CGFloat = 0
    @Environment(\.theme) private var theme
    @Environment(\.sidebarSlot) private var sidebarSlot

    var body: some View {
        Color.clear
            .onGeometryChange(for: CGFloat.self) { max(0, -$0.frame(in: .scrollView).minY) } action: { offset = $0 }
            .overlay(alignment: .top) {
                let ids = TreeCursor.sticky(rows, top: Int(offset / theme.rowHeight), limit: limit)
                if !ids.isEmpty {
                    VStack(spacing: 0) {
                        ForEach(ids.indices, id: \.self) { header(ids[$0]) }
                    }
                    .background(sidebarSlot == nil ? theme.background : theme.surface1, ignoresSafeAreaEdges: [])
                    .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
                    .offset(y: offset)
                }
            }
    }
}

struct Disclosure: View {
    let hasChildren: Bool
    let expanded: Bool
    let toggle: (Bool) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        if hasChildren {
            Button { toggle(expanded) } label: {
                DisclosureChevron(expanded: expanded, font: .system(size: 8, weight: .semibold))
                    .foregroundStyle(theme.muted)
                    .frame(width: theme.indent, height: theme.rowHeight)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(expanded ? "Collapse" : "Expand")
        } else {
            Color.clear.frame(width: theme.indent)
        }
    }
}

struct ItemRow<Leading: View>: View {
    let item: SDUIItem
    let selected: Bool
    var active = true
    var cursor = false
    var muted = false
    var faint = false
    var tone: String?
    var depth = 0
    let action: () -> Void
    @ViewBuilder let leading: () -> Leading

    @State private var hovering = false
    @State private var detailWidth = CGFloat.infinity
    @State private var detailIdeal = CGFloat.zero
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.m) {
            leading()
            if let icon = item.icon, !icon.isEmpty {
                let symbol = SymbolName.resolve(icon)
                if tone == "warning", symbol.hasSuffix(".fill") {
                    Image(systemName: symbol)
                        .symbolRenderingMode(.palette)
                        .foregroundStyle(theme.onWarning, theme.warningFill)
                        .frame(width: 14)
                } else {
                    Image(systemName: symbol)
                        .foregroundStyle(theme.tone(tone) ?? theme.muted)
                        .frame(width: 14)
                }
            }
            Text(item.label)
                .foregroundStyle(faint ? theme.faint : muted ? theme.muted : theme.foreground)
                .lineLimit(1)
                .truncationMode(Text.TruncationMode(item.truncate))
                .help(item.label)
                .layoutPriority(1)
            Spacer(minLength: theme.gutter)
            if let detail = item.detail {
                Text(detail)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.tone(tone) ?? theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(detail)
                    .frame(minWidth: detailIdeal <= keptDetail ? detailIdeal : nil)
                    .opacity(detailWidth + 0.5 < min(detailIdeal, keptDetail) ? 0 : 1)
                    .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { detailWidth = $0 }
                    .background {
                        Text(detail)
                            .font(theme.uiSmall)
                            .fixedSize()
                            .hidden()
                            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { detailIdeal = $0 }
                    }
            }
            if let badge = item.badge {
                BadgeLabel(text: badge, tone: theme.accentInk)
                    .fixedSize()
            }
        }
        .font(theme.ui)
        .padding(.leading, theme.gutter + CGFloat(depth) * theme.indent)
        .padding(.trailing, theme.gutter)
        .frame(height: theme.rowHeight)
        .background(alignment: .leading) { guides }
        .background(background, ignoresSafeAreaEdges: [])
        .overlay {
            if cursor && !selected { Rectangle().strokeBorder(theme.accent, lineWidth: theme.focusRing) }
        }
        .overlay(alignment: .leading) {
            if selected && active { theme.accent.frame(width: 2) }
        }
        .contentShape(Rectangle())
        .onTapGesture(perform: action)
        .onHover { hovering = $0 }
    }

    private var keptDetail: CGFloat { theme.uiSize * 6 }

    private var guides: some View {
        ForEach(0..<depth, id: \.self) { level in
            theme.border
                .frame(width: 1)
                .offset(x: theme.gutter + CGFloat(level) * theme.indent + theme.indent / 2)
        }
    }

    private var background: Color {
        if selected { return active ? theme.selected : theme.selectedIdle }
        return hovering ? theme.hover : .clear
    }
}

extension View {
    func itemMenu(_ entries: [SDUIMenuEntry], choose: @escaping (SDUIMenuEntry) -> Void) -> some View {
        modifier(ItemMenu(entries: entries, choose: choose))
    }
}

private struct ItemMenu: ViewModifier {
    let entries: [SDUIMenuEntry]
    let choose: (SDUIMenuEntry) -> Void

    func body(content: Content) -> some View {
        if entries.isEmpty {
            content
        } else {
            content.contextMenu {
                ForEach(Array(entries.enumerated()), id: \.element.id) { index, entry in
                    if entry.confirm != nil, index > 0 { Divider() }
                    Button(entry.label, role: entry.confirm != nil ? .destructive : nil) { choose(entry) }
                }
            }
        }
    }
}

extension ItemRow where Leading == EmptyView {
    init(item: SDUIItem, selected: Bool, active: Bool, tone: String? = nil, action: @escaping () -> Void) {
        self.init(item: item, selected: selected, active: active, tone: tone, action: action) { EmptyView() }
    }
}
