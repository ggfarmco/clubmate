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
    }

    public static func event(for node: SDUINode, trigger: Trigger, slot: String, projectID: String) -> UIEvent {
        let custom = node.string("action")
        let action: String
        let value: JSONValue
        switch trigger {
        case .click:
            action = custom ?? node.id ?? ""
            value = .null
        case .select(let id):
            action = custom ?? "select"
            value = .string(id)
        case .expand(let id):
            action = "expand"
            value = .string(id)
        case .collapse(let id):
            action = "collapse"
            value = .string(id)
        case .submit(let submitted):
            action = custom ?? "submit"
            value = submitted
        case .toggle(let on):
            action = custom ?? "toggle"
            value = .bool(on)
        case .change(let option):
            action = custom ?? "select"
            value = .string(option)
        }
        return UIEvent(projectId: projectID, slot: slot, nodeId: node.id ?? "", action: action, value: value)
    }
}
