import SwiftUI

import ClubmateKit

struct WorkspaceView: View {
    let session: WorkspaceSession
    let windowID: String

    @Environment(AppModel.self) private var app
    @Environment(WindowRouter.self) private var windows
    @Environment(WorkspaceActions.self) private var actions
    @State private var focus = FocusRouter()
    @State private var panes = PaneFocus()
    @State private var rowWidth: CGFloat = 0

    var body: some View {
        content
            .slotPopup(session)
            .background { VisibleSlotsWatch(store: session.store) { [app] in app.notifications.focusChanged() } }
            .environment(\.surfaceDeck, session.deck)
            .environment(\.focusRouter, focus)
            .environment(\.paneFocus, panes)
            .environment(\.sidebarSide, app.settings.sidebarSide)
            .environment(\.filePicker, FilePicker { [session] target in session.pickFile(target) })
            .eventBanner(session.banner)
            .commandPalette(session, entries: paletteEntries, run: perform)
            .testPresenter("palette", palettePresented)
            .filePicker(session)
            .fuzzyPicker(session)
            .focusedSceneValue(\.workspaceSession, session)
            .onChange(of: session.store?.focusRequest) { _, request in
                guard let request, !request.slot.isEmpty else { return }
                if request.slot.hasSuffix(Self.sidebarSuffix) {
                    session.store?.revealSidebar(String(request.slot.dropLast(Self.sidebarSuffix.count)))
                }
                focus.request(request.field ? request.slot + SidebarField.suffix : request.slot)
            }
            .onChange(of: session.store?.connected, initial: true) { _, connected in
                guard connected == true else { return }
                Task { await session.worktrees.load(retrying: true) }
                Task { [app, session] in await app.notifications.tally(session.workspaceID) }
            }
            .onChange(of: app.workspace(session.workspaceID)) {
                Task { await session.worktrees.load(retrying: true) }
            }
            .onChange(of: session.store?.selectedSection) {
                app.notifications.focusChanged()
            }
            .onChange(of: autoNewKey, initial: true) { old, new in
                let store = session.store
                let section = store?.sections.first { $0.id == new.section }
                let main = new.section.flatMap { store?.mainTree($0) }
                guard session.autoNew.fires(from: old, to: new, section: section, main: main) else { return }
                session.run(.new(section: new.section, commands: app.commands))
            }
            .onChange(of: sidebarKey) { old, new in
                guard let new, new.hidden, old?.section == new.section, old?.hidden == false else { return }
                focus.request("\(new.section).main")
            }
            .onChange(of: chatShown) { old, shown in
                guard old != nil, let shown else { return }
                focus.request(shown ? CodeSection.chatSlot : "\(CodeSection.id).main")
            }
    }

    private var chatShown: Bool? {
        guard let store = session.store, store.togglesChat else { return nil }
        return store.chatShown
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

    @ViewBuilder private var content: some View {
        if let store = session.store, let section = store.selectedSection {
            WorkspaceLayout(store: store, section: section, rowWidth: $rowWidth)
        } else {
            EmptyNodeView(text: placeholder)
        }
    }

    private var placeholder: String {
        if let store = session.store {
            if let failure = store.failure { return failure }
            if store.connected, store.sections.isEmpty { return "No plugins provide sections" }
        }
        switch app.phase {
        case .connecting: return "Starting core…"
        case .failed(let message): return message
        case .ready, .reconnecting: return "Connecting…"
        }
    }

    private var palettePresented: Binding<Bool> {
        Binding { [session] in session.overlay == .palette } set: { [session] shown in
            if shown { session.overlay = .palette } else { session.close(.palette) }
        }
    }

    private func paletteEntries() -> [PaletteEntry] {
        let workspaces = (windows.window(windowID)?.workspaces ?? []).map { PaletteWorkspace(id: $0, name: app.workspace($0)?.name ?? $0) }
        let sections = app.settings.ordered(session.store?.sections ?? [])
        return CommandPalette.entries(commands: app.commands, sections: sections, sidebar: true, chat: session.store?.togglesChat == true, workspaces: workspaces, active: session.workspaceID, worktrees: session.worktrees.worktrees)
    }

    private func perform(_ action: PaletteAction) {
        switch action {
        case .command(let id): session.runConfirmed(id, commands: app.commands)
        case .section(let id): session.store?.select(id)
        case .toggleSidebar: session.store?.toggleSidebar()
        case .openFolder: actions.openFolder(in: windowID)
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
        return [section.flatMap { store?.slot("\($0).sidebar")?.rev }, section.flatMap { store?.slot("\($0).main")?.rev }, store?.chatShown == true ? store?.slot(CodeSection.chatSlot)?.rev : nil]
    }

    var body: some View {
        Color.clear.onChange(of: revs) { changed() }
    }
}
