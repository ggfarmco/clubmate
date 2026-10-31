import SwiftUI

import ClubmateKit

struct WorkspaceWindow: View {
    @Binding var windowID: String?

    @Environment(AppModel.self) var app
    @Environment(\.theme) var theme
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        content
            .frame(minWidth: 800, minHeight: 500)
            .background(theme.background, ignoresSafeAreaEdges: .all)
            .navigationTitle(title)
            .toolbar(removing: worktreeSession == nil ? nil : .title)
            .overlay {
                if let windowID, app.switcherWindow == windowID {
                    SwitcherGridView(windowID: windowID)
                }
            }
            .toolbar { toolbarItems }
            .modifier(WorkspacePresenters(windowID: $windowID))
            .focusedSceneValue(\.workspaceWindowID, windowID)
            .background(WindowObserver { [app, windowID] in
                if let windowID { app.windowBecameKey(windowID) }
            } onAttach: { [app, windowID] window in
                if let windowID { app.windowAttached(windowID, window) }
            } onClose: { [app, windowID] in
                if let windowID { app.windowClosed(windowID) }
            })
            .onAppear {
                app.launch(openWindow)
                if let windowID, app.window(windowID) != nil {
                    app.windowShown(windowID)
                } else {
                    windowID = app.claimWindow()
                }
            }
    }

    var active: String? {
        windowID.flatMap { app.window($0)?.active }
    }

    var worktreeSession: ProjectSession? {
        guard let session = active.flatMap(app.existingSession), !session.worktrees.worktrees.isEmpty else { return nil }
        return session
    }

    var title: String {
        guard let active else { return "Clubmate" }
        let name = app.project(active)?.name ?? active
        return app.existingSession(for: active)?.worktrees.title(name) ?? name
    }

    @ViewBuilder private var content: some View {
        if let windowID, let active, let session = app.existingSession(for: active) {
            WorkspaceView(session: session, windowID: windowID)
                .id(ObjectIdentifier(session))
        } else {
            WelcomeView(windowID: windowID)
        }
    }
}
