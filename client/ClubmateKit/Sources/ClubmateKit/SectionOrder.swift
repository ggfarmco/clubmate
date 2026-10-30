import Foundation

public enum SectionOrder {
    public static func apply(_ order: [String], to defaults: [String]) -> [String] {
        let present = Set(defaults)
        return merge(order, defaults).filter { present.contains($0) }
    }

    public static func move(_ id: String, onto target: String, visible: [String], saved: [String]) -> [String]? {
        var merged = merge(saved, visible)
        let shown = Set(visible)
        var current = merged.filter { shown.contains($0) }
        guard id != target, let from = current.firstIndex(of: id), let to = current.firstIndex(of: target) else { return nil }
        current.remove(at: from)
        current.insert(id, at: to)
        let slots = merged.indices.filter { shown.contains(merged[$0]) }
        for (slot, id) in zip(slots, current) { merged[slot] = id }
        return merged
    }

    private static func merge(_ order: [String], _ defaults: [String]) -> [String] {
        var seen = Set<String>()
        var result = order.filter { seen.insert($0).inserted }
        for (index, id) in defaults.enumerated() where !seen.contains(id) {
            let at = index > 0 ? result.firstIndex(of: defaults[index - 1]).map { $0 + 1 } ?? 0 : 0
            result.insert(id, at: at)
            seen.insert(id)
        }
        return result
    }
}

extension AppSettings {
    public func ordered(_ sections: [SectionInfo]) -> [SectionInfo] {
        guard let sectionOrder else { return sections }
        let byID = Dictionary(sections.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return SectionOrder.apply(sectionOrder, to: sections.map(\.id)).compactMap { byID[$0] }
    }

    public func moveSection(_ id: String, onto target: String, in sections: [SectionInfo]) {
        guard let order = SectionOrder.move(id, onto: target, visible: sections.map(\.id), saved: sectionOrder ?? []) else { return }
        sectionOrder = order
    }
}
