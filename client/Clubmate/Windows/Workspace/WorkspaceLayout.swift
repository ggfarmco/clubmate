import SwiftUI

import ClubmateKit

struct WorkspaceLayout: View {
    let store: WorkspaceUIStore
    let section: String
    @Binding var rowWidth: CGFloat

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.paneFocus) private var panes

    var body: some View {
        let side = app.settings.sidebarSide
        let sidebar = store.sidebarSlot
        let stacked = store.chatShown && CodeSection.stacksChat(width: Double(rowWidth))
        let content = stacked ? AnyLayout(VStackLayout(spacing: 0)) : AnyLayout(HStackLayout(spacing: 0))
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                Rail(
                    sections: app.settings.ordered(store.sections),
                    selected: section,
                    attention: (app.notifications.agents[store.workspaceID]?.needYou ?? 0) > 0 ? [AgentNotifier.section] : [],
                    select: { store.selectedSection = $0 },
                    move: { [app] in app.settings.moveSection($0, onto: $1, in: store.sections) },
                    resetOrder: app.settings.sectionOrder == nil ? nil : { [app] in app.settings.sectionOrder = nil }
                )
                theme.border.frame(width: 1)
                if let sidebar, side == .left {
                    SidebarPane(store: store, slot: sidebar, side: .left)
                }
                content {
                    SlotPane(store: store, slot: "\(section).main")
                        .modifier(MainFocus(store: store, slot: "\(section).main"))
                        .overlay(alignment: .top) {
                            if let failure = store.failure { BannerLabel(text: failure, edge: .bottom) }
                        }
                    if let sidebar, side == .right {
                        SidebarPane(store: store, slot: sidebar, side: .right)
                    }
                    if store.chatShown {
                        ChatPane(store: store, stacked: stacked, saved: store.chatWidth, limit: rowWidth > 0 ? rowWidth / 2 : 640) { [store] width in
                            store.chatWidth = width
                        }
                    }
                }
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { rowWidth = $0 }
            .background {
                Group {
                    Button("Toggle Sidebar") { store.toggleSidebar() }
                        .keyboardShortcut("s", modifiers: [.command, .control])
                    Button("Increase Font Size") { [panes, store] in app.stepFont(1, panes: panes, store: store) }
                        .keyboardShortcut("+")
                }
                .opacity(0)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
            .frame(maxHeight: .infinity)
            .clipped()
            .overlay(alignment: .bottom) {
                if let voice {
                    VoicePanel(voice: voice) { [app, store] session in app.revealSession(session, in: store.workspaceID) }
                        .frame(width: VoicePanel.width(row: rowWidth))
                        .padding(.bottom, theme.spacing.xl)
                }
            }
            StatusBar(voice: voice, connection: connection, agents: app.notifications.agents[store.workspaceID], usage: app.usage.usage, hints: panes?.hints) { [app] in
                app.retry()
            } trailing: {
                if store.hasSlot("\(section).status") {
                    SlotPane(store: store, slot: "\(section).status")
                }
            }
        }
        .onChange(of: app.voiceSettings.isOn) { _, on in
            if !on { app.sessions.existing(store.workspaceID)?.voice.cancel() }
        }
    }

    private var voice: VoiceController? {
        guard app.voiceSettings.isOn else { return nil }
        return app.sessions.existing(store.workspaceID)?.voice
    }

    private var connection: ConnectionStatus {
        switch app.phase {
        case .ready: store.connected ? .connected : .connecting
        case .connecting: .connecting
        case .reconnecting(let attempt): .reconnecting(attempt: attempt)
        case .failed(let message): .failed(message)
        }
    }
}
