import AppKit
import SwiftUI

import ClubmateKit

struct TableNodeView: View {
    let node: SDUINode

    @State private var selected: String?
    @State private var sort: SDUISort?
    @State private var width: CGFloat = 0
    @FocusState private var focused: Bool
    @Environment(\.isFocused) private var paneFocused
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.scrollProxy) private var scroll
    @Environment(\.theme) private var theme

    var body: some View {
        let all = node.columns()
        let columns = SDUIColumn.visible(all, width: Double(width - 2 * theme.gutter), gutter: Double(theme.gutter)).map { (index: $0, column: all[$0]) }
        let rows = node.tableRows()
        let tree = rows.contains { $0.hasChildren || $0.depth > 0 }
        LazyVStack(alignment: .leading, spacing: 0, pinnedViews: [.sectionHeaders]) {
            Section {
                ForEach(rows) { row in
                    if row.group {
                        TableGroupRow(row: row) { expanded in
                            dispatch(node, expanded ? .collapse(row.id) : .expand(row.id))
                        }
                        .id(row.id)
                    } else {
                        tableRow(row, columns: columns, tree: tree)
                    }
                }
            } header: {
                TableHeader(columns: columns.map(\.column), sort: sort) { column in
                    let next = SDUISort.next(clicking: column, current: sort)
                    sort = next
                    dispatch(node, .sort(column: next.column, desc: next.desc))
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .keyMonitor(active: focused, handle: handle)
        .reportsFocus(.table, focused)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .onChange(of: paneFocused) { _, value in
            if value { focused = true }
        }
        .onChange(of: node.tableSort(), initial: true) { _, value in
            sort = value
        }
        .onChange(of: node.string("selected"), initial: true) { _, value in
            guard value != selected else { return }
            selected = value
            if let value { scroll?.scrollTo(value, anchor: .center) }
        }
    }

    private func tableRow(_ row: SDUITableRow, columns: [(index: Int, column: SDUIColumn)], tree: Bool) -> some View {
        TableRowView(row: row, columns: columns, tree: tree, selected: row.id == selected, active: focused) {
            focused = true
            select(row.id)
        } toggle: { expanded in
            dispatch(node, expanded ? .collapse(row.id) : .expand(row.id))
        }
        .id(row.id)
    }

    private func select(_ id: String) {
        selected = id
        dispatch(node, .select(id))
    }

    private func handle(_ event: NSEvent) -> Bool {
        let plain = event.modifierFlags.intersection([.command, .control, .option, .shift]).isEmpty
        guard let action = TableKeys.action(keyCode: event.keyCode, plain: plain) else { return false }
        let all = node.tableRows()
        let rows = TableKeys.rows(all)
        switch TreeCursor.resolve(action, cursor: selected, in: rows) {
        case .cursor(let id):
            let up = (rows.firstIndex { $0.id == id } ?? 0) < (rows.firstIndex { $0.id == selected } ?? 0)
            let index = all.firstIndex { $0.id == id } ?? 0
            scroll?.scrollTo(up ? all[max(index - 1, 0)].id : id)
            select(id)
        case .expand(let id): dispatch(node, .expand(id))
        case .collapse(let id): dispatch(node, .collapse(id))
        case .select, .focusMain, nil: break
        }
        return true
    }
}

private struct TableGroupRow: View {
    let row: SDUITableRow
    let toggle: (Bool) -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        let label = row.cells.first?.text ?? ""
        HStack(spacing: 0) {
            Disclosure(hasChildren: row.hasChildren, expanded: row.expanded, toggle: toggle)
                .padding(.leading, CGFloat(row.depth) * theme.indent)
            Text(label)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.tail)
                .help(label)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, theme.gutter)
        .frame(height: theme.rowHeight)
        .background(hovering ? theme.hover : .clear, ignoresSafeAreaEdges: [])
        .contentShape(Rectangle())
        .onTapGesture { if row.hasChildren { toggle(row.expanded) } }
        .onHover { hovering = $0 }
    }
}

private struct TableHeader: View {
    let columns: [SDUIColumn]
    let sort: SDUISort?
    let tap: (SDUIColumn) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.gutter) {
            ForEach(columns) { column in
                let active = sort?.column == column.id
                Button { tap(column) } label: {
                    HStack(spacing: theme.spacing.xs) {
                        Text(column.title)
                            .lineLimit(1)
                        if active, let sort {
                            Image(systemName: sort.desc ? "chevron.down" : "chevron.up")
                                .font(.system(size: 8, weight: .semibold))
                        }
                    }
                    .foregroundStyle(active ? theme.foreground : theme.muted)
                    .tableColumn(column)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .font(theme.uiSmall.monospacedDigit())
        .padding(.horizontal, theme.gutter)
        .frame(height: theme.rowHeight)
        .background(theme.background, ignoresSafeAreaEdges: [])
        .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
    }
}

private struct TableRowView: View {
    let row: SDUITableRow
    let columns: [(index: Int, column: SDUIColumn)]
    let tree: Bool
    let selected: Bool
    let active: Bool
    let select: () -> Void
    let toggle: (Bool) -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.gutter) {
            ForEach(Array(columns.enumerated()), id: \.element.column.id) { position, entry in
                let cell = entry.index < row.cells.count ? row.cells[entry.index] : SDUICell(text: "", bar: nil, tone: nil, spark: nil)
                HStack(spacing: 0) {
                    if position == 0 && tree {
                        Disclosure(hasChildren: row.hasChildren, expanded: row.expanded, toggle: toggle)
                            .padding(.leading, CGFloat(row.depth) * theme.indent)
                    }
                    TableCellView(cell: cell, truncate: entry.column.truncate, muted: row.muted)
                }
                .tableColumn(entry.column)
                .background(alignment: .leading) {
                    if let bar = cell.bar {
                        BarShape(fraction: bar)
                            .fill(cell.toneColor(theme) ?? theme.accent)
                            .opacity(0.3)
                            .padding(.vertical, theme.spacing.xs)
                    }
                }
            }
        }
        .font(theme.ui.monospacedDigit())
        .padding(.horizontal, theme.gutter)
        .frame(height: theme.rowHeight)
        .background(background, ignoresSafeAreaEdges: [])
        .overlay(alignment: .leading) {
            if selected && active { theme.accent.frame(width: 2) }
        }
        .contentShape(Rectangle())
        .onTapGesture(perform: select)
        .onHover { hovering = $0 }
    }

    private var background: Color {
        if selected { return active ? theme.selected : theme.selectedIdle }
        return hovering ? theme.hover : .clear
    }
}

private struct TableCellView: View {
    let cell: SDUICell
    let truncate: SDUITruncate
    let muted: Bool

    @Environment(\.theme) private var theme

    var body: some View {
        let tone = cell.toneColor(theme)
        HStack(spacing: theme.spacing.s) {
            if let spark = cell.spark {
                SparklineView(spark: spark, color: tone)
            }
            if !cell.text.isEmpty {
                Text(cell.text)
                    .lineLimit(1)
                    .truncationMode(Text.TruncationMode(truncate))
                    .foregroundStyle(textColor(tone))
                    .help(cell.text)
            }
        }
    }

    private func textColor(_ tone: Color?) -> Color {
        if let tone, cell.bar == nil { return tone }
        return muted ? theme.muted : theme.foreground
    }
}

private struct BarShape: Shape {
    let fraction: Double

    func path(in rect: CGRect) -> Path {
        Capsule().path(in: CGRect(x: rect.minX, y: rect.minY, width: rect.width * fraction, height: rect.height))
    }
}

struct SparklineNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        if let spark = node.spark() {
            SparklineView(spark: spark, height: node.sparklineHeight().map { CGFloat($0) })
                .padding(.horizontal, theme.gutter)
        }
    }
}

struct SparklineView: View {
    let spark: SDUISpark
    var color: Color?
    var height: CGFloat?

    @Environment(\.theme) private var theme

    var body: some View {
        let line = SparkShape(values: spark.values, max: spark.max)
            .stroke(color ?? theme.accent, style: StrokeStyle(lineWidth: 1, lineCap: .round, lineJoin: .round))
        if let height {
            line.frame(maxWidth: .infinity).frame(height: height)
        } else {
            line.frame(width: 60, height: 16)
        }
    }
}

private struct SparkShape: Shape {
    let values: [Double]
    let max: Double

    func path(in rect: CGRect) -> Path {
        var path = Path()
        guard values.count > 1, max > 0 else { return path }
        let area = rect.insetBy(dx: 0, dy: 0.5)
        let step = area.width / CGFloat(values.count - 1)
        for (index, value) in values.enumerated() {
            let level = Swift.min(Swift.max(value, 0), max) / max
            let point = CGPoint(x: area.minX + CGFloat(index) * step, y: area.maxY - CGFloat(level) * area.height)
            if index == 0 {
                path.move(to: point)
            } else {
                path.addLine(to: point)
            }
        }
        return path
    }
}

private extension SDUICell {
    func toneColor(_ theme: Theme) -> Color? {
        switch tone {
        case "ok": theme.success
        case "warn": theme.warning
        case "danger": theme.danger
        default: nil
        }
    }
}

private extension View {
    func tableColumn(_ column: SDUIColumn) -> some View {
        let width = column.width.map { CGFloat($0) }
        return frame(minWidth: width, maxWidth: width ?? .infinity, alignment: column.align == .right ? .trailing : .leading)
    }
}
