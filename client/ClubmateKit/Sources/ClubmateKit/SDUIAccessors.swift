import Foundation

public struct SDUIItem: Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let detail: String?
    public let icon: String?
    public let badge: String?
    public let menu: [SDUIMenuEntry]

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        detail = fields["detail"]?.stringValue
        icon = fields["icon"]?.stringValue
        badge = fields["badge"]?.stringValue
        menu = fields["menu"]?.arrayValue?.compactMap(SDUIMenuEntry.init) ?? []
    }
}

public struct SDUIMenuEntry: Sendable, Equatable, Identifiable {
    public let action: String
    public let label: String
    public let confirm: String?

    public var id: String { action }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let action = fields["action"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.action = action
        self.label = label
        confirm = fields["confirm"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
    }
}

public struct SDUITreeItem: Sendable, Equatable, Identifiable {
    public let item: SDUIItem
    public let muted: Bool
    public let hasChildren: Bool
    public let expanded: Bool
    public let children: [SDUITreeItem]
    public let additions: Int?
    public let deletions: Int?
    public let comments: Int
    public let reviewed: Bool

    public var id: String { item.id }

    init?(_ value: JSONValue) {
        guard let item = SDUIItem(value), let fields = value.objectValue else { return nil }
        self.item = item
        muted = fields["muted"]?.boolValue ?? false
        hasChildren = fields["hasChildren"]?.boolValue ?? false
        expanded = fields["expanded"]?.boolValue ?? false
        children = fields["children"]?.arrayValue?.compactMap(SDUITreeItem.init) ?? []
        additions = fields["additions"]?.numberValue.map(Int.init)
        deletions = fields["deletions"]?.numberValue.map(Int.init)
        comments = fields["comments"]?.numberValue.map(Int.init) ?? 0
        reviewed = fields["reviewed"]?.boolValue ?? false
    }
}

public struct SDUIOption: Sendable, Equatable, Identifiable {
    public let value: String
    public let label: String

    public var id: String { value }

    init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let value = fields["value"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.value = value
        self.label = label
    }
}

public struct SDUIColumn: Sendable, Equatable, Identifiable {
    public enum Align: String, Sendable {
        case left
        case right
    }

    public let id: String
    public let title: String
    public let align: Align
    public let width: Double?

    public init(id: String, title: String, align: Align, width: Double?) {
        self.id = id
        self.title = title
        self.align = align
        self.width = width
    }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let title = fields["title"]?.stringValue else { return nil }
        let width = fields["width"]?.numberValue ?? 0
        self.init(id: id, title: title, align: fields["align"]?.stringValue.flatMap(Align.init) ?? .left, width: width > 0 ? width : nil)
    }
}

public struct SDUISpark: Sendable, Equatable {
    public let values: [Double]
    public let max: Double

    public init(values: [Double], max: Double) {
        self.values = values
        self.max = max
    }

    init?(values: JSONValue?, max: JSONValue?) {
        guard let max = max?.numberValue, max > 0 else { return nil }
        self.init(values: values?.arrayValue?.compactMap(\.numberValue) ?? [], max: max)
    }
}

public struct SDUICell: Sendable, Equatable {
    public let text: String
    public let bar: Double?
    public let tone: String?
    public let spark: SDUISpark?

    public init(text: String, bar: Double?, tone: String?, spark: SDUISpark?) {
        self.text = text
        self.bar = bar
        self.tone = tone
        self.spark = spark
    }

    init(_ value: JSONValue) {
        let fields = value.objectValue ?? [:]
        let spark = fields["spark"]?.objectValue
        let tone = fields["tone"]?.stringValue
        self.init(
            text: fields["text"]?.stringValue ?? "",
            bar: fields["bar"]?.numberValue.map { Swift.min(Swift.max($0, 0), 1) },
            tone: tone?.isEmpty == false ? tone : nil,
            spark: spark.flatMap { SDUISpark(values: $0["values"], max: $0["max"]) }
        )
    }
}

public struct SDUITableRow: Sendable, Equatable, Identifiable {
    public let id: String
    public let depth: Int
    public let hasChildren: Bool
    public let expanded: Bool
    public let muted: Bool
    public let cells: [SDUICell]

    public init(id: String, depth: Int, hasChildren: Bool, expanded: Bool, muted: Bool, cells: [SDUICell]) {
        self.id = id
        self.depth = depth
        self.hasChildren = hasChildren
        self.expanded = expanded
        self.muted = muted
        self.cells = cells
    }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue else { return nil }
        self.init(
            id: id,
            depth: fields["depth"]?.numberValue.map { Swift.max(Int($0), 0) } ?? 0,
            hasChildren: fields["hasChildren"]?.boolValue ?? false,
            expanded: fields["expanded"]?.boolValue ?? false,
            muted: fields["muted"]?.boolValue ?? false,
            cells: fields["cells"]?.arrayValue?.map(SDUICell.init) ?? []
        )
    }
}

public struct SDUISort: Sendable, Equatable {
    public let column: String
    public let desc: Bool

    public init(column: String, desc: Bool) {
        self.column = column
        self.desc = desc
    }

    public var value: JSONValue {
        .object(["column": .string(column), "desc": .bool(desc)])
    }

    public static func next(clicking column: SDUIColumn, current: SDUISort?) -> SDUISort {
        if let current, current.column == column.id {
            return SDUISort(column: column.id, desc: !current.desc)
        }
        return SDUISort(column: column.id, desc: column.align == .right)
    }
}

public struct SDUIChild: Sendable, Equatable, Identifiable {
    public let id: String
    public let node: SDUINode
}

extension SDUINode {
    public var identifiedChildren: [SDUIChild] {
        children.enumerated().map { index, child in
            SDUIChild(id: child.id ?? "\(child.type)@\(index)", node: child)
        }
    }

    public func string(_ key: String) -> String? {
        props[key]?.stringValue
    }

    public func bool(_ key: String) -> Bool? {
        props[key]?.boolValue
    }

    public func items() -> [SDUIItem] {
        props["items"]?.arrayValue?.compactMap(SDUIItem.init) ?? []
    }

    public func treeItems() -> [SDUITreeItem] {
        props["items"]?.arrayValue?.compactMap(SDUITreeItem.init) ?? []
    }

    public func breadcrumb() -> [SDUIItem] {
        props["breadcrumb"]?.arrayValue?.compactMap(SDUIItem.init) ?? []
    }

    public func options() -> [SDUIOption] {
        props["options"]?.arrayValue?.compactMap(SDUIOption.init) ?? []
    }

    public func columns() -> [SDUIColumn] {
        props["columns"]?.arrayValue?.compactMap(SDUIColumn.init) ?? []
    }

    public func tableRows() -> [SDUITableRow] {
        props["rows"]?.arrayValue?.compactMap(SDUITableRow.init) ?? []
    }

    public func tableSort() -> SDUISort? {
        guard let fields = props["sort"]?.objectValue, let column = fields["column"]?.stringValue, !column.isEmpty else { return nil }
        return SDUISort(column: column, desc: fields["desc"]?.boolValue ?? false)
    }

    public func spark() -> SDUISpark? {
        SDUISpark(values: props["values"], max: props["max"])
    }

    public func sparklineHeight() -> Double? {
        props["height"]?.numberValue.flatMap { $0 > 0 ? $0 : nil }
    }

    public func values() -> [String] {
        props["values"]?.arrayValue?.compactMap(\.stringValue) ?? []
    }

    public func diffText() -> String? {
        type == "diff" ? string("text") : nil
    }

    public func confirmation() -> String? {
        string("confirm").flatMap { $0.isEmpty ? nil : $0 }
    }

    public func confirmActionTitle() -> String {
        let label = string("label") ?? ""
        let title = label.hasSuffix("…") ? String(label.dropLast()) : label
        return title.isEmpty ? "OK" : title
    }
}
