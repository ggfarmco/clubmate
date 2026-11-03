import SwiftUI

import CompanionKit

struct TabsNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var selected = ""
    @Environment(\.sduiInset) private var inset

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(node.items()) { item in
                    Button {
                        selected = item.id
                        context.run { await $0.fire(node, .select(item.id)) }
                    } label: {
                        chip(item, on: item.id == selected)
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(item.id == selected ? .isSelected : [])
                }
            }
            .padding(.horizontal, inset)
        }
        .onChange(of: node.string("selected"), initial: true) { _, value in
            selected = value ?? ""
        }
    }

    private func chip(_ item: SDUIItem, on: Bool) -> some View {
        let tone = SDUITone.fill(item.tone)
        let ink = on ? Color.onAccent : SDUITone.ink(item.tone) ?? .primary
        return HStack(spacing: 6) {
            Text(item.label)
                .lineLimit(1)
            if let badge = item.badge, !badge.isEmpty {
                Text(badge)
                    .monospacedDigit()
                    .opacity(0.8)
            }
        }
        .font(.subheadline.weight(.semibold))
        .foregroundStyle(ink)
        .padding(.horizontal, 14)
        .frame(minHeight: 36)
        .background(on ? Color.accentColor : tone?.opacity(0.15) ?? Color.surface2, in: .capsule)
        .frame(minHeight: 44)
        .contentShape(.rect)
    }
}

struct RowSection: Identifiable {
    let id: String
    let ids: [String]
}

struct RowGroup<Header: View, Row: View>: View {
    let sections: [RowSection]
    let selected: String?
    @ViewBuilder let header: (String) -> Header
    @ViewBuilder let row: (String) -> Row
    @Environment(\.sduiInScroll) private var inScroll
    @Environment(\.sduiInset) private var inset

    private func background(_ id: String) -> Color {
        id == selected ? Color.accentColor.opacity(0.15) : Color.surface1
    }

    var body: some View {
        if inScroll {
            VStack(alignment: .leading, spacing: 8) {
                ForEach(sections) { section in
                    header(section.id)
                        .padding(.horizontal, inset + 16)
                    if !section.ids.isEmpty {
                        LazyVStack(alignment: .leading, spacing: 0) {
                            ForEach(Array(section.ids.enumerated()), id: \.element) { index, id in
                                if index > 0 { Divider().padding(.leading, 16) }
                                row(id)
                                    .padding(.horizontal, 16)
                                    .background(background(id))
                            }
                        }
                        .background(Color.surface1)
                        .clipShape(.rect(cornerRadius: 22))
                        .padding(.horizontal, inset)
                    }
                }
            }
        } else {
            List {
                ForEach(sections) { section in
                    Section {
                        ForEach(section.ids, id: \.self) { id in
                            row(id)
                                .listRowBackground(id == selected ? Color.accentColor.opacity(0.15) : nil)
                        }
                    } header: {
                        header(section.id)
                    }
                }
            }
            .listStyle(.insetGrouped)
            .environment(\.sduiInScroll, true)
        }
    }
}

extension RowGroup where Header == EmptyView {
    init(ids: [String], selected: String?, @ViewBuilder row: @escaping (String) -> Row) {
        self.init(sections: [RowSection(id: "", ids: ids)], selected: selected, header: { _ in EmptyView() }, row: row)
    }
}

struct ItemLabel: View {
    let item: SDUIItem
    var detailTone: String?
    var muted = false
    var faint = false
    var opens = false

    var body: some View {
        HStack(spacing: 12) {
            if let icon = item.icon, !icon.isEmpty {
                Image(systemName: icon)
                    .foregroundStyle(SDUITone.ink(item.tone) ?? .secondary)
                    .frame(width: 28)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(item.label)
                    .lineLimit(2)
                    .truncationMode(item.truncate.mode)
                    .foregroundStyle(faint ? .tertiary : muted ? .secondary : .primary)
                if let detail = item.detail, !detail.isEmpty {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(SDUITone.ink(detailTone) ?? .secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            if let badge = item.badge, !badge.isEmpty {
                BadgeView(text: badge, tone: nil)
            }
            if opens {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .frame(minHeight: 52)
        .contentShape(.rect)
    }
}

struct MenuChoice: Identifiable {
    let entry: SDUIMenuEntry
    let item: String

    var id: String { entry.action + "\u{0}" + item }
}

private struct ItemMenu: ViewModifier {
    let entries: [SDUIMenuEntry]
    let choose: (SDUIMenuEntry) -> Void

    func body(content: Content) -> some View {
        if entries.isEmpty {
            content
        } else {
            content
                .contextMenu {
                    ForEach(entries) { entry in
                        Button(entry.label, role: entry.confirm == nil ? nil : .destructive) { choose(entry) }
                    }
                }
                .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                    ForEach(entries.filter { $0.confirm == nil }.prefix(2)) { entry in
                        Button(entry.label) { choose(entry) }
                            .tint(Color.gray)
                    }
                }
        }
    }
}

private struct MenuConfirm: ViewModifier {
    @Binding var choice: MenuChoice?
    let node: SDUINode
    let context: SDUIContext

    func body(content: Content) -> some View {
        content.alert(choice?.entry.confirm ?? "", isPresented: Binding(get: { choice != nil }, set: { if !$0 { choice = nil } }), presenting: choice) { choice in
            Button(choice.entry.label, role: .destructive) { context.run { await $0.fire(node, .named(choice.entry.action, .string(choice.item))) } }
            Button("Cancel", role: .cancel) {}
        }
    }
}

extension View {
    func itemMenu(_ entries: [SDUIMenuEntry], choose: @escaping (SDUIMenuEntry) -> Void) -> some View {
        modifier(ItemMenu(entries: entries, choose: choose))
    }

    func menuConfirm(_ choice: Binding<MenuChoice?>, node: SDUINode, context: SDUIContext) -> some View {
        modifier(MenuConfirm(choice: choice, node: node, context: context))
    }
}

extension SDUIContext {
    func choose(_ entry: SDUIMenuEntry, item: String, in node: SDUINode, confirm: inout MenuChoice?) {
        if entry.confirm != nil {
            confirm = MenuChoice(entry: entry, item: item)
        } else {
            run { await $0.fire(node, .named(entry.action, .string(item))) }
        }
    }
}

extension SDUITruncate {
    var mode: Text.TruncationMode {
        switch self {
        case .tail: .tail
        case .middle: .middle
        case .head: .head
        }
    }
}

struct ListNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var selected: String?
    @State private var choice: MenuChoice?

    var body: some View {
        let items = node.items()
        let byID = Dictionary(items.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        RowGroup(ids: items.map(\.id), selected: selected) { id in
            if let item = byID[id] {
                Button {
                    selected = id
                    context.run { await $0.select(node, id) }
                } label: {
                    ItemLabel(item: item, opens: context.mainSlot != nil)
                }
                .buttonStyle(.plain)
                .itemMenu(item.menu) { context.choose($0, item: id, in: node, confirm: &choice) }
            }
        }
        .menuConfirm($choice, node: node, context: context)
        .onChange(of: node.string("selected"), initial: true) { _, value in
            selected = value
        }
    }
}

struct TreeNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var selected: String?
    @State private var choice: MenuChoice?

    private struct Row {
        let item: SDUITreeItem
        let depth: Int
    }

    private static func rows(_ items: [SDUITreeItem], depth: Int = 0) -> [Row] {
        items.flatMap { [Row(item: $0, depth: depth)] + ($0.expanded ? rows($0.children, depth: depth + 1) : []) }
    }

    var body: some View {
        let rows = Self.rows(node.treeItems())
        let gutter = rows.contains { $0.item.hasChildren }
        let byID = Dictionary(rows.map { ($0.item.id, $0) }, uniquingKeysWith: { first, _ in first })
        RowGroup(ids: rows.map(\.item.id), selected: selected) { id in
            if let row = byID[id] {
                HStack(spacing: 0) {
                    Color.clear.frame(width: CGFloat(row.depth) * 16, height: 1)
                    disclosure(row.item, gutter: gutter)
                    Button {
                        selected = id
                        context.run { await $0.select(node, id) }
                    } label: {
                        ItemLabel(item: row.item.item, detailTone: row.item.detailTone, muted: row.item.muted, faint: row.item.faint, opens: context.mainSlot != nil && !row.item.hasChildren)
                    }
                    .buttonStyle(.plain)
                }
                .itemMenu(row.item.item.menu) { context.choose($0, item: id, in: node, confirm: &choice) }
            }
        }
        .menuConfirm($choice, node: node, context: context)
        .onChange(of: node.string("selected"), initial: true) { _, value in
            selected = value
        }
    }

    @ViewBuilder
    private func disclosure(_ item: SDUITreeItem, gutter: Bool) -> some View {
        if !item.hasChildren {
            if gutter { Color.clear.frame(width: 32, height: 1) }
        } else {
            Button {
                context.run { await $0.fire(node, item.expanded ? .collapse(item.id) : .expand(item.id)) }
            } label: {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .rotationEffect(.degrees(item.expanded ? 90 : 0))
                    .frame(width: 32, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(item.expanded ? "Collapse \(item.item.label)" : "Expand \(item.item.label)")
        }
    }
}

struct TableNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var selected: String?
    @Environment(\.sduiInset) private var inset

    var body: some View {
        let columns = node.columns()
        let rows = node.tableRows()
        let byID = Dictionary(rows.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        VStack(alignment: .leading, spacing: 8) {
            if columns.count > 1 {
                sortMenu(columns)
                    .padding(.horizontal, inset + 4)
            }
            RowGroup(sections: Self.sections(rows), selected: selected) { id in
                if let row = byID[id] { groupRow(row) }
            } row: { id in
                if let row = byID[id] { dataRow(row, columns: columns) }
            }
        }
        .onChange(of: node.string("selected"), initial: true) { _, value in
            selected = value
        }
    }

    private static func sections(_ rows: [SDUITableRow]) -> [RowSection] {
        var sections: [RowSection] = []
        var current = ""
        var ids: [String] = []
        for row in rows {
            if row.group {
                if !current.isEmpty || !ids.isEmpty { sections.append(RowSection(id: current, ids: ids)) }
                current = row.id
                ids = []
            } else {
                ids.append(row.id)
            }
        }
        sections.append(RowSection(id: current, ids: ids))
        return sections
    }

    private func sortMenu(_ columns: [SDUIColumn]) -> some View {
        let sort = node.tableSort()
        let current = columns.first { $0.id == sort?.column }
        return Menu {
            ForEach(columns) { column in
                Button {
                    let next = SDUISort.next(clicking: column, current: sort)
                    context.run { await $0.fire(node, .sort(column: next.column, desc: next.desc)) }
                } label: {
                    if column.id == sort?.column {
                        Label(column.title, systemImage: sort?.desc == true ? "arrow.down" : "arrow.up")
                    } else {
                        Text(column.title)
                    }
                }
            }
        } label: {
            Label(current.map { "Sorted by \($0.title)" } ?? "Sort", systemImage: "arrow.up.arrow.down")
                .font(.footnote.weight(.semibold))
                .frame(minHeight: 32)
        }
        .tint(.secondary)
    }

    private func groupRow(_ row: SDUITableRow) -> some View {
        Button {
            context.run { await $0.fire(node, row.expanded ? .collapse(row.id) : .expand(row.id)) }
        } label: {
            HStack(spacing: 6) {
                Image(systemName: "chevron.right")
                    .font(.caption.weight(.semibold))
                    .rotationEffect(.degrees(row.expanded ? 90 : 0))
                Text(row.cells.first?.text ?? "")
                    .lineLimit(1)
                Spacer(minLength: 0)
            }
            .font(.footnote.weight(.semibold))
            .foregroundStyle(.secondary)
            .textCase(nil)
            .padding(.leading, CGFloat(row.depth) * 12)
            .frame(minHeight: 36)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }

    private func dataRow(_ row: SDUITableRow, columns: [SDUIColumn]) -> some View {
        let first = row.cells.first
        return HStack(alignment: .top, spacing: 6) {
            if row.hasChildren {
                Button {
                    context.run { await $0.fire(node, row.expanded ? .collapse(row.id) : .expand(row.id)) }
                } label: {
                    Image(systemName: "chevron.right")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .rotationEffect(.degrees(row.expanded ? 90 : 0))
                        .frame(width: 28, height: 44)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
            Button {
                selected = row.id
                context.run { await $0.select(node, row.id) }
            } label: {
                HStack(spacing: 8) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(first?.text ?? "")
                            .lineLimit(2)
                            .truncationMode(columns.first?.truncate.mode ?? .tail)
                            .foregroundStyle(SDUITone.ink(first?.bar == nil ? first?.tone : nil) ?? .primary)
                        let cells = Array(zip(columns, row.cells).dropFirst().filter { !$0.1.text.isEmpty || $0.1.bar != nil || $0.1.spark != nil }.enumerated())
                        if !cells.isEmpty {
                            ViewThatFits(in: .horizontal) {
                                HStack(spacing: 10) {
                                    ForEach(cells, id: \.offset) { _, pair in TableCellView(column: pair.0, cell: pair.1) }
                                }
                                VStack(alignment: .leading, spacing: 2) {
                                    ForEach(cells, id: \.offset) { _, pair in TableCellView(column: pair.0, cell: pair.1) }
                                }
                            }
                        }
                    }
                    Spacer(minLength: 0)
                    if context.mainSlot != nil {
                        Image(systemName: "chevron.right")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.tertiary)
                    }
                }
                .padding(.vertical, 8)
                .frame(minHeight: 52)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if let link = row.cells.lazy.compactMap(\.link).first {
                Button(link.label) {
                    context.run { await $0.fire(node, .named(link.action, .string(row.id))) }
                }
                .font(.footnote.weight(.semibold))
                .buttonStyle(.borderless)
                .frame(minHeight: 44)
                .accessibilityIdentifier("sdui.table.link.\(row.id)")
            }
        }
        .padding(.leading, CGFloat(row.depth) * 12)
        .opacity(row.muted ? 0.6 : 1)
    }
}

private struct TableCellView: View {
    let column: SDUIColumn
    let cell: SDUICell

    var body: some View {
        HStack(spacing: 4) {
            Text(column.title)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
                .fixedSize()
            if let bar = cell.bar {
                Capsule()
                    .fill(.quaternary)
                    .frame(width: 36, height: 6)
                    .overlay(alignment: .leading) {
                        Capsule()
                            .fill(SDUITone.fill(cell.tone) ?? .accentColor)
                            .frame(width: max(36 * bar, 2), height: 6)
                    }
            }
            if let spark = cell.spark {
                SparkChart(spark: spark, tint: SDUITone.fill(cell.tone) ?? .accentColor)
                    .frame(width: 60, height: 16)
            }
            if !cell.text.isEmpty {
                Text(cell.text)
                    .monospacedDigit()
                    .lineLimit(1)
                    .truncationMode(column.truncate.mode)
                    .foregroundStyle(cell.bar == nil ? SDUITone.ink(cell.tone) ?? .secondary : .secondary)
            }
        }
        .font(.footnote)
    }
}
