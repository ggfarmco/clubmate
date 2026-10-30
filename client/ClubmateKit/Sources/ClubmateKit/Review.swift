import CoreGraphics
import Foundation

public struct ReviewChip: Equatable, Sendable, Identifiable {
    public static let glob = "glob"

    public let id: String
    public let label: String
    public let on: Bool

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        on = fields["on"]?.boolValue ?? false
    }
}

public struct ReviewHeader: Equatable, Sendable {
    public let base: String
    public let head: String
    public let baseRef: String
    public let headRef: String
    public let readOnly: Bool
    public let mode: String
    public let progress: String
    public let warning: String
    public let error: String
    public let chips: [ReviewChip]

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let base = fields["base"]?.stringValue, let head = fields["head"]?.stringValue else { return nil }
        self.base = base
        self.head = head
        baseRef = fields["baseRef"]?.stringValue ?? base
        headRef = fields["headRef"]?.stringValue ?? ""
        readOnly = fields["readOnly"]?.boolValue ?? false
        mode = fields["mode"]?.stringValue ?? ""
        progress = fields["progress"]?.stringValue ?? ""
        warning = fields["warning"]?.stringValue ?? ""
        error = fields["error"]?.stringValue ?? ""
        chips = fields["chips"]?.arrayValue?.compactMap(ReviewChip.init) ?? []
    }

    static let workingTree = "(working tree)"
    static let workingTreeShort = "(wt)"
    static let readOnlyTree = "(read-only)"
    static let readOnlyShort = "(ro)"

    public var headSuffix: String { readOnly ? Self.readOnlyTree : Self.workingTree }

    public var headSuffixShort: String { readOnly ? Self.readOnlyShort : Self.workingTreeShort }

    public var headLabel: String { "\(head) \(headSuffix)" }

    public var title: String { "\(base) ◂ \(headLabel)" }

    public var module: Bool { mode == "module" }

    public var fraction: Double? {
        guard let match = progress.firstMatch(of: /(\d+)\/(\d+) reviewed/), let done = Double(match.1), let total = Double(match.2), total > 0 else { return nil }
        return min(done / total, 1)
    }
}

extension SDUINode {
    public func reviewHeader() -> ReviewHeader? {
        props["review"].flatMap(ReviewHeader.init)
    }
}

public enum ReviewSlot {
    public static let slot = "git.review"

    public static func isActive(_ tree: SDUINode?) -> Bool {
        tree.map { $0.type != "empty" } ?? false
    }

    public static func header(_ tree: SDUINode?) -> ReviewHeader? {
        guard let tree else { return nil }
        return tree.reviewHeader() ?? tree.children.lazy.compactMap { header($0) }.first
    }
}

public enum ReviewCommand {
    public static let startCommand = "git.review"
    public static let filterCommand = "git.review-filter"
    public static let swap = CommandInvocation(id: "git.review-swap")
    public static let end = CommandInvocation(id: "git.end-review")

    public static func start(base: String? = nil, head: String? = nil) -> CommandInvocation {
        var args: [String: JSONValue] = [:]
        if let base, !base.isEmpty { args["base"] = .string(base) }
        if let head, !head.isEmpty { args["head"] = .string(head) }
        return CommandInvocation(id: startCommand, args: .object(args))
    }

    public static func filter(_ chip: ReviewChip) -> CommandInvocation {
        guard chip.id != ReviewChip.glob else { return glob("") }
        return CommandInvocation(id: filterCommand, args: .object([chip.id: .bool(!chip.on)]))
    }

    public static func glob(_ pattern: String) -> CommandInvocation {
        CommandInvocation(id: filterCommand, args: .object([ReviewChip.glob: .string(pattern)]))
    }
}

public enum ReviewKeys {
    public static func trigger(keyCode: UInt16, shift: Bool, control: Bool, command: Bool, option: Bool, cursor: String?, rows: [TreeRow]) -> UIAction.Trigger? {
        guard !control, !command, !option else { return nil }
        switch keyCode {
        case 15:
            guard !shift, let row = rows.first(where: { $0.id == cursor }), !row.hasChildren else { return nil }
            return .reviewed(row.id)
        case 45:
            return .next(shift ? -1 : 1)
        default:
            return nil
        }
    }
}

public enum FlowRows {
    public static func origins(sizes: [CGSize], width: CGFloat, spacing: CGFloat) -> [CGPoint] {
        var origins: [CGPoint] = []
        var x: CGFloat = 0
        var y: CGFloat = 0
        var rowHeight: CGFloat = 0
        for size in sizes {
            if x > 0, x + size.width > width {
                x = 0
                y += rowHeight + spacing
                rowHeight = 0
            }
            origins.append(CGPoint(x: x, y: y))
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
        return origins
    }

    public static func size(sizes: [CGSize], width: CGFloat, spacing: CGFloat) -> CGSize {
        let origins = origins(sizes: sizes, width: width, spacing: spacing)
        let frames = zip(origins, sizes).map { CGRect(origin: $0, size: $1) }
        return CGSize(width: frames.map(\.maxX).max() ?? 0, height: frames.map(\.maxY).max() ?? 0)
    }
}
