import Foundation
import Observation

public enum FocusedPane: Equatable, Sendable {
    case tree(field: Bool)
    case list
    case table
    case graph(history: Bool, edges: Bool = false, output: Bool = false, opens: Bool = true)
    case lanes(title: Bool)
    case runstrip(title: Bool)
    case composer(running: Bool, queues: Bool, steps: Bool = false)
    case transcript
    case openItems
    case connectCard
    case quiz
    case keys(String)
    case field
    case terminal

    public var hints: String? {
        switch self {
        case .tree(let field): field ? "j/k · ↩ open · gc field" : "j/k · ↩ open"
        case .list: "j/k · ↩ open"
        case .table: "j/k · h/l fold"
        case .graph(let history, let edges, let output, let opens): (opens ? "j/k · ↩ open" : "j/k") + (edges ? " · tab edge" : "") + (output ? " · o output" : "") + (history ? " · h history" : "")
        case .lanes(let title): title ? "←/→ · ↩ open · h fold" : "←/→ · ↩ open"
        case .runstrip(let title): title ? "←/→ run · h fold" : "←/→ run"
        case .composer(let running, let queues, false): queues ? "↩ queue · Esc stop" : running ? "↩ send · Esc stop" : "↩ send"
        case .composer(let running, let queues, true): (queues ? "↩ queue" : "↩ send") + " · ⎋ read · ⌥⌘← tree" + (running ? " · ⌘. stop" : "")
        case .transcript: "j/k block · h/l fold · G newest · i type · ? keys"
        case .openItems: "1–4 pick · L later · ⌘↩ send"
        case .connectCard: "⌥1 connect · space un-tick · ⌥3 don't"
        case .quiz: "↑↓ line · ⇧↑↓ range · ↩ pick · 1–3 rate · ⌥Q fold"
        case .keys(let hints): hints
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
    @ObservationIgnored public private(set) var typedIn: String?

    public init() {}

    public private(set) var note: String?
    @ObservationIgnored private var noteSerial = 0

    public var whichKey: String?
    public var keysShown = false

    public var hints: String? { note ?? whichKey ?? pane?.hints }

    public var mode: String? {
        guard AgentsKeys.isAgents(slot: slot) else { return nil }
        switch pane {
        case .tree, .field: return "TREE"
        case .transcript: return "READ"
        case .composer: return "TYPE"
        default: return nil
        }
    }

    public func flash(_ text: String, for duration: Duration = StopHint.duration) {
        noteSerial += 1
        let serial = noteSerial
        note = text
        Task { [weak self] in
            try? await Task.sleep(for: duration)
            guard let self, self.noteSerial == serial else { return }
            self.note = nil
        }
    }

    public var fontSlot: String? {
        guard let pane, pane != .terminal, let slot else { return nil }
        return slot.hasSuffix(Self.fieldSuffix) ? String(slot.dropLast(Self.fieldSuffix.count)) : slot
    }

    private static let fieldSuffix = ".field"

    public func report(_ pane: FocusedPane, from id: String, slot: String? = nil, focused: Bool) {
        if focused {
            if case .composer = pane { typedIn = slot }
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
