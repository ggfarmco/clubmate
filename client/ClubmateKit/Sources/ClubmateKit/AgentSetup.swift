import Foundation
import Observation

public struct AgentSetup: Sendable, Equatable {
    public let state: String
    public let title: String
    public let detail: String
    public let fixLabel: String

    public init(state: String, title: String = "", detail: String = "", fixLabel: String = "") {
        self.state = state
        self.title = title
        self.detail = detail
        self.fixLabel = fixLabel
    }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let state = fields["state"]?.stringValue else { return nil }
        self.init(
            state: state,
            title: fields["title"]?.stringValue ?? "",
            detail: fields["detail"]?.stringValue ?? "",
            fixLabel: fields["fix"]?.objectValue?["label"]?.stringValue ?? ""
        )
    }

    public var shown: Bool { !title.isEmpty }
}

@MainActor
@Observable
public final class AgentSetupModel {
    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public static let command = "agents.setup"
    public static let fixCommand = "agents.setup-fix"
    public static let topic = "agents.setup.changed"

    public private(set) var state: AgentSetup?
    public private(set) var fixError: String?

    @ObservationIgnored private let run: Run

    public init(run: @escaping Run) {
        self.run = run
    }

    public func load(check: Bool = false) async {
        if case .success(let value) = await run(CommandInvocation(id: Self.command, args: .object(["check": .bool(check)]))) {
            receive(value)
        }
    }

    public func fix() async {
        switch await run(CommandInvocation(id: Self.fixCommand, args: .object([:]))) {
        case .success: fixError = nil
        case .failure(let error): fixError = error.commandText
        }
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
        let state = AgentSetup(value)
        if state != self.state {
            self.state = state
            fixError = nil
        }
    }
}
