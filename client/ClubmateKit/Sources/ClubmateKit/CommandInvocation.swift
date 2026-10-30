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
        section?.autoNew == true && main?.type == "empty"
    }

    private static func resolve(_ verb: String, section: String?, commands: [Command]) -> String? {
        guard let section else { return nil }
        let id = "\(section).\(verb)"
        return commands.contains { $0.id == id } ? id : nil
    }
}

public struct AutoNewGate: Sendable {
    public struct Key: Equatable, Sendable {
        public let section: String?
        public let loaded: Bool
        public let request: Int

        public init(section: String?, loaded: Bool, request: Int) {
            self.section = section
            self.loaded = loaded
            self.request = request
        }
    }

    private var done: String?

    public init() {}

    public mutating func fires(from old: Key, to new: Key, section: SectionInfo?, main: SDUINode?) -> Bool {
        if old == new || old.section != new.section || old.request != new.request { done = nil }
        guard let selected = new.section, main != nil, done != selected else { return false }
        done = selected
        return CommandInvocation.shouldAutoNew(section: section, main: main)
    }
}

public enum TerminalsSection {
    public static let id = "terminals"
    public static let busyCommand = "terminals.busy"
    public static let closeCommand = "terminals.close"
    public static let sidebarSlot = "terminals.sidebar"

    public static func selectEvent(workspace: String, session: String) -> UIEvent {
        UIEvent(projectId: workspace, slot: sidebarSlot, nodeId: id, action: "select", value: .string(session))
    }

    public static func close(sidebar: SDUINode?, commands: [Command]) -> CommandInvocation? {
        guard let selected = sidebar?.node(type: "list", id: id)?.string("selected"), !selected.isEmpty,
              commands.contains(where: { $0.id == closeCommand }) else { return nil }
        return CommandInvocation(id: closeCommand, args: .object(["sessionId": .string(selected)]))
    }

    public static func busyMessage(_ result: JSONValue) -> String? {
        guard case .object(let fields) = result, fields["busy"] == .bool(true) else { return nil }
        return "“\(fields["process"]?.stringValue ?? "")” is running. Close the terminal?"
    }
}

extension SDUINode {
    func node(type: String, id: String) -> SDUINode? {
        if self.type == type, self.id == id { return self }
        return children.lazy.compactMap { $0.node(type: type, id: id) }.first
    }

    public var surfaceSessionID: String? {
        if type == "surface" { return string("sessionId") }
        return children.lazy.compactMap(\.surfaceSessionID).first
    }
}
