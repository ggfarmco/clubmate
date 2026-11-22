import SwiftUI

import ClubmateKit

struct WorkspaceView: View {
    let session: WorkspaceSession
    let windowID: String

    @Environment(AppModel.self) private var app
    @Environment(WindowRouter.self) private var windows
    @Environment(WorkspaceActions.self) private var actions
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var focus = FocusRouter()
    @State private var rowWidth: CGFloat = 0
    @State private var handOver: StartupHandOver
    @State private var watch = StartupWatch()

    init(session: WorkspaceSession, windowID: String) {
        self.session = session
        self.windowID = windowID
        _handOver = State(initialValue: StartupHandOver(skeleton: !session.ready))
    }

    var body: some View {
        content
            .slotPopup(session)
            .background { VisibleSlotsWatch(store: session.store) { [app] in app.notifications.focusChanged() } }
            .environment(\.surfaceDeck, session.deck)
            .environment(\.webViewDeck, session.webDeck)
            .environment(\.focusRouter, focus)
            .environment(\.paneFocus, session.panes)
            .environment(\.sidebarSide, app.settings.sidebarSide)
            .environment(\.filePicker, FilePicker { [session] target in session.pickFile(target) })
            .eventBanner(session.banner)
            .commandPalette(session, entries: paletteEntries, run: perform)
            .testPresenter("palette", palettePresented)
            .filePicker(session)
            .fuzzyPicker(session)
            .worktreePicker(session) { [actions, session] in actions.addFolder(to: session.workspaceID) }
            .focusedSceneValue(\.workspaceSession, session)
            .onChange(of: session.store?.focusRequest) { _, request in
                guard let request, !request.slot.isEmpty else { return }
                session.closeOverlay()
                if let section = ChatColumn.section(ofSlot: request.slot), session.store?.hasSlot(request.slot) == true {
                    session.store?.revealChat(section)
                } else if request.slot.hasSuffix(Self.sidebarSuffix) {
                    session.store?.revealSidebar(String(request.slot.dropLast(Self.sidebarSuffix.count)))
                }
                focus.request(request.field ? request.slot + SidebarField.suffix : request.slot, node: request.node)
            }
            .onChange(of: session.store?.connected, initial: true) { _, connected in
                guard connected == true else { return }
                Task { await session.worktrees.load(retrying: true) }
                Task { [app, session] in await app.notifications.tally(session.workspaceID) }
            }
            .onChange(of: app.workspace(session.workspaceID)) {
                Task { await session.worktrees.load(retrying: true) }
            }
            .task(id: watching) {
                guard watching else { return }
                await watch.run()
            }
            .task(id: session.ready) {
                guard session.ready else { return }
                if panesPending { try? await Task.sleep(for: StartupHandOver.group) }
                handOver.finish(reduceMotion: reduceMotion)
            }
            .onChange(of: panesPending) { _, pending in
                if session.ready, !pending { handOver.finish(reduceMotion: reduceMotion) }
            }
            .onChange(of: session.store?.selectedSection) {
                session.closeOverlay()
                app.notifications.focusChanged()
            }
            .onChange(of: session.panes.slot) { _, slot in
                if let slot { session.store?.rememberFocus(slot) }
            }
            .onChange(of: autoNewKey, initial: true) { old, new in
                let store = session.store
                let section = store?.sections.first { $0.id == new.section }
                let main = new.section.flatMap { store?.mainTree($0) }
                let terminals = new.section == TerminalsSection.id
                guard session.autoNew.fires(from: old, to: new, section: section, main: main) else {
                    if terminals, main != nil { session.clearAutoNewArgs() }
                    return
                }
                session.run(.new(section: new.section, commands: app.commands, args: terminals ? session.takeAutoNewArgs() : .null))
            }
            .onChange(of: sidebarKey) { old, new in
                guard let new, new.hidden, old?.section == new.section, old?.hidden == false else { return }
                focus.request("\(new.section).main")
            }
            .onChange(of: columnKey) { old, new in
                guard let old, let new, old.section == new.section, old.shown != new.shown else { return }
                focus.request(new.shown ? ChatColumn.slot(for: new.section) : "\(new.section).main")
            }
    }

    private struct ColumnKey: Equatable {
        let section: String
        let shown: Bool
    }

    private var columnKey: ColumnKey? {
        guard let store = session.store, let section = store.selectedSection, store.togglesChat else { return nil }
        return ColumnKey(section: section, shown: store.columnShown)
    }

    private static let sidebarSuffix = ".sidebar"

    private struct SidebarKey: Equatable {
        let section: String
        let hidden: Bool
    }

    private var sidebarKey: SidebarKey? {
        guard let store = session.store, let section = store.selectedSection else { return nil }
        return SidebarKey(section: section, hidden: store.sidebarSlot == nil)
    }

    private var autoNewKey: AutoNewGate.Key {
        let section = session.store?.selectedSection
        return AutoNewGate.Key(section: section, loaded: section.map { session.store?.hasSlot("\($0).main") == true } ?? false, request: session.autoNewRequest)
    }

    private var content: some View {
        ZStack {
            if let store = session.store, let section = store.selectedSection {
                WorkspaceLayout(store: store, section: section, rowWidth: $rowWidth)
            } else if let placeholder {
                EmptyNodeView(text: placeholder)
            }
            if handOver.skeleton, placeholder == nil {
                StartupSkeleton(record: session.launchRecord, selected: session.launchSection, note: watch.label(app.startWait), showsLog: watch.showsLog, failure: startFailure) { [session] section in
                    session.selectSection(section)
                }
                .allowsHitTesting(!session.ready)
                .transition(.opacity)
            }
        }
    }

    private var placeholder: String? {
        guard !session.ready else { return nil }
        if let store = session.store {
            if let failure = store.failure { return failure }
            if store.connected, store.sections.isEmpty { return "No plugins provide sections" }
        }
        return nil
    }

    private var startFailure: String? {
        session.ready ? nil : app.startFailure
    }

    private var watching: Bool {
        !session.ready && startFailure == nil
    }

    private var panesPending: Bool {
        guard let store = session.store, let section = store.selectedSection else { return false }
        return [store.sidebarSlot, "\(section).main", store.columnSlot].compactMap(\.self).contains { store.launchKind($0) != nil }
    }

    private var palettePresented: Binding<Bool> {
        Binding { [session] in session.overlay == .palette } set: { [session] shown in
            if shown { session.overlay = .palette } else { session.close(.palette) }
        }
    }

    private func paletteEntries() -> [PaletteEntry] {
        let workspaces = (windows.window(windowID)?.workspaces ?? []).map { PaletteWorkspace(id: $0, name: app.workspace($0)?.name ?? $0, home: app.workspace($0)?.home == true) }
        let sections = app.settings.ordered(session.store?.sections ?? [])
        return CommandPalette.entries(commands: app.commands, sections: sections, sidebar: session.store?.togglesSidebar == true, chat: session.store?.togglesChat == true, workspaces: workspaces, active: session.workspaceID, worktrees: session.worktrees.worktrees)
    }

    private func perform(_ action: PaletteAction) {
        switch action {
        case .command(let id): session.runConfirmed(id, commands: app.commands)
        case .section(let id): session.store?.select(id)
        case .toggleSidebar: session.store?.toggleSidebar()
        case .toggleChat: session.store?.toggleChat()
        case .openFolder: actions.openFolder(in: windowID)
        case .openHome: actions.openHome(in: windowID)
        case .newHomeTerminal: actions.newHomeTerminal(in: windowID)
        case .renameWorkspace: actions.rename(session.workspaceID)
        case .closeWorkspace: windows.closeWorkspace(session.workspaceID, in: windowID)
        case .removeWorkspace: actions.remove(session.workspaceID)
        case .moveWorkspaceToNewWindow: windows.moveToNewWindow(session.workspaceID)
        case .switchWorkspace(let id): windows.activate(id, in: windowID)
        case .switchWorktree: session.chooseWorktree()
        case .newWorktree: session.newWorktree(chooseFolder: true)
        case .removeWorktree: session.chooseWorktreeToRemove()
        case .openDoc: session.chooseDoc()
        }
    }
}

private struct VisibleSlotsWatch: View {
    let store: WorkspaceUIStore?
    let changed: () -> Void

    private var revs: [Int64?] {
        let section = store?.selectedSection
        return [section.flatMap { store?.slot("\($0).sidebar")?.rev }, section.flatMap { store?.slot("\($0).main")?.rev }, store?.columnSlot.flatMap { store?.slot($0)?.rev }]
    }

    var body: some View {
        Color.clear.onChange(of: revs) { changed() }
    }
}
