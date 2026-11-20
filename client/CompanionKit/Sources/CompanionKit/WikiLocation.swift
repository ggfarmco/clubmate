import Foundation

public enum WikiCommand {
    public static let locate = "wiki.locate"
}

public struct WikiLocation: Decodable, Sendable {
    public let id: String?
    public let path: String?
    public let title: String?
    public let anchor: String?
    public let history: Bool?

    public init(id: String? = nil, path: String? = nil, title: String? = nil, anchor: String? = nil, history: Bool? = nil) {
        self.id = id
        self.path = path
        self.title = title
        self.anchor = anchor
        self.history = history
    }
}
