public enum Topic {
    public static let notifyPosted = "notify.posted"
    public static let agentsChanged = "agents.changed"
    public static let backlogChanged = "backlog.changed"
    public static let docsChanged = "docs.changed"
    public static let docsOpened = "docs.opened"
    public static let sessionRemoved = "session.removed"
    public static let pluginChanged = "plugin.changed"
}

public enum AgentCommand {
    public static let sessions = "agents.sessions"
    public static let transcript = "agents.transcript"
    public static let prompt = "agents.prompt"
}

public enum BacklogCommand {
    public static let list = "backlog.list"
    public static let get = "backlog.get"
    public static let add = "backlog.add"
    public static let update = "backlog.update"
    public static let claim = "backlog.claim"
    public static let release = "backlog.release"
    public static let done = "backlog.done"
}

public enum DocsCommand {
    public static let read = "docs.read"
    public static let list = "docs.list"
}

public enum AgentSessionID {
    public static let prefix = "agt_"

    public static func matches(_ id: String) -> Bool {
        id.hasPrefix(prefix)
    }
}
