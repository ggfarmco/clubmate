import Foundation

public enum AgentFinishMode: String, Sendable, CaseIterable {
    case ask
    case merge

    public var label: String {
        switch self {
        case .ask: "Ask"
        case .merge: "Merge to main"
        }
    }
}
