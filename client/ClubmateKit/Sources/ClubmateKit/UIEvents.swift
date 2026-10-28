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
        case pick([String])
        case sort(column: String, desc: Bool)

        public var value: JSONValue {
            switch self {
            case .click: .null
            case .select(let id), .expand(let id), .collapse(let id): .string(id)
            case .submit(let submitted): submitted
            case .toggle(let on): .bool(on)
            case .change(let option): .string(option)
            case .pick(let paths): .array(paths.map(JSONValue.string))
            case .sort(let column, let desc): SDUISort(column: column, desc: desc).value
            }
        }
    }

    public static func event(for node: SDUINode, trigger: Trigger, slot: String, projectID: String) -> UIEvent {
        let custom = node.string("action")
        let action: String
        switch trigger {
        case .click: action = custom ?? node.id ?? ""
        case .select, .change: action = custom ?? "select"
        case .expand: action = "expand"
        case .collapse: action = "collapse"
        case .submit: action = custom ?? "submit"
        case .toggle: action = custom ?? "toggle"
        case .pick: action = custom ?? "pick"
        case .sort: action = "sort"
        }
        return UIEvent(projectId: projectID, slot: slot, nodeId: node.id ?? "", action: action, value: trigger.value)
    }
}

public enum UIForm {
    public static let fieldTypes: Set<String> = ["textField", "folderField", "toggle", "select"]

    public static func fieldIDs(in form: SDUINode) -> Set<String> {
        var ids: Set<String> = []
        func visit(_ children: [SDUINode]) {
            for child in children where child.type != "form" {
                if fieldTypes.contains(child.type), let id = child.id { ids.insert(id) }
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
