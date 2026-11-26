import Foundation

public struct SDUIGuideStep: Sendable, Equatable {
    public let step: String
    public let label: String
    public let node: String?
}

public struct SDUIStepAction: Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let enabled: Bool
    public let primary: Bool
    public let shortcut: String?

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        enabled = fields["enabled"]?.boolValue ?? false
        primary = fields["primary"]?.boolValue ?? false
        shortcut = fields["shortcut"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
    }
}

public struct SDUIStepAside: Sendable, Equatable {
    public let step: String
    public let title: String
    public let open: Bool
    public let session: String?

    public init?(_ value: JSONValue?) {
        guard let fields = value?.objectValue, let step = fields["step"]?.stringValue, let title = fields["title"]?.stringValue else { return nil }
        self.step = step
        self.title = title
        open = fields["open"]?.boolValue ?? false
        session = fields["session"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
    }
}

public struct SDUIStepControls: Sendable, Equatable {
    public let session: String
    public let step: String
    public let actions: [SDUIStepAction]
    public let pressed: String?
    public let aside: SDUIStepAside?
}

public struct SDUITourLink: Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
    }
}

public struct SDUITourConcept: Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let state: String
    public let stateLabel: String
    public let stepLabel: String
    public let depth: Int
    public let note: String?
    public let links: [SDUITourLink]
    public let offers: [SDUITourLink]

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue, let state = fields["state"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        self.state = state
        stateLabel = fields["stateLabel"]?.stringValue ?? state
        stepLabel = fields["stepLabel"]?.stringValue ?? ""
        depth = max(0, fields["depth"]?.numberValue.map(Int.init) ?? 0)
        note = fields["note"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
        links = fields["links"]?.arrayValue?.compactMap(SDUITourLink.init) ?? []
        offers = fields["offers"]?.arrayValue?.compactMap(SDUITourLink.init) ?? []
    }
}

public struct SDUITourLegend: Sendable, Equatable, Identifiable {
    public let state: String
    public let label: String

    public var id: String { state }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let state = fields["state"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.state = state
        self.label = label
    }
}

public struct SDUITour: Sendable, Equatable {
    public let session: String
    public let title: String
    public let step: Int
    public let of: Int
    public let stepLabel: String
    public let summary: String
    public let current: String?
    public let concepts: [SDUITourConcept]
    public let legend: [SDUITourLegend]
    public let shown: Bool
    public let shortcut: String?

    public var currentConcept: SDUITourConcept? { concepts.first { $0.id == current } }
}

extension SDUINode {
    public func guideStep() -> SDUIGuideStep? {
        guard let step = string("step"), let label = string("label") else { return nil }
        return SDUIGuideStep(step: step, label: label, node: string("node").flatMap { $0.isEmpty ? nil : $0 })
    }

    public func stepControls() -> SDUIStepControls? {
        guard let session = string("session"), let step = string("step") else { return nil }
        return SDUIStepControls(
            session: session, step: step,
            actions: props["actions"]?.arrayValue?.compactMap(SDUIStepAction.init) ?? [],
            pressed: string("pressed").flatMap { $0.isEmpty ? nil : $0 },
            aside: SDUIStepAside(props["aside"])
        )
    }

    public func tour() -> SDUITour? {
        guard let session = string("session"), let title = string("title") else { return nil }
        let number = { (key: String) in self.props[key]?.numberValue.map(Int.init) ?? 0 }
        return SDUITour(
            session: session, title: title, step: number("step"), of: number("of"),
            stepLabel: string("stepLabel") ?? "", summary: string("summary") ?? "",
            current: string("current").flatMap { $0.isEmpty ? nil : $0 },
            concepts: props["concepts"]?.arrayValue?.compactMap(SDUITourConcept.init) ?? [],
            legend: props["legend"]?.arrayValue?.compactMap(SDUITourLegend.init) ?? [],
            shown: bool("shown") ?? true,
            shortcut: string("shortcut").flatMap { $0.isEmpty ? nil : $0 }
        )
    }

    public var headerStrip: SDUIChild? {
        identifiedChildren.first { $0.node.type == "guide.strip" }
    }
}
