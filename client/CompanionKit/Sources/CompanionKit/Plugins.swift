import Foundation

public protocol CompanionPlugin {
    var id: String { get }
    var title: String { get }
    var systemImage: String { get }
    var requiredCommands: [String] { get }
}

public struct PluginRegistry {
    public let plugins: [any CompanionPlugin]

    public init(_ plugins: [any CompanionPlugin]) {
        self.plugins = plugins
    }

    public func supported(by hello: Hello?) -> [any CompanionPlugin] {
        guard let hello else { return [] }
        let commands = Set(hello.commands)
        return plugins.filter { commands.isSuperset(of: $0.requiredCommands) }
    }

    public func plugin(_ id: String) -> (any CompanionPlugin)? {
        plugins.first { $0.id == id }
    }
}

public enum AgentsPluginInfo {
    public static let id = "agents"
    public static let newSession = "new"
    public static let requiredCommands = [AgentCommand.sessions, AgentCommand.transcript, AgentCommand.prompt]
}

public enum BacklogPluginInfo {
    public static let id = "backlog"
    public static let newTask = "new"
    public static let requiredCommands = [BacklogCommand.list, BacklogCommand.add, BacklogCommand.update]
}

public enum DocsPluginInfo {
    public static let id = "docs"
    public static let requiredCommands = [DocsCommand.list, DocsCommand.read]
}

public enum InboxPluginInfo {
    public static let id = "inbox"
}
