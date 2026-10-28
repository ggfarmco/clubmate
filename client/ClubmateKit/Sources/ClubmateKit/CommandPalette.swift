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
        guard !query.isEmpty else { return entries }
        let needle = query.map { String($0).lowercased() }
        return entries.enumerated()
            .compactMap { index, entry in score(entry.title, needle).map { (rank: $0, index: index, entry: entry) } }
            .sorted { ($0.rank, $0.index) < ($1.rank, $1.index) }
            .map(\.entry)
    }

    private static func score(_ title: String, _ needle: [String]) -> Int? {
        let characters = Array(title)
        let haystack = characters.map { String($0).lowercased() }
        guard isSubsequence(needle, of: haystack) else { return nil }
        if haystack.starts(with: needle) { return 0 }
        let starts = characters.indices.map { $0 == 0 || !isWordCharacter(characters[$0 - 1]) }
        return matchesWordStarts(needle, haystack, starts) ? 1 : 2
    }

    private static func isSubsequence(_ needle: [String], of haystack: [String]) -> Bool {
        var remaining = needle[...]
        for character in haystack where character == remaining.first {
            remaining = remaining.dropFirst()
            if remaining.isEmpty { return true }
        }
        return remaining.isEmpty
    }

    private static func matchesWordStarts(_ needle: [String], _ haystack: [String], _ starts: [Bool]) -> Bool {
        var memo: [[Bool?]] = Array(repeating: Array(repeating: nil, count: haystack.count + 1), count: needle.count)
        func match(_ next: Int, _ index: Int) -> Bool {
            guard next < needle.count else { return true }
            if let known = memo[next][index] { return known }
            let found = (index < haystack.count && haystack[index] == needle[next] && match(next + 1, index + 1))
                || (index..<haystack.count).contains { starts[$0] && haystack[$0] == needle[next] && match(next + 1, $0 + 1) }
            memo[next][index] = found
            return found
        }
        return match(0, 0)
    }

    private static func isWordCharacter(_ character: Character) -> Bool {
        character.isLetter || character.isNumber
    }
}

public enum PaletteAction: Hashable, Sendable {
    case command(String)
    case section(String)
    case toggleSidebar
    case toggleNavigationKeys
    case openProjects

    public init?(entryID: String) {
        if let id = entryID.dropPrefix("command:") {
            self = .command(id)
        } else if let id = entryID.dropPrefix("section:") {
            self = .section(id)
        } else if entryID == "toggle-sidebar" {
            self = .toggleSidebar
        } else if entryID == "toggle-navigation-keys" {
            self = .toggleNavigationKeys
        } else if entryID == "projects" {
            self = .openProjects
        } else {
            return nil
        }
    }

    public var entryID: String {
        switch self {
        case .command(let id): "command:\(id)"
        case .section(let id): "section:\(id)"
        case .toggleSidebar: "toggle-sidebar"
        case .toggleNavigationKeys: "toggle-navigation-keys"
        case .openProjects: "projects"
        }
    }
}

extension CommandPalette {
    public static func entries(commands: [Command], sections: [SectionInfo], sidebar: Bool) -> [PaletteEntry] {
        let commandEntries = commands.filter { !$0.hidden }.map {
            PaletteEntry(id: PaletteAction.command($0.id).entryID, title: $0.title, detail: $0.id)
        }
        let sectionEntries = sections.enumerated().map { index, section in
            PaletteEntry(id: PaletteAction.section(section.id).entryID, title: "Go to \(section.title)", detail: index < 9 ? "⌘\(index + 1)" : "")
        }
        let sidebarEntries = sidebar ? [PaletteEntry(id: PaletteAction.toggleSidebar.entryID, title: "Toggle Sidebar", detail: "⌘B")] : []
        let navigationEntries = sidebar && commands.contains { $0.id == CodeSection.navigationCommand }
            ? [PaletteEntry(id: PaletteAction.toggleNavigationKeys.entryID, title: "Toggle nvim Navigation Keys", detail: "")]
            : []
        return commandEntries + sectionEntries + sidebarEntries + navigationEntries + [PaletteEntry(id: PaletteAction.openProjects.entryID, title: "Open Projects", detail: "⌘⇧O")]
    }
}

private extension String {
    func dropPrefix(_ prefix: String) -> String? {
        hasPrefix(prefix) ? String(dropFirst(prefix.count)) : nil
    }
}
