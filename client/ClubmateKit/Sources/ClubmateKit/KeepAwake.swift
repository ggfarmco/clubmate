import Foundation
import Observation

public struct KeepAwake: Sendable, Equatable {
    public enum Mode: String, Sendable {
        case hours
        case agents
        case on
    }

    public enum Look: Sendable {
        case off
        case armed
        case awake
    }

    public struct Hold: Sendable, Equatable {
        public let kind: String
        public let title: String
        public let trailing: String
        public let id: String
        public let session: String
        public let workspace: String

        public init(kind: String, title: String, trailing: String, id: String = "", session: String = "", workspace: String = "") {
            self.kind = kind
            self.title = title
            self.trailing = trailing
            self.id = id
            self.session = session
            self.workspace = workspace
        }

        init?(_ value: JSONValue) {
            guard let fields = value.objectValue else { return nil }
            self.init(
                kind: fields["kind"]?.stringValue ?? "",
                title: fields["title"]?.stringValue ?? "",
                trailing: fields["trailing"]?.stringValue ?? "",
                id: fields["id"]?.stringValue ?? "",
                session: fields["session"]?.stringValue ?? "",
                workspace: fields["workspace"]?.stringValue ?? ""
            )
        }
    }

    public struct HourOption: Sendable, Equatable {
        public let hours: Int
        public let title: String

        public init(hours: Int, title: String) {
            self.hours = hours
            self.title = title
        }
    }

    public struct Labels: Sendable, Equatable {
        public var section = ""
        public var hoursTitle = ""
        public var hoursActive = ""
        public var hours: [HourOption] = []
        public var agents = ""
        public var agentsSubtitle = ""
        public var on = ""
        public var off = ""

        public init() {}

        init(_ value: JSONValue?) {
            guard let fields = value?.objectValue else { return }
            func title(_ key: String) -> String { fields[key]?.objectValue?["title"]?.stringValue ?? "" }
            section = fields["section"]?.stringValue ?? ""
            let hoursFields = fields["hours"]?.objectValue
            hoursTitle = hoursFields?["title"]?.stringValue ?? ""
            hoursActive = hoursFields?["active"]?.stringValue ?? ""
            hours = (hoursFields?["options"]?.arrayValue ?? []).compactMap { option in
                guard let fields = option.objectValue, let hours = fields["hours"]?.numberValue.flatMap({ Int(exactly: $0) }) else { return nil }
                return HourOption(hours: hours, title: fields["title"]?.stringValue ?? "")
            }
            agents = title("agents")
            agentsSubtitle = fields["agents"]?.objectValue?["subtitle"]?.stringValue ?? ""
            on = title("on")
            off = title("off")
        }
    }

    public static let textMinWidth: CGFloat = 800

    public var on: Bool
    public var armed = false
    public var text = ""
    public var detail = ""
    public var mode: Mode?
    public var hours = 0
    public var until = 0
    public var value = ""
    public var holds: [Hold] = []
    public var labels = Labels()
    public var laptop = false
    public var onBattery = false
    public var battery = false
    public var display = false

    public init(on: Bool) {
        self.on = on
    }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let on = fields["on"]?.boolValue else { return nil }
        self.on = on
        armed = fields["armed"]?.boolValue ?? false
        text = fields["text"]?.stringValue ?? ""
        detail = fields["detail"]?.stringValue ?? ""
        mode = fields["mode"]?.stringValue.flatMap(Mode.init(rawValue:))
        hours = fields["hours"]?.numberValue.flatMap { Int(exactly: $0) } ?? 0
        until = fields["until"]?.numberValue.flatMap { Int(exactly: $0) } ?? 0
        self.value = fields["value"]?.stringValue ?? ""
        holds = (fields["holds"]?.arrayValue ?? []).compactMap(Hold.init)
        labels = Labels(fields["labels"])
        laptop = fields["laptop"]?.boolValue ?? false
        onBattery = fields["onBattery"]?.boolValue ?? false
        battery = fields["battery"]?.boolValue ?? false
        display = fields["display"]?.boolValue ?? false
    }

    public var look: Look {
        on ? .awake : armed ? .armed : .off
    }

    public var headline: String {
        on && !text.isEmpty ? text : detail
    }

    public func showsText(barWidth: CGFloat) -> Bool {
        on && !text.isEmpty && barWidth >= Self.textMinWidth
    }
}

public struct KeepAwakeMenu: Equatable, Sendable {
    public static let settingsTitle = "Keep Awake Settings…"

    public enum Action: Equatable, Sendable {
        case set(KeepAwake.Mode, hours: Int?)
        case turnOff
        case settings
        case openSession(session: String, workspace: String)
    }

    public enum Kind: Equatable, Sendable {
        case status
        case hold
        case section
        case item
        case separator
    }

    public struct Item: Equatable, Sendable {
        public var kind: Kind
        public var title = ""
        public var trailing = ""
        public var checked = false
        public var action: Action?
        public var toggleKey = false
        public var children: [Item] = []

        public var enabled: Bool { action != nil || !children.isEmpty }

        public var shown: String {
            trailing.isEmpty ? title : "\(title)\t\(trailing)"
        }

        var layout: LayoutMenuItem {
            LayoutMenuItem(title: shown, enabled: enabled, checked: checked, children: children.isEmpty ? nil : children.map(\.layout))
        }
    }

    public let items: [Item]

    public init(_ state: KeepAwake) {
        let labels = state.labels
        let chosen = state.mode != nil
        var items = [Item(kind: .status, title: state.headline)]
        items += state.holds.map { hold in
            Item(kind: .hold, title: hold.title, trailing: hold.trailing, action: hold.session.isEmpty ? nil : .openSession(session: hold.session, workspace: hold.workspace))
        }
        items.append(Item(kind: .separator))
        items.append(Item(kind: .section, title: labels.section))
        let hours = labels.hours.map { option in
            Item(kind: .item, title: option.title, checked: state.mode == .hours && state.hours == option.hours, action: .set(.hours, hours: option.hours))
        }
        items.append(Item(kind: .item, title: state.mode == .hours ? labels.hoursActive : labels.hoursTitle, checked: state.mode == .hours, children: hours))
        items.append(Item(kind: .item, title: labels.agents, checked: state.mode == .agents, action: .set(.agents, hours: nil)))
        items.append(Item(kind: .item, title: labels.on, checked: state.mode == .on, action: .set(.on, hours: nil), toggleKey: !chosen))
        if chosen {
            items.append(Item(kind: .item, title: labels.off, action: .turnOff, toggleKey: true))
        }
        items.append(Item(kind: .separator))
        items.append(Item(kind: .item, title: Self.settingsTitle, action: .settings))
        self.items = items
    }

    public var layout: [LayoutMenuItem] {
        items.filter { $0.kind != .separator }.map(\.layout)
    }
}

@MainActor
@Observable
public final class KeepAwakeModel {
    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public static let command = "awake.state"
    public static let setCommand = "awake.set"
    public static let toggleCommand = "awake.toggle"
    public static let topic = "awake.changed"

    public private(set) var state: KeepAwake?
    public private(set) var error: String?

    @ObservationIgnored private let run: Run

    public init(run: @escaping Run) {
        self.run = run
    }

    public func load() async {
        switch await run(CommandInvocation(id: Self.command, args: .object([:]))) {
        case .success(let value):
            receive(value)
        case .failure:
            state = nil
        }
    }

    public func set(mode: KeepAwake.Mode, hours: Int? = nil) async {
        var args: [String: JSONValue] = ["mode": .string(mode.rawValue)]
        if let hours { args["hours"] = .number(Double(hours)) }
        await call(Self.setCommand, args)
    }

    public func turnOff() async {
        await call(Self.setCommand, ["mode": .string("off")])
    }

    public func toggle() async {
        await call(Self.toggleCommand, [:])
    }

    public func follow(_ client: CoreClient) async {
        let events = try? await client.busEvents(pattern: Self.topic)
        await load()
        guard let events else { return }
        for await event in events {
            receive(event.payload)
        }
    }

    public func receive(_ value: JSONValue) {
        error = nil
        let state = KeepAwake(value)
        if state != self.state { self.state = state }
    }

    private func call(_ command: String, _ args: [String: JSONValue]) async {
        switch await run(CommandInvocation(id: command, args: .object(args))) {
        case .success(let value):
            receive(value)
        case .failure(let failure):
            error = failure.commandText
        }
    }
}
