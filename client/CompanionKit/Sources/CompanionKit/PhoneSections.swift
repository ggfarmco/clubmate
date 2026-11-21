import Foundation

public enum PhoneSections {
    public static let native: Set<String> = [AgentsPluginInfo.id, BacklogPluginInfo.id, DocsPluginInfo.id, WikiPluginInfo.id, "terminals", "editor-nvim", "editor-vscode"]

    public static func more(_ sections: [SectionInfo]) -> [SectionInfo] {
        sections
            .filter { $0.scope == "workspace" && $0.host == nil && $0.rail && !native.contains($0.id) }
            .sorted { $0.order < $1.order }
    }

    public static func root(_ section: String, sidebar: SlotView?, main: SlotView?) -> String? {
        if let sidebar, !cleared(sidebar.tree) { return sidebar.slot }
        return main == nil ? nil : "\(section).main"
    }

    public static func shows(_ main: SDUINode, page id: String, path: String) -> Bool {
        guard let shown = document(in: main)?.string("path") else { return true }
        if !path.isEmpty { return shown == path || shown.hasPrefix(path + "@") }
        let page = id.split(separator: "/", maxSplits: 1).last.map(String.init) ?? id
        return shown.hasSuffix("/" + page) || shown.contains("/" + page + "@")
    }

    public static func list(in node: SDUINode) -> SDUINode? {
        if node.type == "list" || node.type == "tree", node.id != nil { return node }
        return node.children.lazy.compactMap(list(in:)).first
    }

    public static func selected(in view: SlotView?) -> String? {
        view.flatMap { list(in: $0.tree) }.flatMap { $0.string("selected") }
    }

    static func document(in node: SDUINode) -> SDUINode? {
        if node.type == "document" { return node }
        return node.children.lazy.compactMap(document(in:)).first
    }

    private static func cleared(_ tree: SDUINode) -> Bool {
        tree.type == "empty" && (tree.string("text") ?? "").isEmpty && tree.children.isEmpty
    }
}
