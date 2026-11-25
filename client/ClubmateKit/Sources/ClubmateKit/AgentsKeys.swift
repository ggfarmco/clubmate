import Foundation

import ClubmateProtocol

public enum AgentsColumn: String, Equatable, Sendable {
    case main
    case aside

    public var slot: String { self == .main ? AgentsSection.mainSlot : AgentsSection.asideSlot }

    public init?(slot: String?) {
        switch slot {
        case AgentsSection.mainSlot: self = .main
        case AgentsSection.asideSlot: self = .aside
        default: return nil
        }
    }
}

public enum AgentsPlace: Equatable, Sendable {
    case tree
    case transcript(AgentsColumn)
    case composer(AgentsColumn)

    public var column: AgentsColumn? {
        switch self {
        case .tree: nil
        case .transcript(let column), .composer(let column): column
        }
    }
}

public enum AgentsChord: Equatable, Sendable {
    case stepOut
    case stop
    case tree
    case right
    case transcript
    case composer
    case keys

    public static func control(keyCode: UInt16, command: Bool, control: Bool, option: Bool, shift: Bool) -> Bool {
        control && !command && !option && !shift && [.transcript, .composer].contains(resolve(keyCode: keyCode, command: command, control: control, option: option, shift: shift))
    }

    public static func resolve(keyCode: UInt16, command: Bool, control: Bool, option: Bool, shift: Bool) -> AgentsChord? {
        if shift { return nil }
        switch (keyCode, command, control, option) {
        case (53, false, false, false), (33, false, true, false): return .stepOut
        case (47, true, false, false): return .stop
        case (44, true, false, false): return .keys
        case (123, true, false, true): return .tree
        case (124, true, false, true): return .right
        case (126, true, false, true), (40, false, true, false): return .transcript
        case (125, true, false, true), (38, false, true, false): return .composer
        default: return nil
        }
    }
}

public enum TranscriptKey: Equatable, Sendable {
    case type
    case page(Int)
    case column(Int)
    case step(TranscriptStep)
    case leader
    case help

    public static func resolve(keyCode: UInt16, characters: String?, command: Bool, control: Bool, option: Bool, shift: Bool) -> TranscriptKey? {
        if command || option { return nil }
        if control { return shift ? nil : keyCode == 4 ? .column(-1) : keyCode == 37 ? .column(1) : nil }
        if keyCode == 49 { return .page(shift ? -1 : 1) }
        if shift { return keyCode == 5 ? .step(.last) : characters == "?" ? .help : nil }
        if characters == "i" || characters == "a" { return .type }
        switch keyCode {
        case 38, 125: return .step(.move(1))
        case 40, 126: return .step(.move(-1))
        case 37, 124: return .step(.unfold)
        case 4, 123: return .step(.fold)
        case 36, 76: return .step(.toggle)
        case TreeChord.leader: return .leader
        default: return nil
        }
    }
}

public enum TranscriptChord: Equatable, Sendable {
    case first
    case file
    case go(TreeChord)

    public static func resolve(_ keyCode: UInt16, shift: Bool) -> TranscriptChord? {
        if !shift, keyCode == TreeChord.leader { return .first }
        if !shift, keyCode == 3 { return .file }
        return TreeChord.resolve(keyCode, shift: shift, sessions: true).map(TranscriptChord.go)
    }

    public static let fileLabel = "gf"

    public static func whichKey(file: Bool) -> String {
        "g → " + (file ? "f file · " : "") + "g first · 1–9 session · l last · n needs you · t tree · i reply"
    }
}

public struct AgentsLayout: Equatable, Sendable {
    public var composers: Set<AgentsColumn>
    public var transcripts: Set<AgentsColumn>
    public var lastComposer: AgentsColumn?
    public var side: SidebarSide

    public init(composers: Set<AgentsColumn> = [], transcripts: Set<AgentsColumn> = [], lastComposer: AgentsColumn? = nil, side: SidebarSide = .left) {
        self.composers = composers
        self.transcripts = transcripts
        self.lastComposer = lastComposer
        self.side = side
    }

    public init(main: SDUINode?, aside: SDUINode?, lastComposer: String?, side: SidebarSide) {
        self.init(lastComposer: AgentsColumn(slot: lastComposer), side: side)
        for (column, tree) in [(AgentsColumn.main, main), (.aside, aside)] {
            guard let tree else { continue }
            if tree.hasComposer { composers.insert(column) }
            if tree.firstNode(type: "transcript")?.children.isEmpty == false { transcripts.insert(column) }
        }
    }

    var typing: AgentsColumn? {
        if let lastComposer, composers.contains(lastComposer) { return lastComposer }
        return composers.contains(.main) ? .main : composers.first
    }
}

public enum AgentsMove: Equatable, Sendable {
    case focus(AgentsPlace)
    case note(String)
    case stay
}

public enum AgentsKeys {
    public static let transcriptSuffix = ".transcript"
    public static let cardSuffix = ".card"
    public static let noSession = "No session open · ↩ open · ⌘N new"
    public static let nothingRuns = "Nothing runs here"

    public static func steps(section: String?) -> Bool {
        section == AgentsSection.id
    }

    public static func isAgents(slot: String?) -> Bool {
        slot?.hasPrefix(AgentsSection.id + ".") == true
    }

    public static func move(_ chord: AgentsChord, from place: AgentsPlace?, in layout: AgentsLayout) -> AgentsMove {
        switch chord {
        case .stepOut: stepOut(from: place, in: layout)
        case .stop: .note(nothingRuns)
        case .keys: .stay
        case .tree: .focus(.tree)
        case .right: right(from: place, in: layout)
        case .transcript: layout.transcripts.contains(place?.column ?? .main) ? .focus(.transcript(place?.column ?? .main)) : .stay
        case .composer: compose(place?.column ?? layout.typing ?? .main, in: layout)
        }
    }

    public static func column(_ delta: Int, from place: AgentsPlace, in layout: AgentsLayout) -> AgentsMove {
        var columns: [AgentsPlace] = [.tree, .transcript(.main)]
        if layout.transcripts.contains(.aside) || layout.composers.contains(.aside) { columns.append(.transcript(.aside)) }
        if layout.side == .right { columns.append(columns.removeFirst()) }
        let at = columns.firstIndex { $0.column == place.column } ?? 0
        let next = at + delta
        guard columns.indices.contains(next) else { return .stay }
        let target = columns[next]
        if case .transcript(let column) = target, !layout.transcripts.contains(column) { return compose(column, in: layout) }
        return .focus(target)
    }

    private static func stepOut(from place: AgentsPlace?, in layout: AgentsLayout) -> AgentsMove {
        switch place {
        case .composer(let column):
            return .focus(layout.transcripts.contains(column) ? .transcript(column) : .tree)
        case .transcript(.aside):
            return .focus(layout.transcripts.contains(.main) ? .transcript(.main) : .tree)
        case .transcript(.main), nil:
            return .focus(.tree)
        case .tree:
            return layout.typing.map { .focus(.composer($0)) } ?? .note(noSession)
        }
    }

    private static func right(from place: AgentsPlace?, in layout: AgentsLayout) -> AgentsMove {
        guard let column = place?.column else { return compose(.main, in: layout) }
        return column == .main && layout.composers.contains(.aside) ? .focus(.composer(.aside)) : .stay
    }

    private static func compose(_ column: AgentsColumn, in layout: AgentsLayout) -> AgentsMove {
        layout.composers.contains(column) ? .focus(.composer(column)) : column == .main && layout.composers.isEmpty ? .note(noSession) : .stay
    }

    public static func stopsHere(button slot: String?, focused: String?) -> Bool {
        guard let slot, isAgents(slot: slot) else { return true }
        return (slot == AgentsSection.asideSlot) == (focused == AgentsSection.asideSlot)
    }
}

@MainActor
public final class StopHint {
    public static let text = StopNote.text
    public static let maxShows = StopNote.shows
    public static let duration = Duration.seconds(StopNote.seconds)

    private let defaults: UserDefaults

    public init(defaults: UserDefaults = TestMode.defaults) {
        self.defaults = defaults
    }

    public func show() -> Bool {
        let shows = defaults.integer(forKey: Self.key)
        guard shows < Self.maxShows else { return false }
        defaults.set(shows + 1, forKey: Self.key)
        return true
    }

    private static let key = "stopHintShows"
}
