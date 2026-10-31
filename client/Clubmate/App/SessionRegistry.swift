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

    func sync(_ layout: WindowLayout) {
        for workspace in layout.workspaces where sessions[workspace] == nil {
            _ = session(for: workspace)
        }
        for (workspace, session) in sessions where SurfaceDeckPolicy.shouldCloseDeck(workspace: workspace, layout: layout) {
            sessions[workspace] = nil
            session.deck.closeAll()
            Task { await session.close(supervisor) }
        }
    }

    func adopt(_ client: CoreClient) {
        for session in sessions.values {
            session.adopt(client, supervisor, settings)
        }
    }

    private func session(for projectID: String) -> ProjectSession {
        if let existing = sessions[projectID] { return existing }
        let session = ProjectSession(projectID: projectID) { [project] in project(projectID) }
        session.deck.fontSize = settings.terminalFontSize
        sessions[projectID] = session
        if let client = supervisor.client { session.adopt(client, supervisor, settings) }
        return session
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
