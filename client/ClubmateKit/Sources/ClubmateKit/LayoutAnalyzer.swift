import Foundation

public enum LayoutAnalyzer {
    static let tolerance = 0.5
    static let overlap = 1.0
    static let order = ["outside-window", "wider-than-parent", "overlaps-sibling", "zero-size", "truncated"]

    public static func flag(_ elements: [LayoutElement], bounds: Rect) -> [LayoutElement] {
        let byID = Dictionary(elements.map { ($0.id, $0) }) { first, _ in first }
        let siblings = Dictionary(grouping: elements.filter { $0.source == "sdui" && $0.parent != nil }) { $0.parent ?? "" }
        return elements.map { element in
            var flags = Set<String>()
            let f = element.frame
            if f.x < bounds.x - tolerance || f.y < bounds.y - tolerance || f.maxX > bounds.maxX + tolerance || f.maxY > bounds.maxY + tolerance {
                flags.insert("outside-window")
            }
            if (f.width == 0 || f.height == 0) && !element.label.isEmpty {
                flags.insert("zero-size")
            }
            if element.source == "sdui" {
                if let parent = element.parent.flatMap({ byID[$0] }), f.width > parent.frame.width + tolerance {
                    flags.insert("wider-than-parent")
                }
                if let parent = element.parent, siblings[parent, default: []].contains(where: { $0.id != element.id && overlaps($0.frame, f) }) {
                    flags.insert("overlaps-sibling")
                }
                if let ideal = element.idealWidth, ideal > f.width + tolerance {
                    flags.insert("truncated")
                }
            }
            var flagged = element
            flagged.flags = order.filter(flags.contains)
            return flagged
        }
    }

    public static func summary(_ windows: [(title: String, elements: [LayoutElement])]) -> String {
        var lines: [String] = []
        for window in windows {
            let byID = Dictionary(window.elements.map { ($0.id, $0) }) { first, _ in first }
            for element in window.elements {
                for flag in element.flags {
                    let detail = detail(flag, element, parent: element.parent.flatMap { byID[$0] })
                    lines.append("\(window.title) \(element.source) \(element.id) \(flag)" + (detail.map { " (\($0))" } ?? ""))
                }
            }
        }
        guard !lines.isEmpty else { return "0 problems" }
        return "\(lines.count) problem\(lines.count == 1 ? "" : "s"):\n" + lines.joined(separator: "\n")
    }

    private static func overlaps(_ a: Rect, _ b: Rect) -> Bool {
        min(a.maxX, b.maxX) - max(a.x, b.x) > overlap && min(a.maxY, b.maxY) - max(a.y, b.y) > overlap
    }

    private static func detail(_ flag: String, _ element: LayoutElement, parent: LayoutElement?) -> String? {
        switch flag {
        case "wider-than-parent": parent.map { "\(number(element.frame.width)) > \(number($0.frame.width))" }
        case "truncated": element.idealWidth.map { "ideal \(number($0)) > \(number(element.frame.width))" }
        case "outside-window": "\(number(element.frame.x)),\(number(element.frame.y)) \(number(element.frame.width))x\(number(element.frame.height))"
        default: nil
        }
    }

    private static func number(_ value: Double) -> String {
        String(format: "%g", (value * 10).rounded() / 10)
    }
}
