import Foundation
import Observation

public enum FocusedPane: Equatable, Sendable {
    case tree(field: Bool)
    case list
    case table
    case graph(history: Bool, edges: Bool = false, output: Bool = false)
    case lanes(title: Bool)
    case runstrip(title: Bool)
    case composer(running: Bool, queues: Bool)
    case field
    case terminal

    public var hints: String? {
        switch self {
        case .tree(let field): field ? "j/k · ↩ open · gc field" : "j/k · ↩ open"
        case .list: "j/k · ↩ open"
        case .table: "j/k · h/l fold"
        case .graph(let history, let edges, let output): "j/k · ↩ open" + (edges ? " · tab edge" : "") + (output ? " · o output" : "") + (history ? " · h history" : "")
        case .lanes(let title): title ? "←/→ · ↩ open · h fold" : "←/→ · ↩ open"
        case .runstrip(let title): title ? "←/→ run · h fold" : "←/→ run"
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
    public private(set) var slot: String?
    @ObservationIgnored private var owner: String?

    public init() {}

    public var hints: String? { pane?.hints }

    public var fontSlot: String? {
        guard let pane, pane != .terminal, let slot else { return nil }
        return slot.hasSuffix(Self.fieldSuffix) ? String(slot.dropLast(Self.fieldSuffix.count)) : slot
    }

    private static let fieldSuffix = ".field"

    public func report(_ pane: FocusedPane, from id: String, slot: String? = nil, focused: Bool) {
        if focused {
            owner = id
            if self.pane != pane { self.pane = pane }
            if self.slot != slot { self.slot = slot }
        } else if owner == id {
            owner = nil
            self.pane = nil
            self.slot = nil
        }
    }
}

public enum PaneFont {
    public static let chat = "chat"

    public static func key(slot: String, composer: Bool) -> String {
        composer ? chat : String(slot.prefix { $0 != "." })
    }
}
