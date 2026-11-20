import SwiftUI

import CompanionKit

struct SidebarParts {
    private(set) var tree: SDUINode?
    private(set) var search: SDUINode?
    private(set) var actions: [SDUINode] = []
    private(set) var rest: [SDUINode] = []

    init(_ node: SDUINode) {
        collect(node)
    }

    private mutating func collect(_ node: SDUINode) {
        switch node.type {
        case "stack", "scroll":
            node.children.forEach { collect($0) }
        case "tree" where tree == nil:
            tree = node
        case "textField" where search == nil:
            search = node
        case "button":
            actions.append(node)
        case "text", "spacer", "divider":
            break
        default:
            rest.append(node)
        }
    }
}

struct SidebarTreeSection: Identifiable {
    struct Row: Identifiable {
        let item: SDUITreeItem
        let depth: Int
        var page = false

        var id: String { (page ? "page:" : "") + item.id }
    }

    let id: String
    let head: SDUITreeItem?
    let rows: [Row]

    static func build(_ items: [SDUITreeItem]) -> [SidebarTreeSection] {
        var sections: [SidebarTreeSection] = []
        var loose: [Row] = []
        func flush() {
            guard let first = loose.first else { return }
            sections.append(SidebarTreeSection(id: "loose:" + first.id, head: nil, rows: loose))
            loose = []
        }
        for item in items {
            guard item.hasChildren else {
                loose.append(Row(item: item, depth: 0))
                continue
            }
            flush()
            let own = item.opensPage ? [Row(item: item, depth: 0, page: true)] : []
            sections.append(SidebarTreeSection(id: item.id, head: item, rows: own + rows(item.shownChildren, depth: 0)))
        }
        flush()
        return sections
    }

    private static func rows(_ items: [SDUITreeItem], depth: Int) -> [Row] {
        items.flatMap { [Row(item: $0, depth: depth)] + ($0.hasChildren ? rows($0.shownChildren, depth: depth + 1) : []) }
    }
}

struct SidebarList: View {
    let node: SDUINode
    let context: SDUIContext
    let connection: ComputerConnection
    @Environment(AppNavigation.self) private var navigation
    @State private var selected: String?
    @State private var choice: MenuChoice?
    @State private var query = ""
    @State private var pending: Task<Void, Never>?

    var body: some View {
        let parts = SidebarParts(node)
        let sections = parts.tree.map { SidebarTreeSection.build($0.treeItems()) } ?? []
        List {
            TabListRows {
                OfflineRow(connection: connection)
                if let tree = parts.tree {
                    ForEach(sections) { section in
                        Section {
                            ForEach(section.rows) { row in
                                rowView(row, tree: tree)
                            }
                        } header: {
                            if let head = section.head {
                                header(head, tree: tree)
                            }
                        }
                    }
                }
                ForEach(Array(parts.rest.enumerated()), id: \.offset) { _, rest in
                    Section {
                        SDUINodeView(node: rest, context: context)
                            .listRowInsets(EdgeInsets())
                            .bareRow()
                    }
                }
            }
        }
        .columnList()
        .tabSearch(.wiki, text: Binding(get: { query }, set: { typed($0, node: parts.search) }), prompt: parts.search?.string("placeholder") ?? "Search")
        .onSubmit(of: .search) { send(query, node: parts.search) }
        .dismissesLiveField()
        .menuConfirm($choice, node: parts.tree ?? node, context: context)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                ForEach(Array(parts.actions.enumerated()), id: \.offset) { _, action in
                    Button(action.string("label") ?? "", systemImage: action.string("icon") ?? "") {
                        SDUIPress.run(action, context: context, form: nil)
                    }
                    .disabled(action.bool("disabled") == true || !connection.canWrite)
                }
            }
        }
        .onChange(of: parts.tree?.string("selected"), initial: true) { _, value in
            selected = value
        }
        .onChange(of: parts.search?.string("value"), initial: true) { _, value in
            if pending == nil { query = value ?? "" }
        }
    }

    private func typed(_ value: String, node: SDUINode?) {
        guard value != query else { return }
        query = value
        pending?.cancel()
        pending = Task {
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled else { return }
            send(value, node: node)
        }
    }

    private func send(_ value: String, node: SDUINode?) {
        pending?.cancel()
        pending = nil
        guard let node else { return }
        context.run { await $0.fire(node, .submit(.string(value))) }
    }

    private func header(_ item: SDUITreeItem, tree: SDUINode) -> some View {
        Button {
            context.run { await $0.fire(tree, item.expanded ? .collapse(item.id) : .expand(item.id)) }
        } label: {
            HStack {
                Text(count(item).map { "\(item.item.label) · \($0)" } ?? item.item.label)
                Spacer()
                Image(systemName: "chevron.right")
                    .rotationEffect(.degrees(item.expanded ? 90 : 0))
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(item.item.label)
        .accessibilityValue(item.expanded ? "Expanded" : "Collapsed")
    }

    private func count(_ item: SDUITreeItem) -> String? {
        if let detail = item.item.detail, !detail.isEmpty { return detail }
        return item.expanded ? String(item.children.count) : nil
    }

    @ViewBuilder
    private func rowView(_ row: SidebarTreeSection.Row, tree: SDUINode) -> some View {
        let item = row.item
        let folder = item.hasChildren && !row.page
        Group {
            if folder && item.opensPage {
                HStack(spacing: 0) {
                    Button { tap(item, tree: tree) } label: {
                        SidebarTreeRow(item: item, depth: row.depth, trailing: .none)
                    }
                    .buttonStyle(.borderless)
                    Button {
                        context.run { await $0.fire(tree, item.expanded ? .collapse(item.id) : .expand(item.id)) }
                    } label: {
                        FoldChevron(expanded: item.expanded)
                            .frame(width: 32, height: 44)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(item.expanded ? "Collapse \(item.item.label)" : "Expand \(item.item.label)")
                }
            } else {
                Button {
                    if row.page {
                        selected = item.target
                        context.run { await $0.select(tree, item.target) }
                    } else {
                        tap(item, tree: tree)
                    }
                } label: {
                    SidebarTreeRow(item: item, depth: row.depth, trailing: folder ? .fold(item.expanded) : .chevron)
                }
                .foregroundStyle(.primary)
            }
        }
        .listRowDensity()
        .columnRow(selected: !folder && item.target == selected)
        .itemMenu(item.item.menu) { context.choose($0, item: item.target, in: tree, confirm: &choice) }
    }

    private func tap(_ item: SDUITreeItem, tree: SDUINode) {
        switch item.tap {
        case .select(let target):
            if !item.action { selected = target }
            context.run { await $0.select(tree, target) }
        case .open(let target, let unfold):
            selected = target
            context.run { context in
                await context.select(tree, target)
                if unfold { await context.fire(tree, .expand(item.id)) }
            }
        case .fold(let expand):
            context.run { await $0.fire(tree, expand ? .expand(item.id) : .collapse(item.id)) }
        }
    }
}

struct SidebarTreeRow: View {
    enum Trailing: Equatable {
        case none
        case chevron
        case fold(Bool)
    }

    let item: SDUITreeItem
    let depth: Int
    let trailing: Trailing
    @Environment(\.horizontalSizeClass) private var sizeClass

    private var pad: Bool { PadLayout.on(sizeClass) }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            RowIcon(systemImage: icon, tint: SDUITone.ink(item.item.tone), fill: SDUITone.fill(item.item.tone)?.opacity(0.18))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(item.item.label)
                    .lineLimit(pad ? 1 : 2)
                    .truncationMode(item.item.truncate.mode)
                    .foregroundStyle(item.faint ? AnyShapeStyle(.tertiary) : AnyShapeStyle(item.muted ? Color.captionInk : Color(.label)))
                if !pad, let subtitle {
                    subtitle
                        .font(.subheadline)
                        .lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            switch trailing {
            case .none:
                EmptyView()
            case .chevron:
                if !pad {
                    RowChevron()
                        .frame(height: 22)
                }
            case .fold(let expanded):
                FoldChevron(expanded: expanded)
                    .frame(height: 22)
            }
        }
        .frame(minHeight: 44)
        .padding(.leading, CGFloat(depth) * 24)
        .contentShape(.rect)
    }

    private var icon: String {
        if let icon = item.item.icon, !icon.isEmpty { return icon }
        return item.hasChildren ? "folder" : "doc.text"
    }

    private var subtitle: Text? {
        var parts: [Text] = []
        if let detail = item.item.detail, !detail.isEmpty, !item.hasChildren || item.opensPage {
            parts.append(Text(detail).foregroundStyle(SDUITone.ink(item.detailTone) ?? Color.captionInk))
        }
        parts += item.badges.map { Text($0.text).foregroundStyle(SDUITone.ink($0.tone) ?? Color.captionInk) }
        guard let first = parts.first else { return nil }
        return parts.dropFirst().reduce(first) { $0 + Text(" · ").foregroundStyle(Color.captionInk) + $1 }
    }
}

struct FoldChevron: View {
    let expanded: Bool

    var body: some View {
        Image(systemName: "chevron.right")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(.tertiary)
            .rotationEffect(.degrees(expanded ? 90 : 0))
            .accessibilityHidden(true)
    }
}
