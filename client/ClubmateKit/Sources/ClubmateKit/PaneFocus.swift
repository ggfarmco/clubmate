import Foundation
import Observation

public enum FocusedPane: Equatable, Sendable {
    case tree(field: Bool)
    case list
    case table
    case composer(running: Bool, queues: Bool)
    case field
    case terminal

    public var hints: String? {
        switch self {
        case .tree(let field): field ? "j/k · ↩ open · gc field" : "j/k · ↩ open"
        case .list: "j/k · ↩ open"
        case .table: "j/k · h/l fold"
        case .composer(let running, let queues): queues ? "⌘↩ queue · Esc stop" : running ? "⌘↩ send · Esc stop" : "⌘↩ send"
        case .field: "↩ submit"
        case .terminal: nil
        }
    }
}

@MainActor
@Observable
public final class PaneFocus {
    public private(set) var pane: FocusedPane?
    @ObservationIgnored private var owner: String?

    public init() {}

    public var hints: String? { pane?.hints }

    public func report(_ pane: FocusedPane, from id: String, focused: Bool) {
        if focused {
            owner = id
            if self.pane != pane { self.pane = pane }
        } else if owner == id {
            owner = nil
            self.pane = nil
        }
    }
}
