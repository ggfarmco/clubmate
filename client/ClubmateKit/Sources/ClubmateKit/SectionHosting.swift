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
