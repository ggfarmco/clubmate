public enum Topic {
    public static let notifyPosted = "notify.posted"
    public static let agentsChanged = "agents.changed"
    public static let pluginChanged = "plugin.changed"
}

public enum AgentCommand {
    public static let sessions = "agents.sessions"
    public static let transcript = "agents.transcript"
    public static let prompt = "agents.prompt"
}

public enum AgentSessionID {
    public static let prefix = "agt_"

    public static func matches(_ id: String) -> Bool {
        id.hasPrefix(prefix)
    }
}
