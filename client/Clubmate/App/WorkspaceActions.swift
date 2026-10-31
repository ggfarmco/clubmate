import AppKit
import Foundation

import ClubmateKit

@MainActor
struct WorkspaceActions {
    let supervisor: ConnectionSupervisor
    let windows: WindowRouter
    let sessions: SessionRegistry
    let project: @MainActor (String) -> Project?

    func openFolder(in window: String?) {
        guard let folders = Self.chooseFolders(prompt: "Open") else { return }
        perform { client in
            let id = try await client.openFolders(folders)
            windows.open(id, in: window)
        }
    }

    func newHomeTerminal(in window: String?) {
        perform { client in
            let id = try await client.homeWorkspace()
            windows.open(id, in: window)
            let session = sessions.existing(id)
            session?.store?.show(section: TerminalsSection.id)
            session?.autoNewRequest += 1
        }
    }

    func addFolder(to projectID: String) {
        guard let folders = Self.chooseFolders(prompt: "Add") else { return }
        perform { client in
            for folder in folders {
                try await client.addRepo(projectID, path: folder)
            }
        }
    }

    func rename(_ projectID: String) {
        Task {
            guard let name = await TextPrompt.ask("Rename Workspace", text: project(projectID)?.name ?? "", action: "Rename"),
                  !name.trimmingCharacters(in: .whitespaces).isEmpty else { return }
            perform { client in try await client.renameProject(projectID, name: name) }
        }
    }

    func remove(_ projectID: String) {
        Task {
            guard await Confirmation.ask(
                "Remove “\(project(projectID)?.name ?? "workspace")”?",
                detail: "Its terminals, editors and agents will stop, and it will be removed from Clubmate.",
                action: "Remove",
                destructive: true
            ) else { return }
            perform { client in try await client.deleteProject(projectID) }
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
