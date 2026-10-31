import SwiftUI

import CompanionKit

struct SessionSettingsView: View {
    let store: AgentsStore
    let sessionID: String
    @Environment(\.dismiss) private var dismiss
    @State private var overrides: [String: String] = [:]
    @State private var failure: String?

    private static let advisors = [("", "Default"), ("fable", "Fable"), ("opus", "Opus"), ("sonnet", "Sonnet")]
    private static let finishes = [(AgentFinish.ask, "Ask"), (AgentFinish.merge, "Merge to Main")]
    private static let policies = [(NotificationPolicy.all, "All"), (NotificationPolicy.needsInput, "Needs Input Only"), (NotificationPolicy.muted, "Muted")]

    var body: some View {
        let session = store.session(sessionID)
        let transcript = store.transcripts[sessionID]
        let claude = session?.agent == "claude"
        let configs = transcript?.configOptions.filter(\.pickable) ?? []
        NavigationStack {
            Form {
                if let session {
                    if session.parentSession == nil && claude {
                        Section {
                            Toggle("AFK", isOn: Binding(
                                get: { (overrides["afk"] ?? (session.afk ? "on" : "off")) == "on" },
                                set: { apply("afk", $0 ? "on" : "off") { try await store.setConfig(sessionID, id: "afk", value: $0) } }
                            ))
                            .accessibilityIdentifier("settings.afk")
                            Picker("Finish", selection: choice("finish", session.finish.rawValue) { try await store.setConfig(sessionID, id: "finish", value: $0) }) {
                                ForEach(Self.finishes, id: \.0) { Text($0.1).tag($0.0.rawValue) }
                            }
                        } footer: {
                            Text("AFK: the agent works without asking you and reports when done; child sessions follow it. Finish: what the agent does once the task is done.")
                        }
                    }
                    if let transcript, transcript.modes.count > 1 || !configs.isEmpty || claude {
                        Section("Agent") {
                            if transcript.modes.count > 1 {
                                Picker("Mode", selection: choice("mode", session.mode) { try await store.setMode(sessionID, mode: $0) }) {
                                    ForEach(transcript.modes) { Text($0.name).tag($0.id) }
                                }
                            }
                            ForEach(configs) { option in
                                Picker(option.name.isEmpty ? option.id : option.name, selection: choice("config/" + option.id, option.currentValue) { try await store.setConfig(sessionID, id: option.id, value: $0) }) {
                                    ForEach(option.choices, id: \.value) { Text($0.label).tag($0.value) }
                                }
                            }
                            if claude {
                                Picker("Advisor", selection: choice("advisor", transcript.advisor) { try await store.setConfig(sessionID, id: "advisor", value: $0) }) {
                                    ForEach(Self.advisors, id: \.0) { Text($0.1).tag($0.0) }
                                }
                            }
                        }
                    }
                    Section {
                        Picker("Notifications", selection: choice("notify", (session.notify ?? .all).rawValue) { try await store.setNotify(sessionID, policy: NotificationPolicy($0)) }) {
                            ForEach(Self.policies, id: \.0) { Text($0.1).tag($0.0.rawValue) }
                        }
                    } footer: {
                        if session.parentSession != nil {
                            Text("AFK and Finish follow the parent session. A quieter parent setting also applies here.")
                        }
                    }
                }
            }
            .navigationTitle("Session Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done", systemImage: "checkmark") { dismiss() }
                }
            }
            .alert("Couldn't Change the Setting", isPresented: Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })) {
                Button("OK") { failure = nil }
            } message: {
                Text(failure ?? "")
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func choice(_ key: String, _ current: String, _ work: @escaping @MainActor (String) async throws -> Void) -> Binding<String> {
        Binding(get: { overrides[key] ?? current }, set: { apply(key, $0, work) })
    }

    private func apply(_ key: String, _ value: String, _ work: @escaping @MainActor (String) async throws -> Void) {
        overrides[key] = value
        Task {
            do {
                try await work(value)
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
            }
            if overrides[key] == value { overrides[key] = nil }
        }
    }
}
