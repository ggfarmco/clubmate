import Foundation
import Observation

public struct KeepAwake: Sendable, Equatable {
    public let on: Bool
    public let text: String
    public let detail: String
    public let manual: Bool
    public let agents: Bool

    public init(on: Bool, text: String, detail: String, manual: Bool, agents: Bool = false) {
        self.on = on
        self.text = text
        self.detail = detail
        self.manual = manual
        self.agents = agents
    }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let on = fields["on"]?.boolValue else { return nil }
        self.init(
            on: on,
            text: fields["text"]?.stringValue ?? "",
            detail: fields["detail"]?.stringValue ?? "",
            manual: fields["manual"]?.boolValue ?? false,
            agents: fields["agents"]?.boolValue ?? false
        )
    }
}

@MainActor
@Observable
public final class KeepAwakeModel {
    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public static let command = "awake.state"
    public static let setCommand = "awake.set"
    public static let settingsCommand = "awake.settings-set"
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

    public func keepAwake(minutes: Int?) async {
        var args: [String: JSONValue] = ["on": .bool(true)]
        if let minutes { args["minutes"] = .number(Double(minutes)) }
        await set(args)
    }

    public func turnOff() async {
        await set(["on": .bool(false)])
    }

    public func whileAgentsWork(_ on: Bool) async {
        await set(["agents": .bool(on)], command: Self.settingsCommand)
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

    private func set(_ args: [String: JSONValue], command: String = setCommand) async {
        switch await run(CommandInvocation(id: command, args: .object(args))) {
        case .success(let value):
            receive(value)
        case .failure(let failure):
            error = failure.commandText
        }
    }
}
