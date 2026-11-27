import SwiftUI

import CompanionKit

struct TabsNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var selected = ""
    @Environment(\.sduiInset) private var inset

    var body: some View {
        if node.bool("segmented") == true {
            Picker("", selection: Binding(get: { selected }, set: { id in
                selected = id
                context.run { await $0.fire(node, .select(id)) }
            })) {
                ForEach(node.items()) { item in
                    Text(item.label).tag(item.id)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .padding(.horizontal, inset)
            .onChange(of: node.string("selected"), initial: true) { _, value in
                selected = value ?? ""
            }
        } else {
            chips
        }
    }

    private var chips: some View {
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
                                .columnRow(selected: id == selected, always: true)
                        }
                    } header: {
                        header(section.id)
                    }
                }
            }
            .columnList()
            .dismissesLiveField()
            .environment(\.sduiInScroll, true)
        }
    }
}

extension RowGroup where Header == EmptyView {
    init(ids: [String], selected: String?, @ViewBuilder row: @escaping (String) -> Row) {
        self.init(sections: [RowSection(id: "", ids: ids)], selected: selected, header: { _ in EmptyView() }, row: row)
    }
}

extension SessionGlyph {
    init?(statusIcon icon: String, needsYou: Bool) {
        switch icon {
        case "play.circle": self = .running
        case "exclamationmark.circle": self = needsYou ? .permission : .childWaiting
        case "arrow.up.circle": self = .asksParent
        case "checkmark.circle", "circle": self = .done
        case "eye.circle": self = .review
        case "xmark.circle": self = .stopped
        case "archivebox": self = .closed
        case SessionLanding.shield: self = .held
        default: return nil
        }
    }
}

struct ItemLabel: View {
    let item: SDUIItem
    var detailTone: String?
    var badgeTone: String?
    var muted = false
    var faint = false
    var opens = false
    @Environment(\.sduiRowHeight) private var rowHeight

    var body: some View {
        HStack(spacing: 12) {
            if let icon = item.icon, !icon.isEmpty {
                if rowHeight != nil, let glyph = SessionGlyph(statusIcon: icon, needsYou: item.badge?.isEmpty == false) {
                    SessionGlyphView(glyph: glyph)
                } else {
                    Image(systemName: icon)
                        .foregroundStyle(SDUITone.ink(item.tone) ?? Color.captionInk)
                        .frame(width: 28)
                }
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(item.label)
                    .lineLimit(2)
                    .truncationMode(item.truncate.mode)
                    .foregroundStyle(faint ? AnyShapeStyle(.tertiary) : AnyShapeStyle(muted ? Color.captionInk : .primary))
                if let detail = item.detail, !detail.isEmpty {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(SDUITone.ink(detailTone) ?? Color.captionInk)
                        .lineLimit(1)
                }
                if let caption = item.caption {
                    Text(caption)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            if let badge = item.badge, !badge.isEmpty {
                BadgeView(text: badge, tone: badgeTone)
            }
            if opens {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .frame(minHeight: rowHeight ?? (item.caption == nil ? 52 : 56))
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
                    ForEach(Array(entries.enumerated()), id: \.element.id) { index, entry in
                        if entry.divider, index > 0 { Divider() }
                        Button(entry.label, role: entry.confirm == nil ? nil : .destructive) { choose(entry) }
                            .disabled(entry.disabled)
                    }
                }
                .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                    ForEach(entries.filter { $0.confirm == nil && !$0.disabled }.prefix(2)) { entry in
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
    @State private var cursor: String?
    @State private var choice: MenuChoice?
    @State private var query = ""
    @FocusState private var listFocused: Bool
    @FocusState private var filterFocused: Bool
    @Environment(\.sduiKeysTyping) private var keysTyping

    private var picker: Bool { node.string("focus") != nil }

    private var filter: SDUIListFilter? {
        guard let filter = node.listFilter(), filter.shown(rows: node.items().filter { !$0.section }.count) else { return nil }
        return filter
    }

    private var sections: [RowSection] {
        let active = filter != nil && !query.trimmingCharacters(in: .whitespaces).isEmpty
        var sections: [RowSection] = []
        var current = ""
        var ids: [String] = []
        for item in node.items() {
            if item.section {
                if !current.isEmpty || !ids.isEmpty { sections.append(RowSection(id: current, ids: ids)) }
                current = item.id
                ids = []
            } else if !active || SDUIListFilter.matches(item, query) {
                ids.append(item.id)
            }
        }
        sections.append(RowSection(id: current, ids: ids))
        return active ? sections.filter { !$0.ids.isEmpty } : sections
    }

    var body: some View {
        let items = node.items()
        let byID = Dictionary(items.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        VStack(alignment: .leading, spacing: 8) {
            if let filter {
                TextField(filter.placeholder, text: $query)
                    .textFieldStyle(.roundedBorder)
                    .padding(.horizontal, 16)
                    .focused($filterFocused)
                    .onDisappear { keysTyping?.wrappedValue = false }
                    .onSubmit { if let first = sections.lazy.flatMap(\.ids).first { take(first) } }
                    .onKeyPress(.escape) {
                        guard !query.isEmpty else { return .ignored }
                        query = ""
                        return .handled
                    }
                    .onKeyPress(.downArrow) {
                        filterFocused = false
                        listFocused = true
                        return .handled
                    }
            }
            RowGroup(sections: sections, selected: picker ? cursor : selected) { id in
                if let heading = byID[id] {
                    HStack {
                        Text(heading.label)
                        Spacer()
                        if let detail = heading.detail { Text(detail).monospacedDigit() }
                    }
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.captionInk)
                }
            } row: { id in
                if let item = byID[id] {
                    Button {
                        take(id)
                    } label: {
                        ItemLabel(item: item, badgeTone: item.tone, opens: context.mainSlot != nil)
                    }
                    .buttonStyle(.plain)
                    .itemMenu(item.menu) { context.choose($0, item: id, in: node, confirm: &choice) }
                }
            }
            .modifier(PickerKeys(on: picker, focused: $listFocused, press: press))
        }
        .menuConfirm($choice, node: node, context: context)
        .onChange(of: node.string("selected"), initial: true) { _, value in
            selected = value
        }
        .onChange(of: node.string("focus"), initial: true) { _, value in
            cursor = value
            if value != nil { listFocused = true }
        }
        .onChange(of: filterFocused) { _, focused in
            keysTyping?.wrappedValue = focused
        }
        .onChange(of: query) {
            let ids = sections.flatMap(\.ids)
            if query.trimmingCharacters(in: .whitespaces).isEmpty {
                cursor = node.string("focus") ?? cursor
            } else if cursor.map({ !ids.contains($0) }) ?? true {
                cursor = ids.first
            }
        }
    }

    private func take(_ id: String) {
        selected = id
        cursor = id
        context.run { await $0.select(node, id) }
    }

    private func press(_ press: KeyPress) -> KeyPress.Result {
        switch (press.key, press.characters) {
        case (.return, _):
            guard let cursor else { return .ignored }
            take(cursor)
        case (.downArrow, _), (_, "j"):
            move(1)
        case (.upArrow, _), (_, "k"):
            move(-1)
        case (_, "/") where filter != nil:
            filterFocused = true
        default:
            return .ignored
        }
        return .handled
    }

    private func move(_ step: Int) {
        let ids = sections.flatMap(\.ids)
        let next = (cursor.flatMap { ids.firstIndex(of: $0) } ?? (step > 0 ? -1 : ids.count)) + step
        if ids.indices.contains(next) { cursor = ids[next] }
    }
}

private struct PickerKeys: ViewModifier {
    let on: Bool
    var focused: FocusState<Bool>.Binding
    let press: (KeyPress) -> KeyPress.Result

    func body(content: Content) -> some View {
        if on {
            content
                .focusable()
                .focused(focused)
                .focusEffectDisabled()
                .onKeyPress(phases: .down, action: press)
        } else {
            content
        }
    }
}

struct KeysNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var typing = false

    private var keys: Set<Character> {
        Set((node.props["keys"]?.arrayValue ?? []).compactMap { $0.stringValue?.first })
    }

    var body: some View {
        SDUIChildren(node: node, context: context)
            .environment(\.sduiKeysTyping, $typing)
            .onKeyPress(characters: CharacterSet(charactersIn: String(keys)), phases: .down) { press in
                guard !typing, let action = node.string("action"), press.characters.first.map(keys.contains) == true else { return .ignored }
                context.run { await $0.fire(node, .named(action, .null)) }
                return .handled
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

    private static func docked(_ node: SDUINode) -> [SDUITreeItem] {
        (node.props["dock"]?.arrayValue ?? []).compactMap { section in
            guard var fields = section.objectValue else { return nil }
            let rows = fields.removeValue(forKey: "rows")?.arrayValue ?? []
            fields.removeValue(forKey: "attention")
            fields["children"] = .array(rows)
            fields["hasChildren"] = .bool(!rows.isEmpty)
            return SDUITreeItem(.object(fields))
        }
    }

    var body: some View {
        let rows = Self.rows(node.treeItems() + Self.docked(node))
        let gutter = rows.contains { $0.item.hasChildren }
        let byID = Dictionary(rows.map { ($0.item.id, $0) }, uniquingKeysWith: { first, _ in first })
        let highlighted = selected.map { selected in rows.first { $0.item.id == selected }?.item.id ?? rows.first { $0.item.target == selected }?.item.id ?? selected }
        RowGroup(ids: rows.map(\.item.id), selected: highlighted) { id in
            if let row = byID[id] {
                let page = row.item.hasChildren && row.item.opensPage
                HStack(spacing: 0) {
                    Color.clear.frame(width: CGFloat(row.depth) * 16, height: 1)
                    if page {
                        Color.clear.frame(width: 32, height: 1)
                    } else {
                        disclosure(row.item, gutter: gutter)
                    }
                    Button {
                        tap(row.item)
                    } label: {
                        ItemLabel(item: row.item.item, detailTone: row.item.detailTone, muted: row.item.muted, faint: row.item.faint, opens: context.mainSlot != nil && !row.item.hasChildren && !row.item.action)
                    }
                    .buttonStyle(.plain)
                    if page { trailingDisclosure(row.item) }
                }
                .itemMenu(row.item.item.menu) { context.choose($0, item: row.item.target, in: node, confirm: &choice) }
            }
        }
        .menuConfirm($choice, node: node, context: context)
        .background {
            Button("Remove") {
                guard let selected, let entry = byID[selected]?.item.item.menu.first(where: { $0.key == "cmd+delete" }) else { return }
                choice = MenuChoice(entry: entry, item: selected)
            }
            .keyboardShortcut(.delete, modifiers: .command)
            .opacity(0)
            .accessibilityHidden(true)
        }
        .onChange(of: node.string("selected"), initial: true) { _, value in
            selected = value
        }
    }

    private func tap(_ item: SDUITreeItem) {
        switch item.tap {
        case .select(let target):
            if !item.action { selected = target }
            context.run { await $0.select(node, target) }
        case .open(let target, let unfold):
            selected = target
            context.run { context in
                await context.select(node, target)
                if unfold { await context.fire(node, .expand(item.id)) }
            }
        case .fold(let expand):
            context.run { await $0.fire(node, expand ? .expand(item.id) : .collapse(item.id)) }
        }
    }

    private func trailingDisclosure(_ item: SDUITreeItem) -> some View {
        Button {
            context.run { await $0.fire(node, item.expanded ? .collapse(item.id) : .expand(item.id)) }
        } label: {
            Image(systemName: "chevron.right")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.captionInk)
                .rotationEffect(.degrees(item.expanded ? 90 : 0))
                .frame(width: 44, height: 44)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(item.expanded ? "Collapse \(item.item.label)" : "Expand \(item.item.label)")
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
                    .foregroundStyle(Color.captionInk)
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
        .tint(Color.captionInk)
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
            .foregroundStyle(Color.captionInk)
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
                        .foregroundStyle(Color.captionInk)
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
                    .foregroundStyle(cell.bar == nil ? SDUITone.ink(cell.tone) ?? Color.captionInk : Color.captionInk)
            }
        }
        .font(.footnote)
    }
}
