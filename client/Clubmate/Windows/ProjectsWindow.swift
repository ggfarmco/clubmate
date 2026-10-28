import SwiftUI

import ClubmateKit

struct ProjectsWindow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.openWindow) private var openWindow
    @Environment(\.dismissWindow) private var dismissWindow

    private var session: ProjectSession { app.projectsSession }

    var body: some View {
        content
            .frame(minWidth: 480, minHeight: 360)
            .background(theme.background, ignoresSafeAreaEdges: .all)
            .eventBanner(session.banner)
            .commandPalette(session, entries: { CommandPalette.entries(commands: [], sections: [], sidebar: false) }) { _ in
                openWindow(id: "projects")
            }
            .focusedSceneValue(\.projectSession, session)
            .onAppear { app.launch(openWindow) }
            .onChange(of: app.launchesProject) { launching, _ in
                if launching { dismissWindow(id: "projects") }
            }
    }

    @ViewBuilder private var content: some View {
        switch app.phase {
        case .connecting:
            EmptyNodeView(text: "Starting core…")
        case .failed(let message):
            VStack(spacing: theme.gutter * 1.5) {
                Text(message)
                    .font(theme.ui)
                    .foregroundStyle(theme.danger)
                    .multilineTextAlignment(.center)
                    .textSelection(.enabled)
                Button("Retry") { app.retry() }
            }
            .padding(theme.gutter * 3)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .ready, .reconnecting:
            if let store = session.store {
                SlotPane(store: store, slot: "projects.main")
                    .padding(theme.gutter)
                    .overlay(alignment: .top) {
                        if let failure = store.failure { BannerLabel(text: failure, edge: .bottom) }
                    }
            } else {
                EmptyNodeView(text: "Connecting…")
            }
        }
    }
}
