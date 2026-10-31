import SwiftUI

import ClubmateKit

struct WorkspaceWindow: View {
    @Binding var windowID: String?

    @Environment(AppModel.self) var app
    @Environment(WindowRouter.self) var windows
    @Environment(\.theme) var theme
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        content
            .frame(minWidth: 800, minHeight: 500)
            .background(theme.background, ignoresSafeAreaEdges: .all)
            .navigationTitle(title)
            .toolbar(removing: worktreeSession == nil ? nil : .title)
            .overlay {
                if let windowID, windows.switcherWindow == windowID {
                    SwitcherGridView(windowID: windowID)
                }
            }
            .toolbar { toolbarItems }
            .modifier(WorkspacePresenters(windowID: $windowID))
            .focusedSceneValue(\.workspaceWindowID, windowID)
            .background(WindowObserver { [app, windowID] in
                if let windowID { windows.windowBecameKey(windowID) }
            } onAttach: { [app, windowID] window in
                if let windowID { windows.windowAttached(windowID, window) }
            } onClose: { [app, windowID] in
                if let windowID { windows.windowClosed(windowID) }
            })
            .onAppear {
                app.launch(openWindow)
                if let windowID, windows.window(windowID) != nil {
                    windows.windowShown(windowID)
                } else {
                    windowID = windows.claimWindow()
                }
            }
    }

    var active: String? {
        windowID.flatMap { windows.window($0)?.active }
    }

    var worktreeSession: WorkspaceSession? {
        guard let session = active.flatMap(app.sessions.existing), !session.worktrees.worktrees.isEmpty else { return nil }
        return session
    }

    var title: String {
        guard let active else { return "Clubmate" }
        let name = app.workspace(active)?.name ?? active
        return app.sessions.existing(active)?.worktrees.title(name) ?? name
    }

    @ViewBuilder private var content: some View {
        if let windowID, let active, let session = app.sessions.existing(active) {
            WorkspaceView(session: session, windowID: windowID)
                .id(ObjectIdentifier(session))
        } else {
            WelcomeView(windowID: windowID)
        }
    }
}
