import Foundation
import Observation

import ClubmateKit

@MainActor
final class SessionRegistry {
    private let supervisor: ConnectionSupervisor
    private let settings: AppSettings
    private let project: @MainActor (String) -> Project?
    private var sessions: [String: ProjectSession] = [:]

    init(supervisor: ConnectionSupervisor, settings: AppSettings, project: @escaping @MainActor (String) -> Project?) {
        self.supervisor = supervisor
        self.settings = settings
        self.project = project
        trackTerminalFontSize()
    }

    func existing(_ projectID: String) -> ProjectSession? {
        sessions[projectID]
    }

    var stores: [ProjectUIStore] {
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

    private func open(_ projectID: String, legacyChat: LegacyChat?) {
        let session = ProjectSession(projectID: projectID, legacyChat: legacyChat) { [project] in project(projectID) }
        session.deck.fontSize = settings.terminalFontSize
        sessions[projectID] = session
        if let client = supervisor.client { adopt(session, client) }
    }

    private func adopt(_ session: ProjectSession, _ client: CoreClient) {
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
