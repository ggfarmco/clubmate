public struct SectionTarget: Equatable, Sendable {
    public let section: String
    public let tab: String?
    public let slot: String

    public init(section: String, tab: String?, slot: String) {
        self.section = section
        self.tab = tab
        self.slot = slot
    }
}

public enum SectionHosting {
    public static func rail(_ sections: [SectionInfo]) -> [SectionInfo] {
        sections.filter { host(of: $0, in: sections) == nil }
    }

    public static func hosted(_ sections: [SectionInfo]) -> [SectionInfo] {
        sections.filter { host(of: $0, in: sections) != nil }
    }

    public static func resolve(_ id: String, slot: String, in sections: [SectionInfo]) -> SectionTarget? {
        guard let section = sections.first(where: { $0.id == id }) else { return nil }
        guard let host = host(of: section, in: sections) else { return SectionTarget(section: id, tab: nil, slot: slot) }
        let suffix = slot.hasPrefix("\(id).") ? String(slot.dropFirst(id.count + 1)) : ""
        if section.slots.contains(suffix) { return SectionTarget(section: host, tab: slot, slot: "\(host).sidebar") }
        let mapped = suffix == "sidebar" || suffix == "main" ? "\(host).\(suffix)" : slot
        return SectionTarget(section: host, tab: "\(id).sidebar", slot: mapped)
    }

    private static func host(of section: SectionInfo, in sections: [SectionInfo]) -> String? {
        guard let host = section.host, host != section.id, sections.contains(where: { $0.id == host && $0.host == nil }) else { return nil }
        return host
    }
}

extension SDUINode {
    public var hasTextField: Bool {
        type == "textField" || children.contains { $0.hasTextField }
    }
}

public struct SidebarTab: Equatable, Sendable, Identifiable {
    public let slot: String
    public let title: String

    public var id: String { slot }
}

public struct SidebarTabs: Equatable, Sendable {
    private var choice: [String: String] = [:]
    private var started: Int?

    public init() {}

    public static func tabs(section: String, title: String, hosted: [SectionInfo], review: Bool) -> [SidebarTab] {
        let own = SidebarTab(slot: "\(section).sidebar", title: section == CodeSection.id ? "Files" : title)
        let guests = hosted.filter { $0.host == section }.map { SidebarTab(slot: "\($0.id).sidebar", title: $0.title) }
        let reviewTab = review && section == CodeSection.id ? [SidebarTab(slot: ReviewSlot.slot, title: "Review")] : []
        return [own] + guests + reviewTab
    }

    public func current(_ section: String, in tabs: [SidebarTab]) -> SidebarTab? {
        tabs.first { $0.slot == choice[section] } ?? tabs.first
    }

    public mutating func choose(_ slot: String, in section: String) {
        choice[section] = slot
    }

    public mutating func reviewSeen(_ tree: SDUINode) {
        let value = ReviewSlot.header(tree)?.started ?? 0
        if let started, value > started { choice[CodeSection.id] = ReviewSlot.slot }
        started = max(started ?? value, value)
    }

    public mutating func cycle(_ section: String, in tabs: [SidebarTab], by delta: Int) -> SidebarTab? {
        guard let current = current(section, in: tabs), let index = tabs.firstIndex(of: current) else { return nil }
        let next = tabs[(index + delta % tabs.count + tabs.count) % tabs.count]
        choice[section] = next.slot
        return next
    }
}
