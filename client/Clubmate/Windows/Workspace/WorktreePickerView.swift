import AppKit
import SwiftUI

import ClubmateKit

extension View {
    func worktreePicker(_ session: WorkspaceSession, addFolder: @escaping () -> Void) -> some View {
        overlay(alignment: .topLeading) {
            if session.overlay == .worktrees {
                GeometryReader { window in
                    WorktreePickerPanel(session: session, window: window.size, addFolder: addFolder)
                }
            }
        }
    }
}

private enum WorktreePickerMetrics {
    static let narrowWidth: CGFloat = 460
    static let wideWidth: CGFloat = 700
    static let fallbackWidth: CGFloat = 480
    static let actionsWidth: CGFloat = 300
    static let margin: CGFloat = 12
    static let bottomMargin: CGFloat = 40
    static let pillFallback: CGFloat = 82
    static let chromeHeight: CGFloat = 120
    static let actionsGap: CGFloat = 10
    static let actionsTop: CGFloat = 50
    static let enterKey: UInt16 = 36
    static let leftKey: UInt16 = 123
    static let rightKey: UInt16 = 124
    static let upKey: UInt16 = 126
    static let downKey: UInt16 = 125
    static let escapeKey: UInt16 = 53
}

private struct WorktreePickerPanel: View {
    let session: WorkspaceSession
    let window: CGSize
    let addFolder: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        let picker = session.worktreePicker
        let width = panelWidth(picker)
        Group {
            if picker.unavailable {
                WorktreeFallbackList(session: session, close: close)
            } else {
                WorktreePickerContent(session: session, picker: picker, wide: !(picker.index?.byFolder ?? true), maxListHeight: maxListHeight, addFolder: addFolder, close: close)
            }
        }
        .frame(width: width)
        .modifier(PickerChrome())
        .overlay(alignment: .topLeading) {
            if let folder = picker.actionsFolder {
                WorktreeFolderActions(session: session, picker: picker, folder: folder, close: close)
                    .frame(width: WorktreePickerMetrics.actionsWidth)
                    .offset(x: actionsLeading(width), y: WorktreePickerMetrics.actionsTop)
            }
        }
        .onClickOutside { [session] in session.closeWorktreePicker() }
        .padding(.leading, min(pill, max(WorktreePickerMetrics.margin, window.width - width - WorktreePickerMetrics.margin)))
    }

    private var pill: CGFloat {
        session.worktreePillLeading > 0 ? session.worktreePillLeading : WorktreePickerMetrics.pillFallback
    }

    private var maxListHeight: CGFloat {
        max(theme.rowHeight * 6, window.height - WorktreePickerMetrics.chromeHeight - WorktreePickerMetrics.bottomMargin)
    }

    private func panelWidth(_ picker: WorktreePickerModel) -> CGFloat {
        let wanted: CGFloat
        if picker.unavailable {
            wanted = WorktreePickerMetrics.fallbackWidth
        } else {
            wanted = picker.index?.byFolder ?? true ? WorktreePickerMetrics.narrowWidth : WorktreePickerMetrics.wideWidth
        }
        return min(wanted, window.width - WorktreePickerMetrics.margin * 2)
    }

    private func actionsLeading(_ width: CGFloat) -> CGFloat {
        let beside = width + WorktreePickerMetrics.actionsGap
        let fits = pill + beside + WorktreePickerMetrics.actionsWidth + WorktreePickerMetrics.margin <= window.width
        return fits ? beside : width - WorktreePickerMetrics.actionsWidth - WorktreePickerMetrics.margin
    }

    private func close() {
        session.closeWorktreePicker()
    }
}

private struct WorktreeFallbackList: View {
    let session: WorkspaceSession
    let close: () -> Void

    @State private var model: FuzzyPickerModel
    @Environment(\.theme) private var theme

    init(session: WorkspaceSession, close: @escaping () -> Void) {
        self.session = session
        self.close = close
        _model = State(initialValue: FuzzyPickerModel(placeholder: "Switch to worktree or branch", source: session.worktrees.switcherSource))
    }

    var body: some View {
        let worktrees = session.worktrees
        VStack(spacing: 0) {
            FuzzyPickerList(model: model, choose: session.pickWorktree, dismiss: close)
            theme.border.frame(height: 1)
            VStack(spacing: 0) {
                WorktreePickerAction(title: "New worktree…", icon: "plus") {
                    close()
                    session.newWorktree(chooseFolder: true)
                }
                if !worktrees.linked.isEmpty {
                    WorktreePickerAction(title: "Remove worktree…", icon: "trash") {
                        close()
                        session.chooseWorktreeToRemove()
                    }
                }
            }
            .padding(.vertical, theme.spacing.s)
        }
        .task {
            await worktrees.load()
            model.update(model.query)
            await worktrees.loadBranches()
            model.update(model.query)
        }
    }
}

private struct WorktreePickerContent: View {
    let session: WorkspaceSession
    let picker: WorktreePickerModel
    let wide: Bool
    let maxListHeight: CGFloat
    let addFolder: () -> Void
    let close: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(spacing: 0) {
            header
            theme.border.frame(height: 1)
            list
            theme.border.frame(height: 1)
            footer
        }
        .keyMonitor(active: picker.actionsFolder == nil, handle: handleKey)
    }

    private var header: some View {
        HStack(spacing: theme.spacing.l) {
            if picker.inside {
                Button { _ = picker.back() } label: {
                    HStack(spacing: theme.spacing.s) {
                        Image(systemName: "chevron.left")
                            .font(theme.uiSmall)
                        Text("All folders")
                            .lineLimit(1)
                    }
                    .font(theme.ui)
                    .foregroundStyle(theme.muted)
                }
                .buttonStyle(.plain)
                .fixedSize()
            }
            field
            if let folder = picker.scopedFolder, let target = picker.actionsTarget(), target.path == folder.path {
                WorktreeMoreButton { picker.actionsFolder = folder }
            }
        }
        .padding(theme.gutter)
    }

    private var field: some View {
        HStack(spacing: theme.spacing.m) {
            Image(systemName: "magnifyingglass")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
            PaletteField(
                text: Binding(get: { picker.query }, set: { picker.update($0) }),
                placeholder: picker.placeholder,
                move: picker.move,
                submit: submit,
                cancel: cancel,
                fontSize: theme.uiSize
            )
            .frame(maxWidth: .infinity)
            if !picker.caption.isEmpty {
                Text(picker.caption)
                    .font(theme.uiSmall.monospacedDigit())
                    .foregroundStyle(theme.faint)
                    .lineLimit(1)
                    .fixedSize()
            }
        }
        .padding(.horizontal, theme.spacing.l)
        .frame(height: theme.control)
        .fieldChrome(focused: true)
    }

    @ViewBuilder private var list: some View {
        if picker.rows.isEmpty {
            Text(picker.index == nil ? "Loading…" : "No matches")
                .font(theme.ui)
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, theme.gutter * 1.5)
                .frame(height: theme.rowHeight + 2)
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(Array(picker.rows.enumerated()), id: \.element.id) { index, row in
                            WorktreePickerRowView(row: row, selected: index == picker.selection, terms: picker.terms, session: session, picker: picker)
                                .id(row.id)
                                .onTapGesture {
                                    guard row.selectable else { return }
                                    picker.select(index)
                                    submit()
                                }
                        }
                    }
                    .padding(.vertical, theme.spacing.s)
                }
                .frame(height: min(listHeight, maxListHeight))
                .onChange(of: picker.selection, initial: true) { _, _ in
                    if let row = picker.selectedRow { proxy.scrollTo(row.id) }
                }
            }
        }
    }

    private var listHeight: CGFloat {
        picker.rows.reduce(theme.spacing.s * 2) { $0 + height(of: $1) }
    }

    private func height(of row: WorktreePickerRow) -> CGFloat {
        switch row {
        case .section: theme.rowHeight + 8
        case .folderHeader(let folder): theme.rowHeight + 6 + (folder.byline.isEmpty ? 0 : theme.rowHeight)
        case .task: theme.rowHeight2 + 6
        default: theme.rowHeight + 2
        }
    }

    @ViewBuilder private var footer: some View {
        if picker.searching, wide, let hint = picker.index?.searchHint {
            Text(hint)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, theme.gutter * 1.5)
                .padding(.vertical, theme.spacing.m)
        } else if wide {
            HStack(spacing: theme.spacing.s) {
                actions
                Spacer(minLength: 0)
            }
            .padding(.vertical, theme.spacing.s)
            .padding(.horizontal, theme.spacing.m)
        } else {
            VStack(spacing: 0) {
                actions
            }
            .padding(.vertical, theme.spacing.s)
        }
    }

    @ViewBuilder private var actions: some View {
        WorktreePickerAction(title: "New worktree…", icon: "plus", wide: wide) {
            let folder = picker.scopedFolder?.path
            close()
            if let folder {
                session.newWorktree(repo: folder)
            } else {
                session.newWorktree(chooseFolder: true)
            }
        }
        WorktreePickerAction(title: picker.index?.addFolder ?? "", icon: "folder.badge.plus", wide: wide) {
            close()
            addFolder()
        }
        if !session.worktrees.linked.isEmpty {
            WorktreePickerAction(title: "Remove worktree…", icon: "trash", wide: wide) {
                close()
                session.chooseWorktreeToRemove()
            }
        }
    }

    private func submit() {
        switch picker.activate() {
        case .worktree(let entry):
            close()
            session.openWorktree(entry)
        case .branch(let branch):
            close()
            session.switchBranch(branch)
        case nil:
            break
        }
    }

    private func cancel() {
        if !picker.back() { close() }
    }

    private func handleKey(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection([.command, .control, .option, .shift])
        switch (event.keyCode, flags) {
        case (WorktreePickerMetrics.enterKey, .command):
            guard let folder = picker.actionsTarget() else { return false }
            picker.actionsFolder = folder
        case (WorktreePickerMetrics.rightKey, []):
            guard picker.opensForward else { return false }
            submit()
        case (WorktreePickerMetrics.leftKey, []):
            guard picker.query.isEmpty else { return false }
            return picker.back()
        default:
            return false
        }
        return true
    }
}

private struct WorktreePickerRowView: View {
    let row: WorktreePickerRow
    let selected: Bool
    let terms: [String]
    let session: WorkspaceSession
    let picker: WorktreePickerModel

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        switch row {
        case .section(_, let title, let hint):
            HStack(spacing: theme.gutter) {
                Text(title)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .layoutPriority(1)
                Spacer(minLength: theme.gutter)
                Text(hint)
                    .foregroundStyle(theme.faint)
                    .lineLimit(1)
                    .truncationMode(.label)
            }
            .font(theme.uiSmall)
            .padding(.horizontal, theme.gutter * 1.5)
            .frame(height: theme.rowHeight + 8, alignment: .bottom)
        case .folderHeader(let folder):
            folderHeader(folder)
        case .worktree(let entry, let style):
            pickable(height: theme.rowHeight + 2) { worktree(entry, style) }
        case .task(let task):
            pickable(height: theme.rowHeight2 + 6) { taskRow(task) }
        case .folder(let folder):
            pickable(height: theme.rowHeight + 2) { folderRow(folder) }
        case .note(_, let text):
            Text(text)
                .font(theme.uiSmall)
                .foregroundStyle(theme.faint)
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.leading, theme.gutter * 1.5 + 14 + theme.gutter)
                .frame(height: theme.rowHeight + 2)
        case .fold(_, let text):
            pickable(height: theme.rowHeight + 2) {
                HStack(spacing: theme.gutter) {
                    Image(systemName: "chevron.right")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                        .frame(width: 14)
                    Text(text)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                }
            }
        case .branch(let branch, let folder):
            pickable(height: theme.rowHeight + 2) {
                HStack(spacing: theme.gutter) {
                    glyph("arrow.triangle.branch", tone: "")
                    label(folder: folder, name: branch.name, faint: true)
                    Spacer(minLength: theme.gutter)
                    if branch.remote {
                        Text("remote")
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.muted)
                    }
                }
            }
        }
    }

    private func pickable(height: CGFloat, @ViewBuilder content: () -> some View) -> some View {
        content()
            .font(theme.ui)
            .padding(.horizontal, theme.gutter * 1.5)
            .frame(height: height)
            .background(selected ? theme.selected : hovering ? theme.hover : .clear, ignoresSafeAreaEdges: [])
            .overlay(alignment: .leading) {
                if selected { theme.accent.frame(width: 2) }
            }
            .contentShape(Rectangle())
            .onHover { hovering = $0 }
    }

    private func worktree(_ entry: WorktreeEntry, _ style: WorktreePickerRow.Style) -> some View {
        HStack(spacing: theme.gutter) {
            if style.plain {
                Image(systemName: "checkmark")
                    .font(theme.uiSmall.bold())
                    .foregroundStyle(theme.accentInk)
                    .opacity(entry.now ? 1 : 0)
                    .frame(width: 14)
            } else {
                glyph(entry.now ? "checkmark" : entry.icon, tone: entry.now ? "accent" : entry.tone)
            }
            label(folder: style.folder ? entry.folder : "", name: entry.name, faint: entry.merged && style.plain)
                .padding(.leading, style.indented ? theme.gutter : 0)
                .help(entry.path)
            Spacer(minLength: theme.gutter)
            Text(style.state ? entry.taskState : entry.detail)
                .font(theme.uiSmall)
                .foregroundStyle(theme.tone(entry.tone == "accent" ? nil : entry.tone) ?? theme.muted)
                .lineLimit(1)
                .truncationMode(.label)
                .help(entry.detail)
        }
        .lineLimit(1)
    }

    private func taskRow(_ task: WorktreeTaskRow) -> some View {
        HStack(alignment: .top, spacing: theme.gutter) {
            glyph(task.icon, tone: task.tone)
                .padding(.top, 2)
            VStack(alignment: .leading, spacing: 2) {
                Text(highlighted(task.title))
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                Text(task.state)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: theme.gutter)
            Text(task.worktrees.count > 1 ? "→ pick folder" : "")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .fixedSize()
        }
    }

    private func folderRow(_ folder: WorktreeFolder) -> some View {
        HStack(spacing: theme.gutter) {
            glyph("folder", tone: "")
            Text(folder.name)
                .font(theme.strong)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .truncationMode(.label)
                .layoutPriority(1)
            if folder.new { WorktreeNewChip() }
            Spacer(minLength: theme.gutter)
            Text(folder.summary)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
            Image(systemName: "chevron.right")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
        }
    }

    private func folderHeader(_ folder: WorktreeFolder) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: theme.gutter) {
                glyph("folder", tone: "")
                Text(folder.name)
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(folder.path)
                    .layoutPriority(1)
                if folder.new { WorktreeNewChip() }
                Spacer(minLength: theme.gutter)
                Text(folder.caption)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
                WorktreeMoreButton { picker.actionsFolder = folder }
            }
            .frame(height: theme.rowHeight + 6, alignment: .bottom)
            if !folder.byline.isEmpty {
                HStack(spacing: theme.spacing.s) {
                    Text(folder.byline)
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                    if !folder.session.isEmpty {
                        Text("·")
                            .foregroundStyle(theme.faint)
                        Button {
                            session.closeWorktreePicker()
                            session.openSession(folder.session)
                        } label: {
                            Text("open session")
                                .underline()
                                .foregroundStyle(theme.text2)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .font(theme.uiSmall)
                .padding(.leading, 14 + theme.gutter)
                .frame(height: theme.rowHeight, alignment: .center)
            }
        }
        .padding(.horizontal, theme.gutter * 1.5)
    }

    private func glyph(_ name: String, tone: String) -> some View {
        Image(systemName: name)
            .font(theme.uiSmall)
            .foregroundStyle(theme.tone(tone) ?? theme.muted)
            .frame(width: 14)
    }

    private func label(folder: String, name: String, faint: Bool) -> some View {
        HStack(spacing: theme.spacing.m) {
            if !folder.isEmpty {
                Text(highlighted(folder))
                    .foregroundStyle(theme.muted)
                    .truncationMode(.label)
                Text("▸")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.faint)
                    .fixedSize()
            }
            Text(highlighted(name))
                .foregroundStyle(faint ? theme.faint : theme.foreground)
                .truncationMode(.label)
                .layoutPriority(1)
        }
        .lineLimit(1)
    }

    private func highlighted(_ text: String) -> AttributedString {
        var out = AttributedString(text)
        let lower = text.lowercased()
        guard lower.count == text.count else { return out }
        for term in terms {
            var from = lower.startIndex
            while let found = lower.range(of: term, range: from..<lower.endIndex) {
                let start = out.index(out.startIndex, offsetByCharacters: lower.distance(from: lower.startIndex, to: found.lowerBound))
                let end = out.index(start, offsetByCharacters: lower.distance(from: found.lowerBound, to: found.upperBound))
                out[start..<end].backgroundColor = selected ? theme.toneBorder(theme.accent) : theme.selected
                from = found.upperBound
            }
        }
        return out
    }
}

private struct WorktreeNewChip: View {
    @Environment(\.theme) private var theme

    var body: some View {
        Text("new")
            .font(theme.uiSmall)
            .foregroundStyle(theme.accentInk)
            .padding(.horizontal, theme.spacing.m)
            .background(Capsule().fill(theme.toneBg(theme.accent)))
            .fixedSize()
    }
}

private struct WorktreeMoreButton: View {
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            Image(systemName: "ellipsis")
                .font(theme.uiSmall)
                .foregroundStyle(theme.text2)
                .frame(width: theme.rowHeight, height: theme.rowHeight)
                .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(hovering ? theme.hover : .clear))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help("Folder actions (⌘↩)")
    }
}

private struct WorktreePickerAction: View {
    let title: String
    let icon: String
    var wide = false
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: theme.gutter) {
                Image(systemName: icon)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .frame(width: 14)
                Text(title)
                    .font(theme.ui)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .truncationMode(.label)
            }
            .frame(maxWidth: wide ? nil : .infinity, alignment: .leading)
            .padding(.horizontal, wide ? theme.spacing.m : theme.gutter * 1.5)
            .frame(height: theme.rowHeight + 2)
            .background(hovering ? theme.hover : .clear, ignoresSafeAreaEdges: [])
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}

private struct WorktreeFolderActions: View {
    let session: WorkspaceSession
    let picker: WorktreePickerModel
    let folder: WorktreeFolder
    let close: () -> Void

    @State private var selection = 0
    @Environment(\.theme) private var theme

    private struct Item: Identifiable {
        let id: String
        var note = ""
        var destructive = false
        let run: () -> Void
    }

    private var items: [Item] {
        var out: [Item] = []
        if !folder.session.isEmpty {
            out.append(Item(id: "Open its session") { [session, folder, close] in
                close()
                session.openSession(folder.session)
            })
        }
        out.append(Item(id: "Reveal in Finder") { [folder, close] in
            close()
            NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: folder.path)])
        })
        if picker.multipleFolders, let word = picker.index?.remove {
            out.append(Item(id: word, note: "keeps the files", destructive: true) { [session, folder, close] in
                close()
                session.askToRemoveFolder(folder.path)
            })
        }
        return out
    }

    var body: some View {
        let items = items
        VStack(spacing: 0) {
            ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
                if item.destructive {
                    theme.border.frame(height: 1)
                        .padding(.vertical, theme.spacing.s)
                }
                Button(action: item.run) {
                    HStack(spacing: theme.gutter) {
                        Text(item.id)
                            .font(theme.ui)
                            .foregroundStyle(item.destructive ? theme.danger : theme.foreground)
                            .lineLimit(1)
                        Spacer(minLength: theme.gutter)
                        Text(item.note)
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.muted)
                            .lineLimit(1)
                    }
                    .padding(.horizontal, theme.gutter * 1.5)
                    .frame(height: theme.rowHeight + 4)
                    .background(index == selection ? theme.selected : .clear, ignoresSafeAreaEdges: [])
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .onHover { if $0 { selection = index } }
            }
        }
        .padding(.vertical, theme.spacing.s)
        .modifier(PickerChrome())
        .keyMonitor(active: true, handle: { handleKey($0, items) })
        .onClickOutside { picker.actionsFolder = nil }
    }

    private func handleKey(_ event: NSEvent, _ items: [Item]) -> Bool {
        switch event.keyCode {
        case WorktreePickerMetrics.upKey:
            selection = (selection + items.count - 1) % items.count
        case WorktreePickerMetrics.downKey:
            selection = (selection + 1) % items.count
        case WorktreePickerMetrics.enterKey:
            items[selection].run()
        case WorktreePickerMetrics.escapeKey:
            picker.actionsFolder = nil
        default:
            return false
        }
        return true
    }
}
