import Foundation

public enum PhoneSections {
    public static let native: Set<String> = [AgentsPluginInfo.id, BacklogPluginInfo.id, DocsPluginInfo.id, "terminals", "editor-nvim"]

    public static func more(_ sections: [SectionInfo]) -> [SectionInfo] {
        sections
            .filter { $0.scope == "workspace" && $0.host == nil && $0.rail && !native.contains($0.id) }
            .sorted { $0.order < $1.order }
    }

    public static func root(_ section: String, sidebar: SlotView?, main: SlotView?) -> String? {
        if let sidebar, !cleared(sidebar.tree) { return sidebar.slot }
        return main == nil ? nil : "\(section).main"
    }

    private static func cleared(_ tree: SDUINode) -> Bool {
        tree.type == "empty" && (tree.string("text") ?? "").isEmpty && tree.children.isEmpty
    }
}
