import Foundation

public struct CommandInvocation: Sendable, Equatable {
    public let id: String
    public let args: JSONValue

    public init(id: String, args: JSONValue = .null) {
        self.id = id
        self.args = args
    }

    public static func navigation(enabled: Bool, side: SidebarSide) -> CommandInvocation {
        CommandInvocation(id: CodeSection.navigationCommand, args: .object(["enabled": .bool(enabled), "side": .string(side.rawValue)]))
    }

    public static func new(section: String?, commands: [Command]) -> CommandInvocation? {
        resolve("new", section: section, commands: commands).map { CommandInvocation(id: $0) }
    }

    public static func close(section: String?, main: SDUINode?, commands: [Command]) -> CommandInvocation? {
        guard let session = main?.surfaceSessionID, let id = resolve("close", section: section, commands: commands) else { return nil }
        return CommandInvocation(id: id, args: .object(["sessionId": .string(session)]))
    }

    public static func shouldAutoNew(section: SectionInfo?, main: SDUINode?) -> Bool {
        guard section?.autoNew == true, let main else { return false }
        return main.surfaceSessionID == nil
    }

    private static func resolve(_ verb: String, section: String?, commands: [Command]) -> String? {
        guard let section else { return nil }
        let id = "\(section).\(verb)"
        return commands.contains { $0.id == id } ? id : nil
    }
}

public enum TerminalsSection {
    public static let id = "terminals"
    public static let busyCommand = "terminals.busy"

    public static func busyMessage(_ result: JSONValue) -> String? {
        guard case .object(let fields) = result, fields["busy"] == .bool(true) else { return nil }
        return "“\(fields["process"]?.stringValue ?? "")” is running. Close the terminal?"
    }
}

extension SDUINode {
    public var surfaceSessionID: String? {
        if type == "surface" { return string("sessionId") }
        return children.lazy.compactMap(\.surfaceSessionID).first
    }
}
