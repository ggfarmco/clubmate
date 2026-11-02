import Foundation
import Observation

public struct VoiceModelOption: Sendable, Equatable, Identifiable {
    public let id: String
    public let title: String
    public let detail: String

    public init(id: String, title: String, detail: String) {
        self.id = id
        self.title = title
        self.detail = detail
    }
}

@MainActor
@Observable
public final class VoiceSettingsModel {
    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public static let command = "voice.settings"
    public static let setCommand = "voice.settings-set"
    public static let topic = "voice.settings-changed"

    public private(set) var enabled: Bool?
    public private(set) var model: String?
    public private(set) var models: [VoiceModelOption] = []
    public private(set) var error: String?

    @ObservationIgnored private let run: Run

    public init(run: @escaping Run) {
        self.run = run
    }

    public var isOn: Bool { enabled == true }

    public var chosen: VoiceModelOption? {
        models.first { $0.id == model }
    }

    public func load() async {
        await apply(CommandInvocation(id: Self.command, args: .object([:])))
    }

    public func setEnabled(_ enabled: Bool) async {
        guard enabled != self.enabled else { return }
        await apply(CommandInvocation(id: Self.setCommand, args: .object(["enabled": .bool(enabled)])))
    }

    public func setModel(_ model: String) async {
        guard model != self.model else { return }
        await apply(CommandInvocation(id: Self.setCommand, args: .object(["model": .string(model)])))
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
        guard let fields = value.objectValue, let enabled = fields["enabled"]?.boolValue else {
            error = "unexpected reply from \(Self.command)"
            return
        }
        self.enabled = enabled
        model = fields["model"]?.stringValue
        models = (fields["models"]?.arrayValue ?? []).compactMap { item in
            guard let item = item.objectValue, let id = item["id"]?.stringValue else { return nil }
            return VoiceModelOption(id: id, title: item["title"]?.stringValue ?? id, detail: item["detail"]?.stringValue ?? "")
        }
        error = nil
    }

    private func apply(_ invocation: CommandInvocation) async {
        switch await run(invocation) {
        case .success(let value):
            receive(value)
        case .failure(let failure):
            error = failure.commandText
        }
    }
}
