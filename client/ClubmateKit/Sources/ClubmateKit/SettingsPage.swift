import Foundation

public enum SettingsPage: Hashable, Sendable {
    case general
    case plugin(String)

    public init(argument: String) {
        switch argument {
        case "general": self = .general
        case "remote", "devices": self = .plugin("remote")
        default: self = .plugin(argument)
        }
    }

    public static func slot(plugin: String) -> String {
        "\(plugin).settings"
    }
}
