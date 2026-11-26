import Foundation

import ClubmateProtocol

public enum GuideCommand {
    public static let stepAct = "agents.step-act"
    public static let tourOffer = "agents.tour-offer"
}

public struct GuideKey: Sendable, Equatable {
    public let key: String
    public let command: Bool
    public let option: Bool
    public let shift: Bool
    public let control: Bool

    public init?(_ text: String?) {
        guard let text, !text.isEmpty else { return nil }
        let parts = text.lowercased().split(separator: "+").map(String.init)
        guard let key = parts.last, !key.isEmpty else { return nil }
        let mods = Set(parts.dropLast())
        self.key = key
        command = mods.contains("cmd")
        option = mods.contains("opt") || mods.contains("alt")
        shift = mods.contains("shift")
        control = mods.contains("ctrl")
    }

    public var label: String {
        let glyph = switch key {
        case "return", "enter": "↩"
        case "escape", "esc": "⎋"
        case "space": "Space"
        default: key.uppercased()
        }
        return (control ? "⌃" : "") + (option ? "⌥" : "") + (shift ? "⇧" : "") + (command ? "⌘" : "") + glyph
    }
}

public struct GuideStep: Decodable, Sendable, Equatable {
    public let step: String
    public let label: String
    public let node: String?
}

public struct StepControls: Decodable, Sendable, Equatable {
    public struct Action: Decodable, Sendable, Equatable, Identifiable {
        public let id: String
        public let label: String
        public let enabled: Bool
        public let primary: Bool
        public let shortcut: String?

        enum CodingKeys: String, CodingKey {
            case id, label, enabled, primary, shortcut
        }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.decode(String.self, forKey: .id)
            label = try c.decodeIfPresent(String.self, forKey: .label) ?? id
            enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
            primary = try c.decodeIfPresent(Bool.self, forKey: .primary) ?? false
            shortcut = try c.decodeIfPresent(String.self, forKey: .shortcut)
        }

        public var key: GuideKey? { GuideKey(shortcut) }
    }

    public struct Aside: Decodable, Sendable, Equatable {
        public let step: String
        public let title: String
        public let open: Bool
        public let session: String?
    }

    public static let goDeeper = "go_deeper"

    public let session: String
    public let step: String
    public let actions: [Action]
    public let pressed: String?
    public let aside: Aside?

    public func on(_ action: Action) -> Bool {
        pressed == action.id || (action.id == Self.goDeeper && aside?.open == true)
    }

    public func openAside(_ action: Action) -> String? {
        guard action.id == Self.goDeeper, let aside, aside.open, let session = aside.session, !session.isEmpty else { return nil }
        return session
    }
}

public enum TourState: String, FallbackEnum {
    case known
    case shaky
    case missed
    case current
    case notYet = "not_yet"
    case other

    public static let fallback = Self.other
}

public struct TourMap: Decodable, Sendable, Equatable {
    public struct Counts: Decodable, Sendable, Equatable {
        public let known: Int
        public let shaky: Int
        public let missed: Int
        public let notYet: Int
    }

    public struct Chip: Decodable, Sendable, Equatable, Identifiable {
        public let id: String
        public let label: String
    }

    public struct Concept: Decodable, Sendable, Equatable, Identifiable {
        public let id: String
        public let label: String
        public let state: TourState
        public let stateLabel: String
        public let step: Int
        public let stepLabel: String
        public let depth: Int
        public let note: String?
        public let links: [Chip]
        public let offers: [Chip]

        enum CodingKeys: String, CodingKey {
            case id, label, state, stateLabel, step, stepLabel, depth, note, links, offers
        }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.decode(String.self, forKey: .id)
            label = try c.decodeIfPresent(String.self, forKey: .label) ?? id
            state = try c.decodeIfPresent(TourState.self, forKey: .state) ?? .other
            stateLabel = try c.decodeIfPresent(String.self, forKey: .stateLabel) ?? ""
            step = try c.decodeIfPresent(Int.self, forKey: .step) ?? 0
            stepLabel = try c.decodeIfPresent(String.self, forKey: .stepLabel) ?? ""
            depth = try c.decodeIfPresent(Int.self, forKey: .depth) ?? 0
            note = try c.decodeIfPresent(String.self, forKey: .note)
            links = try c.decodeIfPresent([Chip].self, forKey: .links) ?? []
            offers = try c.decodeIfPresent([Chip].self, forKey: .offers) ?? []
        }

        public var opens: Bool { note != nil || !offers.isEmpty || !links.isEmpty }
    }

    public struct Legend: Decodable, Sendable, Equatable, Identifiable {
        public let state: TourState
        public let label: String

        public var id: String { state.rawValue }
    }

    public let session: String
    public let title: String
    public let step: Int
    public let of: Int
    public let stepLabel: String
    public let summary: String
    public let current: String?
    public let counts: Counts?
    public let concepts: [Concept]
    public let legend: [Legend]
    public let shortcut: String?

    public var currentConcept: Concept? {
        current.flatMap { id in concepts.first { $0.id == id } }
    }

    public var stripLabel: String {
        currentConcept.map { "\(step) \($0.label)" } ?? stepLabel
    }
}
