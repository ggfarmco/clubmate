import Foundation

public struct ConfirmRequest: Decodable, Sendable, Equatable {
    public let title: String
    public let workspace: String
    public let command: String
    public let args: JSONValue

    public init(title: String, workspace: String, command: String, args: JSONValue) {
        self.title = title
        self.workspace = workspace
        self.command = command
        self.args = args
    }

    public var message: String {
        "Allow agent to \(title.prefix(1).lowercased() + title.dropFirst())?"
    }

    public var detail: String {
        var lines = ["Workspace: \(workspace.isEmpty ? "none" : workspace)", "Command: \(command)"]
        if let fields = args.objectValue {
            lines += fields.keys.sorted().map { "\($0): \(fields[$0].map(Self.summary) ?? "")" }
        }
        return lines.joined(separator: "\n")
    }

    private static func summary(_ value: JSONValue) -> String {
        switch value {
        case .null: return "null"
        case .bool(let value): return String(value)
        case .number(let value): return value == value.rounded() && abs(value) < 1e15 ? String(Int64(value)) : String(value)
        case .string(let value): return value
        case .array, .object:
            guard let data = try? JSONEncoder().encode(value) else { return "" }
            return String(decoding: data, as: UTF8.self)
        }
    }
}

struct ConfirmReply: Encodable, Sendable {
    let approved: Bool
}
