import Foundation

public struct SDUIItem: Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let detail: String?
    public let icon: String?
    public let badge: String?

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        detail = fields["detail"]?.stringValue
        icon = fields["icon"]?.stringValue
        badge = fields["badge"]?.stringValue
    }
}

public struct SDUITreeItem: Sendable, Equatable, Identifiable {
    public let item: SDUIItem
    public let hasChildren: Bool
    public let expanded: Bool
    public let children: [SDUITreeItem]

    public var id: String { item.id }

    init?(_ value: JSONValue) {
        guard let item = SDUIItem(value), let fields = value.objectValue else { return nil }
        self.item = item
        hasChildren = fields["hasChildren"]?.boolValue ?? false
        expanded = fields["expanded"]?.boolValue ?? false
        children = fields["children"]?.arrayValue?.compactMap(SDUITreeItem.init) ?? []
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

    public func options() -> [SDUIOption] {
        props["options"]?.arrayValue?.compactMap(SDUIOption.init) ?? []
    }

    public func values() -> [String] {
        props["values"]?.arrayValue?.compactMap(\.stringValue) ?? []
    }

    public func confirmation() -> String? {
        string("confirm").flatMap { $0.isEmpty ? nil : $0 }
    }
}
