import AppKit
import SwiftUI

import ClubmateKit

struct TabsNodeView: View {
    let node: SDUINode

    private static let wrapCount = 3

    @State private var selected = ""
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        let items = node.items()
        Group {
            if items.count > Self.wrapCount {
                FlowLayout(spacing: theme.spacing.s) { chipButtons(items, compact: false) }
            } else {
                ViewThatFits(in: .horizontal) {
                    chips(items, compact: false)
                    chips(items, compact: true)
                    ScrollViewReader { proxy in
                        ScrollView(.horizontal, showsIndicators: false) {
                            chips(items, compact: true)
                        }
                        .clipped()
                        .onChange(of: selected, initial: true) { _, id in
                            proxy.scrollTo(id)
                        }
                    }
                }
            }
        }
        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, theme.spacing.s)
        .onChange(of: node, initial: true) { _, value in
            selected = value.string("selected") ?? ""
        }
    }

    private var segmented: Bool { node.bool("segmented") == true }

    @ViewBuilder
    private func chips(_ items: [SDUIItem], compact: Bool) -> some View {
        if segmented {
            HStack(spacing: Self.segmentGap) {
                chipButtons(items, compact: compact)
            }
            .padding(Self.segmentGap)
            .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.hover))
            .fixedSize()
        } else {
            HStack(spacing: theme.spacing.s) {
                chipButtons(items, compact: compact)
            }
            .fixedSize()
        }
    }

    private static let segmentGap: CGFloat = 2

    private func chipButtons(_ items: [SDUIItem], compact: Bool) -> some View {
        ForEach(items) { item in
            TabButton(item: item, compact: compact, selected: item.id == selected, segmented: segmented) {
                selected = item.id
                dispatch(node, .select(item.id))
            }
            .id(item.id)
            .keyMonitor(active: item.shortcut != nil) { event in
                guard item.shortcut.flatMap(KeyCombo.init)?.fires(on: event) == true else { return false }
                selected = item.id
                dispatch(node, .select(item.id))
                return true
            }
        }
    }
}

struct TabButton: View {
    let item: SDUIItem
    let compact: Bool
    let selected: Bool
    var segmented = false
    let action: () -> Void

    @State private var hovering = false
    @State private var keyboardFocus = false
    @FocusState private var focused: Bool
    @Environment(\.theme) private var theme

    var body: some View {
        let ink = theme.tone(item.tone)
        let icon = compact || segmented ? item.icon.flatMap { $0.isEmpty ? nil : $0 } : nil
        let shape = segmented ? AnyShape(RoundedRectangle(cornerRadius: theme.radius.s)) : AnyShape(Capsule())
        Button(action: action) {
            HStack(spacing: theme.spacing.s) {
                if let icon {
                    Image(systemName: SymbolName.resolve(icon))
                }
                if icon == nil || segmented {
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
            .background(shape.fill(fill))
            .overlay {
                if keyboardFocus && focused {
                    shape
                        .stroke(theme.accent, lineWidth: theme.focusRing)
                        .padding(-ButtonNodeView.ringGap)
                }
            }
            .contentShape(shape)
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
        .testRow(item.id, label: item.label, selected: selected, focused: keyboardFocus && focused, hovered: hovering)
    }

    private var fill: Color {
        if let tone = theme.toneFill(item.tone) { return selected ? theme.toneBorder(tone) : theme.toneBg(tone) }
        if segmented { return selected ? theme.pressed : hovering ? theme.hover : .clear }
        return selected || hovering ? theme.pressed : theme.hover
    }
}

struct ListNodeView: View {
    let node: SDUINode

    @State private var selected: String?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.scrollProxy) private var scroll
    @Environment(\.focusRouter) private var router
    @Environment(\.sidebarSide) private var side
    @Environment(\.sidebarSlot) private var sidebarSlot

    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(node.items()) { item in
                ItemRow(item: item, selected: item.id == selected, active: focused, tone: item.tone) {
                    focused = true
                    select(item.id)
                }
                .itemMenu(item.menu) { dispatch.choose($0, item: item.id, in: node) }
                .id(item.id)
            }
        }
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .keyMonitor(active: focused, handle: handle)
        .reportsFocus(.list, focused, slot: sidebarSlot)
        .onChange(of: node.string("selected"), initial: true) { _, value in
            guard value != selected else { return }
            selected = value
            if let value { scroll?.scrollTo(value, anchor: .center) }
        }
        .onChange(of: router?.pending, initial: true) {
            guard let sidebarSlot, router?.take(sidebarSlot) == true else { return }
            focused = true
        }
    }

    private func select(_ id: String) {
        selected = id
        dispatch(node, .select(id))
    }

    private func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        switch TreeKeys.action(keyCode: event.keyCode, control: flags.contains(.control), command: flags.contains(.command), side: side, shift: flags.contains(.shift)) {
        case .move(let delta):
            let rows = node.items().map { TreeRow(id: $0.id, depth: 0, parent: nil, hasChildren: false, expanded: false) }
            guard let id = TreeCursor.move(selected, by: delta, in: rows) else { return true }
            if id != selected { select(id) }
            scroll?.scrollTo(id)
        case .open, .focusMain, .escape:
            router?.requestMain(from: sidebarSlot ?? dispatch.slot)
        default:
            return false
        }
        return true
    }
}

struct TreeNodeView: View {
    let node: SDUINode

    @State private var selected: String?
    @State private var cursor: String?
    @State private var activated: String?
    @State private var leader = false
    @State private var ownScroll: ScrollViewProxy?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.scrollProxy) private var scroll
    @Environment(\.focusRouter) private var router
    @Environment(\.sidebarSide) private var side
    @Environment(\.sidebarSlot) private var sidebarSlot
    @Environment(\.sidebarHasField) private var sidebarHasField
    @Environment(\.sidebarTabs) private var sidebarTabs
    @Environment(\.treeHold) private var treeHold

    private static let stickyLimit = 4

    private struct Row: Identifiable {
        let item: SDUITreeItem
        let depth: Int
        let section: Bool
        let first: Bool

        var id: String { item.id }
    }

    private struct Columns {
        let chevron: Bool
        let icon: Bool
    }

    private var rows: [Row] {
        var rows: [Row] = []
        func visit(_ items: [SDUITreeItem], _ depth: Int) {
            for item in items {
                let section = depth == 0 && item.heading
                rows.append(Row(item: item, depth: depth, section: section, first: rows.isEmpty))
                visit(item.shownChildren, section ? depth : depth + 1)
            }
        }
        visit(node.treeItems(), 0)
        return rows
    }

    private var dock: [SDUITreeDock] { node.treeDock() }

    private var scroller: ScrollViewProxy? { scroll ?? ownScroll }

    var body: some View {
        if dock.isEmpty {
            keyed(stack)
        } else {
            keyed(VStack(spacing: 0) {
                ScrollViewReader { proxy in
                    ScrollView {
                        stack
                            .frame(maxWidth: .infinity, alignment: .topLeading)
                            .onAppear { ownScroll = proxy }
                    }
                    .clipped()
                    .environment(\.scrollProxy, proxy)
                }
                .overlay(alignment: .bottom) { TreeDockFade() }
                TreeDockView(dock: dock, selected: selected, cursor: focused ? cursor : nil, active: focused) { id in
                    cursor = id
                    focused = true
                    toggleDock(id)
                } add: { section in
                    if let add = section.add { dispatch(node, .named(add.action, .string(section.id))) }
                } open: { id in
                    cursor = id
                    focused = true
                    select(id)
                }
            })
        }
    }

    private func toggleDock(_ id: String) {
        guard let section = dock.first(where: { $0.id == id }) else { return }
        dispatch(node, section.expanded ? .collapse(id) : .expand(id))
    }

    private var stack: some View {
        let rows = rows
        let columns = Columns(
            chevron: rows.contains { !$0.section && $0.depth == 0 && $0.item.hasChildren },
            icon: rows.contains { !$0.section && $0.item.item.icon?.isEmpty == false }
        )
        return LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(rows) { row in
                itemRow(row, columns: columns, selected: row.item.target == selected, cursor: focused && row.id == cursor) {
                    cursor = row.id
                    focused = true
                    if case .fold(let expand) = row.item.tap {
                        dispatch(node, expand ? .expand(row.id) : .collapse(row.id))
                    } else {
                        select(row.id)
                    }
                }
                .id(row.id)
            }
        }
        .disclosureAnimation(Set(rows.filter(\.item.expanded).map(\.id)))
        .overlay(alignment: .top) {
            if scroller != nil {
                let tree = TreeCursor.rows(node.treeItems())
                StickyHeaders(rows: tree, limit: Self.stickyLimit) { id in
                    if let found = rows.first(where: { $0.id == id }) {
                        let row = Row(item: found.item, depth: found.depth, section: found.section, first: true)
                        itemRow(row, columns: columns, selected: false, cursor: false) {
                            if let target = TreeCursor.stickyTarget(id, in: tree, limit: Self.stickyLimit) {
                                scroller?.scrollTo(target, anchor: .top)
                            }
                        }
                    }
                }
            }
        }
    }

    private func keyed(_ content: some View) -> some View {
        content
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .keyMonitor(active: focused, handle: handle)
        .reportsFocus(.tree(field: sidebarHasField), focused, slot: sidebarSlot)
        .onChange(of: focused) { _, focused in
            if !focused { leader = false }
            if focused { treeHold?.land() }
        }
        .onChange(of: node.string("selected"), initial: true) { _, value in
            guard value != selected else { return }
            selected = value
            if let value { scroller?.scrollTo(rowID(value), anchor: .center) }
        }
        .onChange(of: router?.pending, initial: true) {
            guard let sidebarSlot, router?.take(sidebarSlot) == true else { return }
            cursor = selected.map(rowID) ?? rows.first?.id
            focused = true
        }
        .onChange(of: TreeCursor.rows(node.treeItems(), dock: dock).map(\.id)) { old, new in
            defer { activated = nil }
            guard let id = TreeCursor.settle(cursor, activated: cursor == activated, from: old, to: new) else { return }
            cursor = id
            scroller?.scrollTo(id)
        }
    }

    @ViewBuilder
    private func itemRow(_ row: Row, columns: Columns, selected: Bool, cursor: Bool, action: @escaping () -> Void) -> some View {
        if row.section {
            SectionLabel(item: row.item, spaced: !row.first, cursor: cursor) {
                self.cursor = row.id
                focused = true
                dispatch(node, row.item.expanded ? .collapse(row.id) : .expand(row.id))
            }
        } else {
            ItemRow(item: row.item.item, selected: selected, active: focused, cursor: cursor, muted: row.item.muted, faint: row.item.faint, tone: row.item.tone, detailTone: row.item.detailTone, detailFaint: row.item.detailFaint, depth: row.depth, badges: row.item.badges, changes: row.item.changes, labelIcon: row.item.labelIcon, reserveIcon: columns.icon, action: action) {
                Disclosure(hasChildren: row.item.hasChildren, expanded: row.item.expanded, reserve: columns.chevron) { expanded in
                    dispatch(node, expanded ? .collapse(row.id) : .expand(row.id))
                }
            }
            .itemMenu(row.item.item.menu) { dispatch.choose($0, item: row.item.target, in: node) }
        }
    }

    private func rowID(_ selected: String) -> String {
        let rows = rows
        if rows.contains(where: { $0.id == selected }) { return selected }
        return rows.first(where: { $0.item.target == selected })?.id ?? selected
    }

    private func select(_ id: String) {
        let item = rows.first(where: { $0.id == id })?.item ?? dock.flatMap(\.rows).first(where: { $0.id == id })
        let target = item?.target ?? id
        if item?.action != true { selected = target } else if item?.hasChildren == false { activated = id }
        dispatch(node, .select(target))
        if let item, case .open(_, unfold: true) = item.tap { dispatch(node, .expand(id)) }
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
        if plain, let key = event.charactersIgnoringModifiers, menuKey(key) { return true }
        let rows = TreeCursor.rows(node.treeItems(), dock: dock)
        guard let action = TreeKeys.action(keyCode: event.keyCode, control: flags.contains(.control), command: flags.contains(.command), side: side, shift: flags.contains(.shift), option: flags.contains(.option)) else { return false }
        switch TreeCursor.resolve(action, cursor: cursor, in: rows) {
        case .cursor(let id):
            if !dock.contains(where: { $0.id == id }), !dock.flatMap(\.rows).contains(where: { $0.id == id }), let target = TreeCursor.scrollTarget(id, from: cursor, in: rows, limit: Self.stickyLimit) { scroller?.scrollTo(target) }
            cursor = id
        case .select(let id): select(id)
        case .openPage(let id):
            select(id)
            router?.requestMain(from: sidebarSlot ?? dispatch.slot)
        case .remove(let id):
            let item = treeItem(id)
            guard let item, let entry = item.item.menu.first(where: { $0.key == Self.removeKey }) else { return false }
            dispatch.choose(entry, item: item.target, in: node)
        case .expand(let id): dispatch(node, .expand(id))
        case .collapse(let id): dispatch(node, .collapse(id))
        case .focusMain: router?.requestMain(from: sidebarSlot ?? dispatch.slot)
        case .cycleTabs(let delta): cycleTabs(by: delta)
        case nil: break
        }
        return true
    }

    private static let removeKey = "cmd+delete"

    private func treeItem(_ id: String) -> SDUITreeItem? {
        rows.first(where: { $0.id == id })?.item ?? dock.flatMap(\.rows).first(where: { $0.id == id })
    }

    private func menuKey(_ key: String) -> Bool {
        guard let id = cursor ?? selected, let row = rows.first(where: { $0.id == id }), let entry = row.item.item.menu.first(where: { $0.key == key }) else { return false }
        dispatch.choose(entry, item: row.item.target, in: node)
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

private struct SectionLabel: View {
    let item: SDUITreeItem
    let spaced: Bool
    let cursor: Bool
    let toggle: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.s) {
            Text(item.item.label)
                .lineLimit(1)
                .truncationMode(.tail)
                .help(item.item.label)
            if !item.expanded {
                Image(systemName: "chevron.right")
                    .font(.system(size: 8, weight: .semibold))
            }
            Spacer(minLength: theme.spacing.m)
            if let detail = item.item.detail {
                Text(detail)
                    .monospacedDigit()
                    .foregroundStyle(theme.tone(item.detailTone) ?? theme.muted)
                    .lineLimit(1)
                    .fixedSize()
            }
        }
        .font(theme.uiSmall)
        .foregroundStyle(hovering ? theme.text2 : theme.muted)
        .padding(.horizontal, theme.gutter)
        .frame(height: theme.rowHeight)
        .overlay {
            if cursor { Rectangle().strokeBorder(theme.accent, lineWidth: theme.focusRing) }
        }
        .padding(.top, spaced ? theme.spacing.m : 0)
        .contentShape(Rectangle())
        .onTapGesture(perform: toggle)
        .onHover { hovering = $0 }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
        .accessibilityAction(named: item.expanded ? "Collapse" : "Expand", toggle)
        .testRow(item.id, label: item.item.label, selected: false, focused: cursor, hovered: hovering)
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
    var reserve = true
    let toggle: (Bool) -> Void

    private static let cell: CGFloat = 20

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        if hasChildren {
            Button { toggle(expanded) } label: {
                DisclosureChevron(expanded: expanded, font: .system(size: 8, weight: .semibold))
                    .foregroundStyle(hovering ? theme.foreground : theme.muted)
                    .frame(width: Self.cell, height: Self.cell)
                    .background {
                        if hovering { RoundedRectangle(cornerRadius: theme.radius.s).fill(theme.hover) }
                    }
                    .overlay {
                        if hovering { RoundedRectangle(cornerRadius: theme.radius.s).strokeBorder(theme.borderStrong) }
                    }
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .onHover { hovering = $0 }
            .accessibilityLabel(expanded ? "Collapse" : "Expand")
            .frame(width: theme.indent, height: theme.rowHeight)
        } else if reserve {
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
    var detailTone: String?
    var detailFaint = false
    var depth = 0
    var badges: [SDUIHeaderBadge] = []
    var changes: SDUIChanges?
    var labelIcon: String?
    var note: ItemNote?
    var reserveIcon = false
    var reserveTrailing: CGFloat = 0
    let action: () -> Void
    @ViewBuilder let leading: () -> Leading

    @State private var hovering = false
    @State private var detailWidth = CGFloat.infinity
    @State private var detailIdeal = CGFloat.zero
    @Environment(\.theme) private var theme

    var body: some View {
        row
            .testRow(item.id, label: item.label, selected: selected, focused: cursor, hovered: hovering)
    }

    private var row: some View {
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
            } else if reserveIcon {
                Color.clear.frame(width: 14, height: 1)
            }
            if let labelIcon {
                Image(systemName: SymbolName.resolve(labelIcon))
                    .foregroundStyle(theme.muted)
                    .frame(width: 14)
            }
            RowLayout(hasDetail: item.detail != nil, lead: labelIcon == nil ? theme.gutter + theme.spacing.m * 2 : theme.spacing.l, spacing: theme.spacing.m, keptLabel: keptLabel, keptDetail: keptDetail) {
                HStack(spacing: theme.spacing.s) {
                    Text(item.label)
                        .foregroundStyle(faint ? theme.faint : muted ? theme.muted : theme.foreground)
                        .lineLimit(1)
                        .truncationMode(Text.TruncationMode(item.truncate))
                        .help(item.label)
                        .layoutPriority(1)
                    if let note {
                        ItemNoteView(note: note)
                    }
                }
                ForEach(Array(badges.enumerated()), id: \.offset) { _, badge in
                    BadgeLabel(text: badge.text, tone: theme.tone(badge.tone), outline: badge.outline)
                        .fixedSize()
                }
                if let detail = item.detail {
                    Text(detail)
                        .font(theme.uiSmall)
                        .foregroundStyle(detailInk)
                        .lineLimit(1)
                        .truncationMode(.label)
                        .help(detail)
                        .frame(minWidth: 0, maxWidth: .infinity, alignment: .trailing)
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
            }
            .frame(maxWidth: .infinity)
            if let badge = item.badge {
                BadgeLabel(text: badge, tone: theme.accentInk)
                    .fixedSize()
            }
            if let changes {
                ChangeCounts(changes: changes)
            }
        }
        .font(theme.ui)
        .padding(.leading, theme.gutter + CGFloat(depth) * theme.indent)
        .padding(.trailing, theme.gutter + reserveTrailing)
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

    private var detailInk: Color {
        if detailFaint { return theme.faint }
        if detailTone == "plain" { return theme.text2 }
        return theme.tone(detailTone ?? tone) ?? theme.muted
    }

    private var keptDetail: CGFloat { theme.uiSize * 6 }

    private var keptLabel: CGFloat { theme.uiSize * 10 }

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

struct ItemNote: Equatable {
    let text: String
    let attention: Bool
}

private struct ItemNoteView: View {
    let note: ItemNote

    @Environment(\.theme) private var theme

    var body: some View {
        if note.attention {
            BadgeLabel(text: note.text, tone: theme.warning)
                .lineLimit(1)
                .help(note.text)
        } else {
            Text("· " + note.text)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.tail)
                .help(note.text)
        }
    }
}

struct ChangeCounts: View {
    let changes: SDUIChanges

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.xs) {
            Text("+\(changes.added)").foregroundStyle(theme.success)
            Text("−\(changes.removed)").foregroundStyle(theme.danger)
        }
        .font(theme.uiSmall)
        .monospacedDigit()
        .lineLimit(1)
        .fixedSize()
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
                    switch MenuEntryKey(entry.key) {
                    case .plain(let key):
                        Button(entry.label, role: entry.confirm != nil ? .destructive : nil) { choose(entry) }
                            .keyboardShortcut(KeyEquivalent(key), modifiers: [])
                    case .commandDelete:
                        Button(entry.label, role: entry.confirm != nil ? .destructive : nil) { choose(entry) }
                            .keyboardShortcut(.delete, modifiers: .command)
                    case nil:
                        Button(entry.label, role: entry.confirm != nil ? .destructive : nil) { choose(entry) }
                    }
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
