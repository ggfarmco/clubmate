import Foundation

public enum UIAction {
    public enum Trigger: Sendable, Equatable {
        case click
        case select(String)
        case expand(String)
        case collapse(String)
        case submit(JSONValue)
        case toggle(Bool)
        case change(String)
        case sort(column: String, desc: Bool)
        case named(String, JSONValue)
        case permission(request: String, option: String)

        public var value: JSONValue {
            switch self {
            case .click: .null
            case .select(let id), .expand(let id), .collapse(let id): .string(id)
            case .submit(let submitted): submitted
            case .toggle(let on): .bool(on)
            case .change(let option): .string(option)
            case .sort(let column, let desc): SDUISort(column: column, desc: desc).value
            case .named(_, let value): value
            case .permission(let request, let option): .object(["request": .string(request), "option": .string(option)])
            }
        }

        public static func attachFile(_ path: String) -> Trigger {
            .named("attachFile", .object(["path": .string(path)]))
        }
    }

    public static func event(for node: SDUINode, trigger: Trigger, slot: String, workspaceID: String) -> UIEvent {
        let custom = node.string("action")
        let action: String
        switch trigger {
        case .click: action = custom ?? node.id ?? ""
        case .select, .change: action = custom ?? "select"
        case .expand: action = "expand"
        case .collapse: action = "collapse"
        case .submit: action = custom ?? "submit"
        case .toggle: action = custom ?? "toggle"
        case .sort: action = "sort"
        case .named(let name, _): action = name
        case .permission: action = custom ?? "permission"
        }
        return UIEvent(workspaceId: workspaceID, slot: slot, nodeId: node.id ?? "", action: action, value: trigger.value)
    }
}

public enum UIForm {
    public static let fieldTypes: Set<String> = ["textField", "toggle", "select", "choice"]

    public static func fieldIDs(in form: SDUINode) -> Set<String> {
        var ids: Set<String> = []
        func visit(_ children: [SDUINode]) {
            for child in children where child.type != "form" {
                if fieldTypes.contains(child.type), let id = child.id { ids.insert(id) }
                if child.type == "choice", let other = child.choiceOther() { ids.insert(other.id) }
                if child.type == "header", let field = child.string("field"), !field.isEmpty { ids.insert(field) }
                visit(child.children)
            }
        }
        visit(form.children)
        return ids
    }

    public static func questionIDs(in form: SDUINode) -> [String] {
        var ids: [String] = []
        func visit(_ children: [SDUINode]) {
            for child in children where child.type != "form" {
                if child.type == "choice", child.string("text") != nil, let id = child.id { ids.append(id) }
                visit(child.children)
            }
        }
        visit(form.children)
        return ids
    }

    public static func collect(_ values: [String: JSONValue], in form: SDUINode) -> JSONValue {
        let ids = fieldIDs(in: form)
        return .object(values.filter { ids.contains($0.key) })
    }
}

public struct UIFormSave: Sendable, Equatable {
    public static let verb = "Saving…"

    public private(set) var dirty = false
    private var inFlight = 0

    public init() {}

    public var saving: Bool { dirty || inFlight > 0 }

    public mutating func edited() {
        dirty = true
    }

    public mutating func sent() {
        dirty = false
        inFlight += 1
    }

    public mutating func answered() {
        inFlight = max(0, inFlight - 1)
    }
}
