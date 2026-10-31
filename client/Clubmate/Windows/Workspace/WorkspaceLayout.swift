import SwiftUI

import ClubmateKit

struct WorkspaceLayout: View {
    let store: ProjectUIStore
    let section: String
    let windowID: String
    let chatShown: Bool
    @Binding var rowWidth: CGFloat

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        let side = app.settings.sidebarSide
        let sidebar = store.sidebarSlot
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                Rail(
                    sections: app.settings.ordered(store.sections),
                    selected: section,
                    select: { store.selectedSection = $0 },
                    move: { [app] in app.settings.moveSection($0, onto: $1, in: store.sections) },
                    resetOrder: app.settings.sectionOrder == nil ? nil : { [app] in app.settings.sectionOrder = nil }
                )
                theme.border.frame(width: 1)
                if let sidebar, side == .left {
                    SidebarPane(store: store, slot: sidebar, side: .left)
                }
                SlotPane(store: store, slot: "\(section).main")
                    .modifier(MainFocus(slot: "\(section).main", surface: store.slots["\(section).main"]?.tree.surfaceSessionID))
                    .overlay(alignment: .top) {
                        if let failure = store.failure { BannerLabel(text: failure, edge: .bottom) }
                    }
                if let sidebar, side == .right {
                    SidebarPane(store: store, slot: sidebar, side: .right)
                }
                if chatShown {
                    ChatPane(store: store, saved: app.window(windowID)?.chatWidth, limit: rowWidth > 0 ? rowWidth / 2 : 640) { [app, windowID] width in
                        app.setChatWidth(width, in: windowID)
                    }
                }
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { rowWidth = $0 }
            .background {
                Group {
                    Button("Toggle Sidebar") { app.toggleSidebar(store, in: windowID) }
                        .keyboardShortcut("s", modifiers: [.command, .control])
                    Button("Increase Font Size") { app.stepTerminalFont(1) }
                        .keyboardShortcut("+")
                }
                .opacity(0)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
            .frame(maxHeight: .infinity)
            .clipped()
            StatusBar(connection: connection, section: store.sections.first { $0.id == section }?.title ?? section) { [app] in
                app.retry()
            } trailing: {
                if store.slots["\(section).status"] != nil {
                    SlotPane(store: store, slot: "\(section).status")
                }
            }
        }
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
