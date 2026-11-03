import Foundation

public enum SDUITruncate: String, Sendable {
    case tail
    case middle
    case head
}

public struct SDUIItem: Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let detail: String?
    public let icon: String?
    public let badge: String?
    public let tone: String?
    public let menu: [SDUIMenuEntry]
    public let truncate: SDUITruncate

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        detail = fields["detail"]?.stringValue
        icon = fields["icon"]?.stringValue
        badge = fields["badge"]?.stringValue
        tone = fields["tone"]?.stringValue
        menu = fields["menu"]?.arrayValue?.compactMap(SDUIMenuEntry.init) ?? []
        truncate = fields["truncate"]?.stringValue.flatMap(SDUITruncate.init) ?? .tail
    }
}

public struct SDUIMenuEntry: Sendable, Equatable, Identifiable {
    public let action: String
    public let label: String
    public let confirm: String?
    public let key: String?

    public var id: String { action }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let action = fields["action"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.action = action
        self.label = label
        confirm = fields["confirm"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
        key = fields["key"]?.stringValue.flatMap { $0.count == 1 ? $0 : nil }
    }
}

public struct SDUITreeItem: Sendable, Equatable, Identifiable {
    public let item: SDUIItem
    public let tone: String?
    public let detailTone: String?
    public let muted: Bool
    public let faint: Bool
    public let hasChildren: Bool
    public let expanded: Bool
    public let children: [SDUITreeItem]
    public let badges: [SDUIHeaderBadge]
    public let changes: SDUIChanges?

    public var id: String { item.id }

    public var visibleItems: [SDUIItem] {
        [item] + (expanded ? children.flatMap(\.visibleItems) : [])
    }

    public init?(_ value: JSONValue) {
        guard let item = SDUIItem(value), let fields = value.objectValue else { return nil }
        self.item = item
        tone = fields["tone"]?.stringValue
        detailTone = fields["detailTone"]?.stringValue
        muted = fields["muted"]?.boolValue ?? false
        faint = fields["faint"]?.boolValue ?? false
        hasChildren = fields["hasChildren"]?.boolValue ?? false
        expanded = fields["expanded"]?.boolValue ?? false
        children = fields["children"]?.arrayValue?.compactMap(SDUITreeItem.init) ?? []
        badges = fields["badges"]?.arrayValue?.compactMap(SDUIHeaderBadge.init) ?? []
        changes = fields["changes"].flatMap(SDUIChanges.init)
    }
}

public struct SDUIChanges: Sendable, Equatable {
    public let added: Int
    public let removed: Int

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let added = fields["added"]?.numberValue, let removed = fields["removed"]?.numberValue else { return nil }
        self.added = Int(added)
        self.removed = Int(removed)
    }
}

public struct SDUIHeaderBadge: Sendable, Equatable {
    public let text: String
    public let tone: String?

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let text = fields["text"]?.stringValue else { return nil }
        self.text = text
        tone = fields["tone"]?.stringValue
    }
}

public struct SDUIOption: Sendable, Equatable, Identifiable {
    public let value: String
    public let label: String

    public var id: String { value }

    public init?(_ json: JSONValue) {
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

    public static let minFlexibleWidth: Double = 200

    public let id: String
    public let title: String
    public let align: Align
    public let width: Double?
    public let truncate: SDUITruncate
    public let priority: Int

    public init(id: String, title: String, align: Align, width: Double?, truncate: SDUITruncate = .tail, priority: Int = 0) {
        self.id = id
        self.title = title
        self.align = align
        self.width = width
        self.truncate = truncate
        self.priority = priority
    }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let title = fields["title"]?.stringValue else { return nil }
        let width = fields["width"]?.numberValue ?? 0
        self.init(
            id: id,
            title: title,
            align: fields["align"]?.stringValue.flatMap(Align.init) ?? .left,
            width: width > 0 ? width : nil,
            truncate: fields["truncate"]?.stringValue.flatMap(SDUITruncate.init) ?? .tail,
            priority: max(Int(fields["priority"]?.numberValue ?? 0), 0)
        )
    }

    public static func visible(_ columns: [SDUIColumn], width: Double, gutter: Double) -> [Int] {
        var shown = Array(columns.indices)
        guard width > 0 else { return shown }
        func needed() -> Double {
            let spacing = gutter * Double(max(shown.count - 1, 0))
            return shown.reduce(spacing) { $0 + (columns[$1].width ?? minFlexibleWidth) }
        }
        while needed() > width, let drop = shown.filter({ columns[$0].priority > 0 }).max(by: { columns[$0].priority < columns[$1].priority }) {
            shown.removeAll { $0 == drop }
        }
        return shown
    }
}

public struct SDUISpark: Sendable, Equatable {
    public let values: [Double]
    public let max: Double

    public init(values: [Double], max: Double) {
        self.values = values
        self.max = max
    }

    public init?(values: JSONValue?, max: JSONValue?) {
        guard let max = max?.numberValue, max > 0 else { return nil }
        self.init(values: values?.arrayValue?.compactMap(\.numberValue) ?? [], max: max)
    }
}

public struct SDUICell: Sendable, Equatable {
    public struct Link: Sendable, Equatable {
        public let action: String
        public let label: String

        public init(action: String, label: String) {
            self.action = action
            self.label = label
        }
    }

    public let text: String
    public let bar: Double?
    public let tone: String?
    public let spark: SDUISpark?
    public let since: Date?
    public let link: Link?

    public init(text: String, bar: Double?, tone: String?, spark: SDUISpark?, since: Date? = nil, link: Link? = nil) {
        self.text = text
        self.bar = bar
        self.tone = tone
        self.spark = spark
        self.since = since
        self.link = link
    }

    public static func elapsed(since start: Date, now: Date) -> String {
        let interval = now.timeIntervalSince(start)
        let seconds = interval > 0 ? Int(Swift.min(interval, 1e10)) : 0
        let two = { (v: Int) in v < 10 ? "0\(v)" : "\(v)" }
        return "\(seconds / 3600):\(two(seconds / 60 % 60)):\(two(seconds % 60))"
    }

    public init(_ value: JSONValue) {
        let fields = value.objectValue ?? [:]
        let spark = fields["spark"]?.objectValue
        let tone = fields["tone"]?.stringValue
        self.init(
            text: fields["text"]?.stringValue ?? "",
            bar: fields["bar"]?.numberValue.map { Swift.min(Swift.max($0, 0), 1) },
            tone: tone?.isEmpty == false ? tone : nil,
            spark: spark.flatMap { SDUISpark(values: $0["values"], max: $0["max"]) },
            since: fields["since"]?.numberValue.flatMap { $0 > 0 ? Date(timeIntervalSince1970: $0) : nil },
            link: fields["action"]?.stringValue.flatMap { action in
                guard let label = fields["label"]?.stringValue, !action.isEmpty, !label.isEmpty else { return nil }
                return Link(action: action, label: label)
            }
        )
    }
}

public struct SDUITableRow: Sendable, Equatable, Identifiable {
    public let id: String
    public let depth: Int
    public let hasChildren: Bool
    public let expanded: Bool
    public let muted: Bool
    public let group: Bool
    public let cells: [SDUICell]

    public init(id: String, depth: Int, hasChildren: Bool, expanded: Bool, muted: Bool, group: Bool = false, cells: [SDUICell]) {
        self.id = id
        self.depth = depth
        self.hasChildren = hasChildren
        self.expanded = expanded
        self.muted = muted
        self.group = group
        self.cells = cells
    }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue else { return nil }
        self.init(
            id: id,
            depth: fields["depth"]?.numberValue.map { Swift.max(Int($0), 0) } ?? 0,
            hasChildren: fields["hasChildren"]?.boolValue ?? false,
            expanded: fields["expanded"]?.boolValue ?? false,
            muted: fields["muted"]?.boolValue ?? false,
            group: fields["group"]?.boolValue ?? false,
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

    public var docLinks: [String: String] {
        props["docLinks"]?.objectValue?.compactMapValues(\.stringValue) ?? [:]
    }

    public var docCards: [DocCard] {
        DocCard.list(props["docCards"])
    }

    public var images: [ImageRef] {
        ImageRef.list(props["images"])
    }

    public func items() -> [SDUIItem] {
        props["items"]?.arrayValue?.compactMap(SDUIItem.init) ?? []
    }

    public func treeItems() -> [SDUITreeItem] {
        props["items"]?.arrayValue?.compactMap(SDUITreeItem.init) ?? []
    }

    public func headerBadges() -> [SDUIHeaderBadge] {
        props["badges"]?.arrayValue?.compactMap(SDUIHeaderBadge.init) ?? []
    }

    public var headerField: SDUIChild? {
        identifiedChildren.first { $0.node.type == "textField" }
    }

    public var headerButtons: [SDUIChild] {
        identifiedChildren.filter { $0.node.type != "textField" && $0.id != headerStop?.id }
    }

    public var headerStatusTone: String? {
        switch string("status") {
        case "running", "childrenRunning", "review": "accent"
        case "waiting", "asks", "childrenWaiting": "warning"
        case "error": "danger"
        default: nil
        }
    }

    public var headerStop: SDUIChild? {
        guard string("status") != nil else { return nil }
        return identifiedChildren.first { $0.node.type == "button" && $0.node.string("action") == "stop" }
    }

    public var headerBar: [SDUIChild] {
        headerButtons.filter { $0.node.bool("overflow") != true }
    }

    public func headerOverflow() -> (shown: [SDUIChild], more: [SDUIChild]) {
        let all = headerButtons
        let shown = Array(headerBar.filter { $0.node.string("variant") != "danger" }.prefix(3))
        let ids = Set(shown.map(\.id))
        return (shown, all.filter { !ids.contains($0.id) })
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
