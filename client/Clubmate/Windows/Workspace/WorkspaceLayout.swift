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
        let column = store.columnSlot
        let narrow = reviewNarrow
        let stacked = column != nil && !narrow && CodeSection.stacksChat(width: Double(rowWidth))
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
                    VStack(spacing: 0) {
                        if narrow {
                            ReviewStrip(store: store)
                        }
                        SlotPane(store: store, slot: "\(section).main")
                            .modifier(MainFocus(store: store, slot: "\(section).main"))
                            .overlay(alignment: .top) {
                                if let failure = store.failure { BannerLabel(text: failure, edge: .bottom) }
                            }
                    }
                    .overlay(alignment: .leading) {
                        if narrow, let column {
                            ReviewDrawer(store: store, slot: column, width: drawerWidth)
                        }
                    }
                    if let sidebar, side == .right {
                        SidebarPane(store: store, slot: sidebar, side: .right)
                    }
                    if let column, !narrow {
                        CodeColumn(store: store, slot: column, stacked: stacked, width: columnWidth(limit: Double(columnLimit)), limit: columnLimit) { [store] width in
                            if column == CodeSection.reviewSlot { store.reviewWidth = width } else { store.chatWidth = width }
                        }
                    }
                }
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { rowWidth = $0 }
            .background {
                Group {
                    Button("Toggle Sidebar") { store.toggleSidebar() }
                        .keyboardShortcut(AppShortcuts.toggleSidebar, key: AppShortcuts.toggleSidebar.keys[1])
                    Button("Increase Font Size") { [panes, store] in app.stepFont(1, panes: panes, store: store) }
                        .keyboardShortcut(AppShortcuts.fontIncrease, key: AppShortcuts.fontIncrease.keys[1])
                }
                .opacity(0)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
            .frame(maxHeight: .infinity)
            .clipped()
            .overlay(alignment: voice?.dictating == true ? .top : .bottom) {
                if let voice {
                    VoicePanel(voice: voice) { [app, store] session in app.revealSession(session, in: store.workspaceID) }
                        .frame(width: VoicePanel.width(row: rowWidth))
                        .padding(voice.dictating ? .top : .bottom, theme.spacing.xl)
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
        .environment(\.voice, voice)
        .onChange(of: reviewLayout, initial: true) { _, layout in
            guard let layout else { return }
            Task { [store] in
                _ = await store.send(UIEvent(workspaceId: store.workspaceID, slot: CodeSection.reviewSlot, nodeId: "review", action: "layout", value: .string(layout)))
            }
        }
        .onChange(of: app.voiceSettings.isOn) { _, on in
            if !on { app.sessions.existing(store.workspaceID)?.voice.cancel() }
        }
    }

    private var columnLimit: CGFloat {
        rowWidth > 0 ? rowWidth / 2 : 640
    }

    private var editorArea: CGFloat {
        rowWidth - theme.rail - 1
    }

    private func columnWidth(limit: Double) -> CGFloat {
        let review = store.columnSlot == CodeSection.reviewSlot
        return CGFloat(CodeSection.columnWidth(saved: review ? store.reviewWidth : store.chatWidth, review: review, limit: limit))
    }

    private var editorWidth: Double {
        CodeSection.editorWidth(row: Double(rowWidth), rail: Double(theme.rail), column: store.columnShown ? Double(columnWidth(limit: Double(columnLimit))) : nil)
    }

    private var reviewNarrow: Bool {
        section == CodeSection.id && store.isOpen(CodeSection.reviewSlot) && CodeSection.reviewNarrow(width: editorWidth)
    }

    private var reviewLayout: String? {
        guard rowWidth > 0, section == CodeSection.id, store.isOpen(CodeSection.reviewSlot) else { return nil }
        return CodeSection.reviewLayout(width: editorWidth)
    }

    private var drawerWidth: CGFloat {
        min(columnWidth(limit: .infinity), max(CGFloat(CodeSection.columnMinWidth), editorArea - theme.spacing.overlay))
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
