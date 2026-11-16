import Foundation

public struct AwakeState: Codable, Equatable, Sendable {
    public var on: Bool
    public var armed: Bool
    public var text: String
    public var detail: String
    public var mode: String
    public var hours: Int
    public var until: Int
    public var value: String
    public var ends: String
    public var holds: [AwakeHold]
    public var labels: AwakeLabels?
    public var laptop: Bool
    public var onBattery: Bool

    public init(on: Bool = false, armed: Bool = false, text: String = "", detail: String = "", mode: String = "", hours: Int = 0, until: Int = 0, value: String = "", ends: String = "", holds: [AwakeHold] = [], labels: AwakeLabels? = nil, laptop: Bool = false, onBattery: Bool = false) {
        self.on = on
        self.armed = armed
        self.text = text
        self.detail = detail
        self.mode = mode
        self.hours = hours
        self.until = until
        self.value = value
        self.ends = ends
        self.holds = holds
        self.labels = labels
        self.laptop = laptop
        self.onBattery = onBattery
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        on = try c.decodeIfPresent(Bool.self, forKey: .on) ?? false
        armed = try c.decodeIfPresent(Bool.self, forKey: .armed) ?? false
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        detail = try c.decodeIfPresent(String.self, forKey: .detail) ?? ""
        mode = try c.decodeIfPresent(String.self, forKey: .mode) ?? ""
        hours = try c.decodeIfPresent(Int.self, forKey: .hours) ?? 0
        until = try c.decodeIfPresent(Int.self, forKey: .until) ?? 0
        value = try c.decodeIfPresent(String.self, forKey: .value) ?? ""
        ends = try c.decodeIfPresent(String.self, forKey: .ends) ?? ""
        holds = (try? c.decodeIfPresent([AwakeHold].self, forKey: .holds)) ?? []
        labels = try? c.decodeIfPresent(AwakeLabels.self, forKey: .labels)
        laptop = try c.decodeIfPresent(Bool.self, forKey: .laptop) ?? false
        onBattery = try c.decodeIfPresent(Bool.self, forKey: .onBattery) ?? false
    }

    public var headline: String {
        on ? text : detail
    }

    public var footnote: String? {
        on && !detail.isEmpty ? detail : nil
    }

    public var chosen: Bool {
        !mode.isEmpty
    }

    public var agentHolds: [AwakeHold] {
        holds.filter { $0.kind == AwakeHold.agent }
    }
}

public struct AwakeHold: Codable, Equatable, Sendable, Identifiable {
    public static let agent = "agent"

    public var kind: String
    public var title: String
    public var trailing: String
    public var hold: String?
    public var session: String?
    public var workspace: String?

    public var id: String {
        hold ?? kind + ":" + title
    }

    public init(kind: String, title: String, trailing: String = "", hold: String? = nil, session: String? = nil, workspace: String? = nil) {
        self.kind = kind
        self.title = title
        self.trailing = trailing
        self.hold = hold
        self.session = session
        self.workspace = workspace
    }

    private enum CodingKeys: String, CodingKey {
        case kind, title, trailing, hold = "id", session, workspace
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        trailing = try c.decodeIfPresent(String.self, forKey: .trailing) ?? ""
        hold = try c.decodeIfPresent(String.self, forKey: .hold)
        session = try c.decodeIfPresent(String.self, forKey: .session)
        workspace = try c.decodeIfPresent(String.self, forKey: .workspace)
    }
}

public struct AwakeLabels: Codable, Equatable, Sendable {
    public struct Hours: Codable, Equatable, Sendable {
        public var title: String
        public var active: String
        public var options: [Option]
    }

    public struct Option: Codable, Equatable, Sendable, Identifiable {
        public var hours: Int
        public var title: String
        public var short: String?

        public var id: Int { hours }
    }

    public struct Choice: Codable, Equatable, Sendable {
        public var title: String
        public var subtitle: String?
    }

    public var section: String
    public var picker: String?
    public var hours: Hours
    public var agents: Choice
    public var on: Choice
    public var off: Choice
}
