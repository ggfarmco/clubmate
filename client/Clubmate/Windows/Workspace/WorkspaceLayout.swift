import AppKit
import SwiftUI

import ClubmateDocument
import ClubmateKit

struct WorkspaceLayout: View {
    let store: WorkspaceUIStore
    let section: String
    @Binding var rowWidth: CGFloat

    @State private var tourWidth: Double?

    @Environment(AppModel.self) private var app
    @Environment(WindowRouter.self) private var windows
    @Environment(\.theme) private var theme
    @Environment(\.paneFocus) private var panes
    @Environment(\.focusRouter) private var router
    @Environment(\.openSettings) private var openSettings

    var body: some View {
        let side = app.settings.sidebarSide
        let sidebar = store.sidebarSlot
        let column = store.columnSlot
        let stacked = column != nil && ChatColumn.stacksChat(width: Double(rowWidth))
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
                if let sidebar, side == .right, !stacked {
                    mainPane
                    SidebarPane(store: store, slot: sidebar, side: .right)
                    chatColumn(column, stacked: false)
                } else {
                    content {
                        mainPane
                        chatColumn(column, stacked: stacked)
                    }
                    if let sidebar, side == .right {
                        SidebarPane(store: store, slot: sidebar, side: .right)
                    }
                }
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { rowWidth = $0 }
            .background {
                Group {
                    Button("Increase Font Size") { [panes, store] in app.stepFont(1, panes: panes, store: store) }
                        .keyboardShortcut(AppShortcuts.fontIncrease, key: AppShortcuts.fontIncrease.keys[1])
                }
                .opacity(0)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
            .background {
                if section == CodeSection.id, store.isOpen(CodeSection.reviewSlot) {
                    SlotPane(store: store, slot: CodeSection.reviewSlot)
                        .frame(width: 0, height: 0)
                        .clipped()
                }
                if store.isOpen(AgentsSection.sheetSlot) {
                    SlotPane(store: store, slot: AgentsSection.sheetSlot)
                        .frame(width: 0, height: 0)
                        .clipped()
                }
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
            StatusBar(voice: voice, connection: connection, agents: app.notifications.agents[store.workspaceID], usage: app.usage.usage, awake: app.awake, hints: panes?.hints, mode: panes?.mode, note: panes?.note, pairPhone: pairPhone) { [app] in
                app.retry()
            } awakeSettings: { [app, openSettings] in
                app.settingsRequest = .plugin("awake")
                openSettings()
            } openSession: { [app, store] session, workspace in
                app.revealSession(session, in: workspace.isEmpty ? store.workspaceID : workspace)
            } trailing: {
                if store.hasSlot("\(section).status") {
                    SlotPane(store: store, slot: "\(section).status", paneTheme: theme.scaled(theme.uiSize - 1))
                }
            }
        }
        .environment(\.voice, voice)
        .modifier(AgentsKeysSink(focus: agentsFocus))
        .onChange(of: reviewLayout, initial: true) { _, layout in
            guard let layout else { return }
            Task { [store] in
                _ = await store.send(UIEvent(workspaceId: store.workspaceID, slot: CodeSection.reviewSlot, nodeId: "review", action: "layout", value: .string(layout)))
            }
        }
        .onChange(of: section) { mainFind?.hide() }
        .onChange(of: app.voiceSettings.isOn) { _, on in
            if !on { app.sessions.existing(store.workspaceID)?.voice.cancel() }
        }
    }

    private var agentsFocus: AgentsFocus? {
        guard AgentsKeys.steps(section: section), let panes, let router else { return nil }
        return AgentsFocus(store: store, panes: panes, router: router, side: app.settings.sidebarSide, commands: app.commands)
    }

    private var pairPhone: (() -> Void)? {
        guard app.remoteDevices.status?.companion?.state == .dismissed, let window = windows.window(holding: store.workspaceID) else { return nil }
        return { [app] in Task { await app.remoteDevices.pair(in: window) } }
    }

    @ViewBuilder private func chatColumn(_ slot: String?, stacked: Bool) -> some View {
        if let slot {
            ChatColumnView(store: store, slot: slot, stacked: stacked, width: columnWidth(limit: Double(columnLimit)), limit: columnLimit) { [store] width in
                store.chatWidth = width
            }
        }
    }

    private var mainPane: some View {
        let stacked = asideVisible && ChatColumn.stacksChat(width: Double(rowWidth))
        let split = stacked ? AnyLayout(VStackLayout(spacing: 0)) : AnyLayout(HStackLayout(spacing: 0))
        return split {
            SlotPane(store: store, slot: "\(section).main")
                .background(PaneMarker(slot: "\(section).main"))
                .environment(\.chatColumnShown, store.columnShown)
                .environment(\.documentFind, mainFind)
                .environment(\.guidePaneShown, guidePaneShown)
                .keyMonitor(active: pageFindActive, handle: handlePageFind)
                .modifier(MainFocus(store: store, slot: "\(section).main"))
                .overlay(alignment: .top) {
                    if let failure = store.failure { BannerLabel(text: failure, edge: .bottom) }
                }
                .frame(maxHeight: .infinity)
            if asideVisible {
                AsideColumn(store: store, stacked: stacked, width: asideWidth, limit: asideLimit) { [store] width in
                    if GuideTour.isTour(asideTree) { tourWidth = width } else { store.asideWidth = width }
                }
            }
        }
    }

    private var mainFind: DocumentFind? {
        app.sessions.existing(store.workspaceID)?.mainFind
    }

    private var pageFindActive: Bool {
        guard let session = app.sessions.existing(store.workspaceID), session.overlay == nil else { return false }
        guard store.popup == nil || session.popupPoppedOut || session.popupHidden else { return false }
        return store.slot("\(section).main")?.tree.hasPageDocument == true
    }

    private func handlePageFind(_ event: NSEvent) -> Bool {
        guard let find = mainFind else { return false }
        let flags = event.modifierFlags
        let key = PopupKeys.pageFindKey(
            keyCode: event.keyCode, characters: event.charactersIgnoringModifiers,
            command: flags.contains(.command), control: flags.contains(.control), option: flags.contains(.option), shift: flags.contains(.shift),
            finding: find.visible
        )
        if key != .find, !Self.focusIn("\(section).main", event.window) { return false }
        switch key {
        case .find: find.show()
        case .findNext: Task { await find.next() }
        case .findPrevious: Task { await find.previous() }
        case .closeFind:
            find.hide()
            router?.request("\(section).main")
        default: return false
        }
        return true
    }

    private static func focusIn(_ slot: String, _ window: NSWindow?) -> Bool {
        guard let window else { return false }
        let responder = window.firstResponder
        if responder == nil || responder === window || responder === window.contentView || responder === window.contentView?.superview { return true }
        return PaneMarker.holds(slot, responder)
    }

    private var asideLimit: CGFloat {
        max(AsideColumn.minWidth, mainWidth - AsideColumn.minWidth)
    }

    private var mainWidth: CGFloat {
        let sidebar = store.sidebarSlot == nil ? 0 : CGFloat(store.sidebarWidth ?? 248) + 1
        return max(0, rowWidth - theme.rail - 1 - sidebar)
    }

    private var asideTree: SDUINode? {
        store.slot(AgentsSection.asideSlot)?.tree
    }

    private var asideVisible: Bool {
        GuideTour.asideVisible(shown: store.asideShown, aside: asideTree, main: Double(mainWidth))
    }

    private var guidePaneShown: Bool {
        GuideTour.paneShown(shown: store.asideShown, aside: asideTree, main: Double(mainWidth))
    }

    private var asideWidth: CGFloat {
        let fallback = GuideTour.isTour(asideTree) ? AsideColumn.minWidth + 40 : mainWidth / 2
        let saved = GuideTour.isTour(asideTree) ? tourWidth : store.asideWidth
        return min(max(saved.map { CGFloat($0) } ?? fallback, AsideColumn.minWidth), asideLimit)
    }

    private var columnLimit: CGFloat {
        rowWidth > 0 ? rowWidth / 2 : 640
    }

    private func columnWidth(limit: Double) -> CGFloat {
        CGFloat(ChatColumn.width(saved: store.chatWidth, limit: limit))
    }

    private var editorWidth: Double {
        CodeSection.editorWidth(row: Double(rowWidth), rail: Double(theme.rail), column: store.columnShown ? Double(columnWidth(limit: Double(columnLimit))) : nil)
    }

    private var reviewLayout: String? {
        guard rowWidth > 0, section == CodeSection.id, store.connected, store.hasSlot(CodeSection.reviewSlot) else { return nil }
        return CodeSection.reviewLayout(width: editorWidth)
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
