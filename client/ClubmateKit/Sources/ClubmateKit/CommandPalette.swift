import Foundation

public struct PaletteEntry: Hashable, Sendable {
    public let id: String
    public let title: String
    public let detail: String

    public init(id: String, title: String, detail: String) {
        self.id = id
        self.title = title
        self.detail = detail
    }
}

public enum CommandPalette {
    public static func rank(_ entries: [PaletteEntry], query: String) -> [PaletteEntry] {
        FuzzyRank.rank(entries, query: query, text: \.title)
    }
}

public struct PaletteWorkspace: Hashable, Sendable {
    public let id: String
    public let name: String

    public init(id: String, name: String) {
        self.id = id
        self.name = name
    }
}

public enum PaletteAction: Hashable, Sendable {
    case command(String)
    case section(String)
    case toggleSidebar
    case openFolder
    case newHomeTerminal
    case renameWorkspace
    case closeWorkspace
    case removeWorkspace
    case moveWorkspaceToNewWindow
    case switchWorkspace(String)
    case switchWorktree
    case newWorktree
    case removeWorktree
    case openDoc

    private static let fixed: [PaletteAction] = [
        .toggleSidebar, .openFolder, .newHomeTerminal,
        .renameWorkspace, .closeWorkspace, .removeWorkspace, .moveWorkspaceToNewWindow,
        .switchWorktree, .newWorktree, .removeWorktree, .openDoc,
    ]

    public init?(entryID: String) {
        if let id = entryID.dropPrefix("command:") {
            self = .command(id)
        } else if let id = entryID.dropPrefix("section:") {
            self = .section(id)
        } else if let id = entryID.dropPrefix("workspace:") {
            self = .switchWorkspace(id)
        } else if let action = Self.fixed.first(where: { $0.entryID == entryID }) {
            self = action
        } else {
            return nil
        }
    }

    public var entryID: String {
        switch self {
        case .command(let id): "command:\(id)"
        case .section(let id): "section:\(id)"
        case .switchWorkspace(let id): "workspace:\(id)"
        case .toggleSidebar: "toggle-sidebar"
        case .openFolder: "open-folder"
        case .newHomeTerminal: "new-home-terminal"
        case .renameWorkspace: "rename-workspace"
        case .closeWorkspace: "close-workspace"
        case .removeWorkspace: "remove-workspace"
        case .moveWorkspaceToNewWindow: "move-workspace-to-new-window"
        case .switchWorktree: "switch-worktree"
        case .newWorktree: "new-worktree"
        case .removeWorktree: "remove-worktree"
        case .openDoc: "open-doc"
        }
    }
}

extension CommandPalette {
    public static func entries(commands: [Command], sections: [SectionInfo], sidebar: Bool, chat: Bool = false, workspaces: [PaletteWorkspace] = [], active: String? = nil, worktrees: [Worktree] = []) -> [PaletteEntry] {
        let commandEntries = commands.filter { !$0.hidden && $0.running }.map {
            PaletteEntry(id: PaletteAction.command($0.id).entryID, title: $0.title, detail: pluginName($0.plugin, sections: sections))
        }
        let sectionEntries = SectionHosting.rail(sections).enumerated().map { index, section in
            PaletteEntry(id: PaletteAction.section(section.id).entryID, title: "Go to \(section.title)", detail: index < 9 ? "⌘\(index + 1)" : "")
        }
        let sidebarEntries = sidebar ? [PaletteEntry(id: PaletteAction.toggleSidebar.entryID, title: chat ? "Toggle chat" : "Toggle sidebar", detail: "⌘B")] : []
        let docEntries = commands.contains { $0.id == DocsPicker.listCommand } ? [PaletteEntry(id: PaletteAction.openDoc.entryID, title: "Open doc…", detail: "⇧⌘D")] : []
        return commandEntries + sectionEntries + sidebarEntries + docEntries + workspaceEntries(workspaces, active: active) + worktreeEntries(worktrees)
    }

    private static func pluginName(_ plugin: String, sections: [SectionInfo]) -> String {
        sections.filter { $0.plugin == plugin }.min { $0.order < $1.order }?.title ?? plugin.prefix(1).uppercased() + plugin.dropFirst()
    }

    private static func worktreeEntries(_ worktrees: [Worktree]) -> [PaletteEntry] {
        guard !worktrees.isEmpty else { return [] }
        var entries = [
            PaletteEntry(id: PaletteAction.switchWorktree.entryID, title: "Switch worktree…", detail: ""),
            PaletteEntry(id: PaletteAction.newWorktree.entryID, title: "New worktree…", detail: ""),
        ]
        if worktrees.contains(where: { !$0.main }) {
            entries.append(PaletteEntry(id: PaletteAction.removeWorktree.entryID, title: "Remove worktree…", detail: ""))
        }
        return entries
    }

    private static func workspaceEntries(_ workspaces: [PaletteWorkspace], active: String?) -> [PaletteEntry] {
        var entries = [
            PaletteEntry(id: PaletteAction.openFolder.entryID, title: "Open folder…", detail: "⌘O"),
            PaletteEntry(id: PaletteAction.newHomeTerminal.entryID, title: "New home terminal", detail: "⇧⌘N"),
        ]
        guard let active, workspaces.contains(where: { $0.id == active }) else { return entries }
        entries += [
            PaletteEntry(id: PaletteAction.renameWorkspace.entryID, title: "Rename workspace…", detail: ""),
            PaletteEntry(id: PaletteAction.closeWorkspace.entryID, title: "Close workspace", detail: ""),
            PaletteEntry(id: PaletteAction.removeWorkspace.entryID, title: "Remove workspace…", detail: ""),
        ]
        if workspaces.count > 1 {
            entries.append(PaletteEntry(id: PaletteAction.moveWorkspaceToNewWindow.entryID, title: "Move workspace to new window", detail: ""))
        }
        return entries + workspaces.filter { $0.id != active }.map {
            PaletteEntry(id: PaletteAction.switchWorkspace($0.id).entryID, title: "Switch to \($0.name)", detail: "")
        }
    }
}

private extension String {
    func dropPrefix(_ prefix: String) -> String? {
        hasPrefix(prefix) ? String(dropFirst(prefix.count)) : nil
    }
}
