import Foundation

import ClubmateProtocol

public enum AgentsPane: String, Sendable {
    case list
    case transcript
    case composer
}

public enum PaneArrow: Sendable {
    case left
    case right
    case up
    case down
}

public struct PaneFacts: Equatable, Sendable {
    public var hasMessages: Bool
    public var canType: Bool
    public var listReachable: Bool
    public var turnRunning: Bool

    public init(hasMessages: Bool, canType: Bool = true, listReachable: Bool, turnRunning: Bool = false) {
        self.hasMessages = hasMessages
        self.canType = canType
        self.listReachable = listReachable
        self.turnRunning = turnRunning
    }
}

public struct PaneStep: Equatable, Sendable {
    public let pane: AgentsPane
    public let leftRunningPrompt: Bool
}

public enum PaneLadder {
    public static let controlKeys: [String: PaneArrow] = ["j": .down, "k": .up]

    public static func escape(from pane: AgentsPane?, _ facts: PaneFacts) -> PaneStep {
        let next: AgentsPane = switch pane {
        case .composer: facts.hasMessages ? .transcript : outOfSession(facts)
        case .transcript: outOfSession(facts)
        case .list, nil: intoSession(facts)
        }
        return PaneStep(pane: next, leftRunningPrompt: pane == .composer && next != .composer && facts.turnRunning)
    }

    public static func arrow(_ arrow: PaneArrow, _ facts: PaneFacts) -> AgentsPane? {
        switch arrow {
        case .left: facts.listReachable ? .list : nil
        case .right: intoSession(facts) == .list ? nil : intoSession(facts)
        case .up: facts.hasMessages ? .transcript : nil
        case .down: facts.canType ? .composer : nil
        }
    }

    private static func outOfSession(_ facts: PaneFacts) -> AgentsPane {
        facts.listReachable ? .list : intoSession(facts)
    }

    private static func intoSession(_ facts: PaneFacts) -> AgentsPane {
        if facts.canType { return .composer }
        return facts.hasMessages ? .transcript : .list
    }
}

public enum StopHint {
    public static let text = StopNote.text
    public static let limit = StopNote.shows
    public static let seconds = Double(StopNote.seconds)
    public static let defaultsKey = "agents.stopHintShown"

    public static func take(from defaults: UserDefaults, key: String = defaultsKey) -> Bool {
        let shown = defaults.integer(forKey: key)
        guard shown < limit else { return false }
        defaults.set(shown + 1, forKey: key)
        return true
    }
}
