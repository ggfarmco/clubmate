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
        let title = Self.clean(title)
        return "Allow agent to \(title.prefix(1).lowercased() + title.dropFirst())?"
    }

    public var detail: String {
        var lines = ["Workspace: \(workspace.isEmpty ? "none" : Self.clean(workspace))", "Command: \(Self.clean(command))"]
        switch args {
        case .null:
            break
        case .object(let fields):
            lines += fields.keys.sorted().map { "\(Self.clean($0)): \(fields[$0].map(Self.summary) ?? "")" }
        default:
            lines.append("Args (not an object): \(Self.summary(args))")
        }
        return lines.joined(separator: "\n")
    }

    static func clean(_ text: String) -> String {
        var out = ""
        for scalar in text.unicodeScalars {
            switch scalar {
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            case _ where [.control, .format, .lineSeparator, .paragraphSeparator].contains(scalar.properties.generalCategory):
                out += "\\u{\(String(scalar.value, radix: 16))}"
            default:
                out.unicodeScalars.append(scalar)
            }
        }
        return out
    }

    private static func summary(_ value: JSONValue) -> String {
        switch value {
        case .null: return "null"
        case .bool(let value): return String(value)
        case .number(let value): return value == value.rounded() && abs(value) < 1e15 ? String(Int64(value)) : String(value)
        case .string(let value): return clean(value)
        case .array, .object:
            guard let data = try? JSONEncoder().encode(value) else { return "" }
            return clean(String(decoding: data, as: UTF8.self))
        }
    }
}

struct ConfirmReply: Encodable, Sendable {
    let approved: Bool
}
