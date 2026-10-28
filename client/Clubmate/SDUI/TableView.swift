import SwiftUI

import ClubmateKit

struct TableNodeView: View {
    let node: SDUINode

    @State private var selected: String?
    @State private var sort: SDUISort?
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.scrollProxy) private var scroll

    var body: some View {
        let columns = node.columns()
        let rows = node.tableRows()
        let tree = rows.contains { $0.hasChildren || $0.depth > 0 }
        LazyVStack(alignment: .leading, spacing: 0, pinnedViews: [.sectionHeaders]) {
            Section {
                ForEach(rows) { row in
                    TableRowView(row: row, columns: columns, tree: tree, selected: row.id == selected) {
                        selected = row.id
                        dispatch(node, .select(row.id))
                    } toggle: { expanded in
                        dispatch(node, expanded ? .collapse(row.id) : .expand(row.id))
                    }
                    .id(row.id)
                }
            } header: {
                TableHeader(columns: columns, sort: sort) { column in
                    let next = SDUISort.next(clicking: column, current: sort)
                    sort = next
                    dispatch(node, .sort(column: next.column, desc: next.desc))
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .onChange(of: node.tableSort(), initial: true) { _, value in
            sort = value
        }
        .onChange(of: node.string("selected"), initial: true) { _, value in
            guard value != selected else { return }
            selected = value
            if let value { scroll?.scrollTo(value, anchor: .center) }
        }
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
                    HStack(spacing: 3) {
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
        .font(theme.uiSmall)
        .padding(.horizontal, theme.gutter)
        .frame(height: theme.rowHeight)
        .background(theme.background, ignoresSafeAreaEdges: [])
        .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
    }
}

private struct TableRowView: View {
    let row: SDUITableRow
    let columns: [SDUIColumn]
    let tree: Bool
    let selected: Bool
    let select: () -> Void
    let toggle: (Bool) -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.gutter) {
            ForEach(Array(zip(columns, row.cells).enumerated()), id: \.offset) { index, pair in
                HStack(spacing: 0) {
                    if index == 0 && tree {
                        Disclosure(hasChildren: row.hasChildren, expanded: row.expanded, toggle: toggle)
                            .padding(.leading, CGFloat(row.depth) * theme.indent)
                    }
                    TableCellView(cell: pair.1, muted: row.muted)
                }
                .tableColumn(pair.0)
                .background(alignment: .leading) {
                    if let bar = pair.1.bar {
                        BarShape(fraction: bar)
                            .fill(pair.1.toneColor(theme) ?? theme.accent)
                            .opacity(0.3)
                            .padding(.vertical, 3)
                    }
                }
            }
        }
        .font(theme.ui)
        .padding(.horizontal, theme.gutter)
        .frame(height: theme.rowHeight)
        .background(background, ignoresSafeAreaEdges: [])
        .contentShape(Rectangle())
        .onTapGesture(perform: select)
        .onHover { hovering = $0 }
    }

    private var background: Color {
        if selected { return theme.accent.opacity(0.25) }
        return hovering ? theme.foreground.opacity(0.05) : .clear
    }
}

private struct TableCellView: View {
    let cell: SDUICell
    let muted: Bool

    @Environment(\.theme) private var theme

    var body: some View {
        let tone = cell.toneColor(theme)
        HStack(spacing: 4) {
            if let spark = cell.spark {
                SparklineView(spark: spark, color: tone)
            }
            if !cell.text.isEmpty {
                Text(cell.text)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .foregroundStyle(textColor(tone))
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
