import ClubmateProtocol

public enum BrowserAction: String, Decodable, Sendable {
    case open, snapshot, click, type, select, press, scroll, back, screenshot, handoff
}

public struct BrowserCall: Decodable, Sendable {
    public let owner: String
    public let tab: String
    public let action: BrowserAction
    public let args: JSONValue
    public let timeout: Int?

    public var key: String { owner + "/" + tab }

    public init(owner: String, tab: String, action: BrowserAction, args: JSONValue = .object([:]), timeout: Int? = nil) {
        self.owner = owner
        self.tab = tab
        self.action = action
        self.args = args
        self.timeout = timeout
    }
}

public struct BrowserError: Error, Sendable {
    public let message: String

    public init(message: String) {
        self.message = message
    }
}

public struct PageSummary: Encodable, Sendable {
    public let url: String
    public let status: Int
    public let title: String
    public let blocked: Bool

    public init(url: String, status: Int, title: String, blocked: Bool) {
        self.url = url
        self.status = status
        self.title = title
        self.blocked = blocked
    }
}
