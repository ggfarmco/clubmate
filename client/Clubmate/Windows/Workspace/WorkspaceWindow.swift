import SwiftUI

import ClubmateKit

struct WorkspaceWindow: View {
    @Binding var windowID: String?

    @Environment(AppModel.self) var app
    @Environment(WindowRouter.self) var windows
    @Environment(\.theme) var theme
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        WindowMinimum(size: CGSize(width: 800, height: 500)) { content }
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
        return app.sessions.existing(active)?.worktrees.title(workspaceName) ?? workspaceName
    }

    var workspaceName: String {
        active.map { app.workspace($0)?.name ?? $0 } ?? "Clubmate"
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

private struct WindowMinimum: Layout {
    let size: CGSize

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        if let width = proposal.width, let height = proposal.height {
            return CGSize(width: max(width, size.width), height: max(height, size.height))
        }
        let fitted = subviews.first?.sizeThatFits(proposal) ?? .zero
        return CGSize(width: max(proposal.width ?? fitted.width, size.width), height: max(proposal.height ?? fitted.height, size.height))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for subview in subviews {
            subview.place(at: CGPoint(x: bounds.midX, y: bounds.midY), anchor: .center, proposal: ProposedViewSize(bounds.size))
        }
    }

    func explicitAlignment(of guide: HorizontalAlignment, in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGFloat? {
        nil
    }

    func explicitAlignment(of guide: VerticalAlignment, in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGFloat? {
        nil
    }
}
