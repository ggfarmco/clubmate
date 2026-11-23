import Foundation
import Observation

public struct EditorCommand: Codable, Hashable, Sendable {
    public let id: String
    public let title: String
    public let detail: String

    public init(id: String, title: String, detail: String) {
        self.id = id
        self.title = title
        self.detail = detail
    }
}

public struct EditorState: Codable, Hashable, Sendable {
    public static let neovim = "editor-nvim"
    public static let vscode = "editor-vscode"
    public static let importCommand = "editor-nvim.import-config"
    public static let importTopic = "editor-nvim.import"

    public let enabled: [String]
    public let review: String
    public let nvimImport: String
    public let importRow: Bool
    public let caption: String
    public let commands: [EditorCommand]

    public init(enabled: [String], review: String, nvimImport: String, importRow: Bool, caption: String, commands: [EditorCommand]) {
        self.enabled = enabled
        self.review = review
        self.nvimImport = nvimImport
        self.importRow = importRow
        self.caption = caption
        self.commands = commands
    }

    public func isOn(_ plugin: String) -> Bool {
        enabled.contains(plugin)
    }
}

@MainActor
@Observable
public final class EditorsModel {
    public private(set) var state: EditorState?

    @ObservationIgnored private var client: CoreClient?

    public init() {}

    public func follow(_ client: CoreClient) async {
        self.client = client
        let events = try? await client.busEvents(pattern: Topic.pluginChanged + "," + EditorState.importTopic)
        await load()
        guard let events else { return }
        for await _ in events {
            await load()
        }
    }

    public func set(_ plugin: String, on: Bool) async -> String? {
        guard let client else { return "Not connected to the core" }
        do {
            try await client.pluginSetEnabled(id: plugin, enabled: on)
            await load()
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    public func load() async {
        guard let client, let state = try? await client.editorState() else { return }
        if state != self.state { self.state = state }
    }
}
