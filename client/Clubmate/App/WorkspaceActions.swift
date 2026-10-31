import AppKit
import Foundation
import Observation

import ClubmateKit

@MainActor
@Observable
final class WorkspaceActions {
    private let supervisor: ConnectionSupervisor
    private let windows: WindowRouter
    private let sessions: SessionRegistry
    private let workspace: @MainActor (String) -> Workspace?

    init(supervisor: ConnectionSupervisor, windows: WindowRouter, sessions: SessionRegistry, workspace: @escaping @MainActor (String) -> Workspace?) {
        self.supervisor = supervisor
        self.windows = windows
        self.sessions = sessions
        self.workspace = workspace
    }

    func openFolder(in window: String?) {
        guard let folders = Self.chooseFolders(prompt: "Open") else { return }
        perform { [windows] client in
            let id = try await client.openFolders(folders)
            windows.open(id, in: window)
        }
    }

    func newHomeTerminal(in window: String?) {
        perform { [windows, sessions] client in
            let id = try await client.homeWorkspace()
            windows.open(id, in: window)
            let session = sessions.existing(id)
            session?.store?.show(section: TerminalsSection.id)
            session?.autoNewRequest += 1
        }
    }

    func addFolder(to workspaceID: String) {
        guard let folders = Self.chooseFolders(prompt: "Add") else { return }
        perform { client in
            for folder in folders {
                try await client.addRepo(workspaceID, path: folder)
            }
        }
    }

    func rename(_ workspaceID: String) {
        Task {
            guard let name = await TextPrompt.ask("Rename Workspace", text: workspace(workspaceID)?.name ?? "", action: "Rename"),
                  !name.trimmingCharacters(in: .whitespaces).isEmpty else { return }
            perform { client in try await client.renameWorkspace(workspaceID, name: name) }
        }
    }

    func remove(_ workspaceID: String) {
        Task {
            guard await Confirmation.ask(
                "Remove “\(workspace(workspaceID)?.name ?? "workspace")”?",
                detail: "Its terminals, editors and agents will stop, and it will be removed from Clubmate.",
                action: "Remove",
                destructive: true
            ) else { return }
            perform { client in try await client.deleteWorkspace(workspaceID) }
        }
    }

    private func perform(_ action: @escaping (CoreClient) async throws -> Void) {
        guard let client = supervisor.client else {
            return AppAlert.warning("Clubmate is not connected to its core yet.")
        }
        Task {
            do {
                try await action(client)
            } catch {
                AppAlert.warning((error as? ClubmateError)?.message ?? "\(error)")
            }
        }
    }

    private static func chooseFolders(prompt: String) -> [String]? {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = true
        panel.showsHiddenFiles = true
        panel.prompt = prompt
        guard !TestMode.current, panel.runModal() == .OK, !panel.urls.isEmpty else { return nil }
        return panel.urls.map { $0.path(percentEncoded: false) }
    }
}
