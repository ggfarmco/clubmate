import Foundation
import Observation

import ClubmateKit

@MainActor
final class SessionRegistry {
    private let supervisor: ConnectionSupervisor
    private let settings: AppSettings
    private let workspace: @MainActor (String) -> Workspace?
    private var sessions: [String: WorkspaceSession] = [:]

    init(supervisor: ConnectionSupervisor, settings: AppSettings, workspace: @escaping @MainActor (String) -> Workspace?) {
        self.supervisor = supervisor
        self.settings = settings
        self.workspace = workspace
        trackTerminalFontSize()
    }

    func existing(_ workspaceID: String) -> WorkspaceSession? {
        sessions[workspaceID]
    }

    var stores: [WorkspaceUIStore] {
        sessions.values.compactMap(\.store)
    }

    func sync(_ layout: WindowLayout) {
        for workspace in layout.workspaces where sessions[workspace] == nil {
            open(workspace, legacyChat: layout.takeLegacyChat(workspace))
        }
        for (workspace, session) in sessions where SurfaceDeckPolicy.shouldCloseDeck(workspace: workspace, layout: layout) {
            sessions[workspace] = nil
            session.deck.closeAll()
            Task { await session.close() }
        }
    }

    func adopt(_ client: CoreClient) {
        for session in sessions.values {
            adopt(session, client)
        }
    }

    private func open(_ workspaceID: String, legacyChat: LegacyChat?) {
        let session = WorkspaceSession(workspaceID: workspaceID, legacyChat: legacyChat) { [workspace] in workspace(workspaceID) }
        session.deck.fontSize = settings.terminalFontSize
        sessions[workspaceID] = session
        if let client = supervisor.client { adopt(session, client) }
    }

    private func adopt(_ session: WorkspaceSession, _ client: CoreClient) {
        guard let store = session.adopt(client, settings) else { return }
        supervisor.attach(store)
    }

    private func trackTerminalFontSize() {
        withObservationTracking { _ = settings.terminalFontSize } onChange: { [weak self] in
            Task { @MainActor in
                guard let self else { return }
                for session in self.sessions.values { session.deck.fontSize = self.settings.terminalFontSize }
                self.trackTerminalFontSize()
            }
        }
    }
}
