import Foundation

public enum SectionOrder {
    public static func apply(_ order: [String], to defaults: [String]) -> [String] {
        let present = Set(defaults)
        var seen = Set<String>()
        var result = order.filter { present.contains($0) && seen.insert($0).inserted }
        for (index, id) in defaults.enumerated() where !seen.contains(id) {
            let anchor = defaults[..<index].last { result.contains($0) }
            let at = anchor.flatMap { result.firstIndex(of: $0) }.map { $0 + 1 } ?? 0
            result.insert(id, at: at)
            seen.insert(id)
        }
        return result
    }

    public static func move(_ id: String, onto target: String, visible: [String], saved: [String]) -> [String]? {
        guard id != target, let from = visible.firstIndex(of: id), let to = visible.firstIndex(of: target) else { return nil }
        var moved = visible
        moved.remove(at: from)
        moved.insert(id, at: to)
        return moved + saved.filter { !visible.contains($0) }
    }
}

extension AppSettings {
    public func ordered(_ sections: [SectionInfo]) -> [SectionInfo] {
        guard let sectionOrder else { return sections }
        let byID = Dictionary(sections.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return SectionOrder.apply(sectionOrder, to: sections.map(\.id)).compactMap { byID[$0] }
    }

    public func moveSection(_ id: String, onto target: String, in sections: [SectionInfo]) {
        guard let order = SectionOrder.move(id, onto: target, visible: ordered(sections).map(\.id), saved: sectionOrder ?? []) else { return }
        sectionOrder = order
    }
}
