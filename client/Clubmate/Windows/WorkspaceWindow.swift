import AppKit
import SwiftUI

import ClubmateKit

struct WorkspaceWindow: View {
    @Binding var windowID: String?

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
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
            .toolbar {
                if let worktreeSession {
                    ToolbarItem(placement: .navigation) {
                        WorktreeTitleMenu(session: worktreeSession, title: title)
                    }
                }
                ToolbarSpacer(.flexible)
                ToolbarItem(placement: .primaryAction) {
                    Button { windowID.map(app.toggleSwitcher) } label: {
                        Text("▦ \(workspaceCount)")
                            .monospacedDigit()
                            .padding(theme.spacing.capsule)
                    }
                    .help("Workspaces (⌃⌘W)")
                    .accessibilityLabel("Workspaces, \(workspaceCount)")
                    .disabled(windowID == nil)
                }
                ToolbarSpacer(.fixed, placement: .primaryAction)
            }
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

    private var active: String? {
        windowID.flatMap { app.window($0)?.active }
    }

    private var workspaceCount: Int {
        windowID.flatMap { app.window($0)?.workspaces.count } ?? 0
    }

    private var worktreeSession: ProjectSession? {
        guard let session = active.flatMap(app.existingSession), !session.worktrees.worktrees.isEmpty else { return nil }
        return session
    }

    private var title: String {
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

private struct WorkspaceView: View {
    let session: ProjectSession
    let windowID: String

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @State private var focus = FocusRouter()

    var body: some View {
        content
            .environment(\.surfaceDeck, session.deck)
            .environment(\.focusRouter, focus)
            .environment(\.sidebarSide, app.settings.sidebarSide)
            .eventBanner(session.banner)
            .commandPalette(session, entries: paletteEntries, run: perform)
            .goToFile(session)
            .findInFiles(session)
            .fuzzyPicker(session)
            .focusedSceneValue(\.projectSession, session)
            .onChange(of: session.store?.focusRequest) { _, request in
                guard let request, !request.slot.isEmpty, request.accepts(side: app.settings.sidebarSide) else { return }
                if request.slot.hasSuffix(Self.sidebarSuffix) {
                    session.store?.revealSidebar(String(request.slot.dropLast(Self.sidebarSuffix.count)))
                }
                focus.request(request.slot)
            }
            .onChange(of: session.store?.connected, initial: true) { _, connected in
                guard connected == true else { return }
                app.syncNavigationKeys()
                Task { await session.worktrees.load(retrying: true) }
            }
            .onChange(of: app.project(session.projectID)) {
                Task { await session.worktrees.load(retrying: true) }
            }
            .onChange(of: session.store?.selectedSection, initial: true) { _, section in
                if section == CodeSection.id { app.askNavigationKeys() }
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
            .onChange(of: app.settings.autoHideSidebar) { _, enabled in
                if !enabled { session.store?.sidebarAutoHidden = false }
            }
    }

    private static let sidebarSuffix = ".sidebar"

    private struct SidebarKey: Equatable {
        let section: String
        let hidden: Bool
    }

    private var sidebarKey: SidebarKey? {
        guard let store = session.store, let section = store.selectedSection else { return nil }
        return SidebarKey(section: section, hidden: app.settings.hidesSidebar(section, manual: store.sidebarHidden[section] == true, autoHidden: store.sidebarAutoHidden))
    }

    private var autoNewKey: AutoNewGate.Key {
        let section = session.store?.selectedSection
        return AutoNewGate.Key(section: section, loaded: section.flatMap { session.store?.mainTree($0) } != nil, request: session.autoNewRequest)
    }

    @ViewBuilder private var content: some View {
        if let store = session.store, let section = store.selectedSection {
            let side = app.settings.sidebarSide
            let sidebar = sidebarKey?.hidden == true ? nil : side
            VStack(spacing: 0) {
                HStack(spacing: 0) {
                    Rail(sections: store.sections, selected: section) { store.selectedSection = $0 }
                    theme.border.frame(width: 1)
                    if sidebar == .left {
                        SidebarPane(session: session, store: store, section: section, side: .left)
                    }
                    SlotPane(store: store, slot: "\(section).main")
                        .modifier(MainFocus(slot: "\(section).main", surface: store.slots["\(section).main"]?.tree.surfaceSessionID) {
                            if app.settings.autoHides(section) { store.sidebarAutoHidden = true }
                        })
                        .overlay(alignment: .top) {
                            if let failure = store.failure { BannerLabel(text: failure, edge: .bottom) }
                        }
                    if sidebar == .right {
                        SidebarPane(session: session, store: store, section: section, side: .right)
                    }
                }
                .background {
                    Group {
                        Button("Toggle Sidebar") { store.toggleSidebar(app.settings) }
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
                StatusBar(connection: connection(store), section: store.sections.first { $0.id == section }?.title ?? section) { [app] in
                    app.retry()
                } trailing: {
                    if store.slots["\(section).status"] != nil {
                        SlotPane(store: store, slot: "\(section).status")
                    }
                }
            }
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

    private func connection(_ store: ProjectUIStore) -> ConnectionStatus {
        switch app.phase {
        case .ready: store.connected ? .connected : .connecting
        case .connecting: .connecting
        case .reconnecting(let attempt): .reconnecting(attempt: attempt)
        case .failed(let message): .failed(message)
        }
    }

    private func paletteEntries() -> [PaletteEntry] {
        let workspaces = (app.window(windowID)?.workspaces ?? []).map { PaletteWorkspace(id: $0, name: app.project($0)?.name ?? $0) }
        return CommandPalette.entries(commands: app.commands, sections: session.store?.sections ?? [], sidebar: true, workspaces: workspaces, active: session.projectID, worktrees: session.worktrees.worktrees)
    }

    private func perform(_ action: PaletteAction) {
        switch action {
        case .command(let id): confirm(id)
        case .section(let id): session.store?.selectedSection = id
        case .toggleSidebar: session.store?.toggleSidebar(app.settings)
        case .toggleNavigationKeys: app.settings.navigationKeys = !(app.settings.navigationKeys ?? false)
        case .openFolder: app.openFolder(in: windowID)
        case .newHomeTerminal: app.newHomeTerminal(in: windowID)
        case .renameWorkspace: app.rename(session.projectID)
        case .closeWorkspace: app.closeWorkspace(session.projectID, in: windowID)
        case .removeWorkspace: app.remove(session.projectID)
        case .moveWorkspaceToNewWindow: app.moveToNewWindow(session.projectID)
        case .switchWorkspace(let id): app.activate(id, in: windowID)
        case .switchWorktree: session.chooseWorktree()
        case .newWorktree: session.newWorktree(chooseFolder: true)
        case .removeWorktree: session.chooseWorktreeToRemove()
        case .review: session.chooseReviewBase(head: session.worktrees.current?.path)
        }
    }

    private func confirm(_ id: String) {
        let message = app.commands.first { $0.id == id }?.confirm ?? ""
        guard !message.isEmpty else { return run(id) }
        Task {
            if await Confirmation.ask(message, action: "Run") { run(id) }
        }
    }

    private func run(_ id: String) {
        guard id == NvimRestart.command, let store = session.store else { return session.run(CommandInvocation(id: id)) }
        let name = app.project(session.projectID)?.name ?? session.projectID
        let banner = session.banner
        Task {
            let outcome = await NvimRestart.run(workspace: name, run: { await store.run($0) }) { message in
                await Confirmation.ask(message, action: "Restart", destructive: true)
            }
            if case .failed(let message) = outcome { banner.show(message) }
        }
    }
}

private struct WorktreeTitleMenu: View {
    let session: ProjectSession
    let title: String

    @State private var shown = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button { shown.toggle() } label: {
            Text(title)
                .font(.headline)
                .padding(theme.spacing.capsule)
                .onHover { inside in
                    if inside { session.worktreeMenuOpened() }
                }
        }
        .buttonStyle(.plain)
        .fixedSize()
        .help("Worktrees")
        .popover(isPresented: $shown, arrowEdge: .bottom) {
            WorktreeSwitcher(session: session) { shown = false }
        }
    }
}

private struct WorktreeSwitcher: View {
    let session: ProjectSession
    let close: () -> Void

    @State private var model: FuzzyPickerModel
    @Environment(\.theme) private var theme

    init(session: ProjectSession, close: @escaping () -> Void) {
        self.session = session
        self.close = close
        _model = State(initialValue: FuzzyPickerModel(placeholder: "Switch to worktree or branch", source: session.worktrees.switcherSource))
    }

    var body: some View {
        let worktrees = session.worktrees
        VStack(spacing: 0) {
            FuzzyPickerList(model: model, choose: switchTo, dismiss: close)
            theme.border.frame(height: 1)
            VStack(spacing: 0) {
                action(worktrees.reviewLabel) { session.review(base: nil) }
                action("Review…") { session.chooseReviewBase(head: worktrees.current?.path) }
                action("New Worktree…") { session.newWorktree(chooseFolder: false) }
                if !worktrees.linked.isEmpty {
                    action("Remove Worktree…") { session.chooseWorktreeToRemove() }
                }
            }
            .padding(.vertical, theme.spacing.s)
        }
        .frame(width: 360)
        .background(theme.background)
        .task {
            await worktrees.load()
            model.update(model.query)
            await worktrees.loadBranches()
            model.update(model.query)
        }
    }

    private func switchTo(_ item: PickerItem) {
        session.pickWorktree(item)
    }

    private func action(_ title: String, perform: @escaping () -> Void) -> some View {
        SwitcherAction(title: title) {
            close()
            perform()
        }
    }
}

private struct SwitcherAction: View {
    let title: String
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(theme.ui)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, theme.gutter * 1.5)
                .frame(height: theme.rowHeight + 2)
                .background(hovering ? theme.foreground.opacity(0.06) : .clear, ignoresSafeAreaEdges: [])
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}

private struct MainFocus: ViewModifier {
    let slot: String
    let surface: String?
    let claimed: () -> Void

    @FocusState private var focused: Bool
    @Environment(\.focusRouter) private var router
    @Environment(\.surfaceDeck) private var deck

    func body(content: Content) -> some View {
        content
            .focusable(surface == nil)
            .focusEffectDisabled()
            .focused($focused)
            .onChange(of: router?.pending, initial: true) { claim() }
            .onChange(of: slot) { claim() }
            .onChange(of: surface) { claim() }
    }

    private func claim() {
        guard router?.take(slot) == true else { return }
        if let surface { deck?.focus(surface) } else { focused = true }
        claimed()
    }
}

struct SlotPane: View {
    let store: ProjectUIStore
    let slot: String

    var body: some View {
        if let view = store.slots[slot] {
            SDUIView(node: view.tree, slot: slot, projectID: store.projectID, owner: view.owner) { [store] event in
                await store.send(event)
            } run: { [store] invocation in
                await store.run(invocation)
            }
        } else {
            EmptyNodeView(text: "")
        }
    }
}

private struct SidebarPane: View {
    let session: ProjectSession
    let store: ProjectUIStore
    let section: String
    let side: SidebarSide

    @Environment(\.theme) private var theme
    @State private var dragStart: CGFloat?
    @State private var dragWidth: CGFloat?
    @State private var hovering = false

    private static let range: ClosedRange<CGFloat> = 160...400

    private var width: CGFloat {
        let saved = store.sidebarWidth.map { CGFloat($0) } ?? 220
        return min(max(dragWidth ?? saved, Self.range.lowerBound), Self.range.upperBound)
    }

    var body: some View {
        HStack(spacing: 0) {
            if side == .right { handle }
            content
                .frame(width: width)
                .environment(\.sidebarSlot, "\(section).sidebar")
            if side == .left { handle }
        }
    }

    @ViewBuilder private var content: some View {
        if section == CodeSection.id {
            CodeSidebar(session: session, store: store)
        } else {
            SlotPane(store: store, slot: "\(section).sidebar")
        }
    }

    private var handle: some View {
        theme.border
            .frame(width: 1)
            .overlay {
                Color.clear
                    .frame(width: 7)
                    .contentShape(Rectangle())
                    .onHover(perform: hover)
                    .gesture(resize)
            }
            .zIndex(1)
    }

    private var resize: some Gesture {
        DragGesture(minimumDistance: 1, coordinateSpace: .global)
            .onChanged { drag in
                let start = dragStart ?? width
                dragStart = start
                let delta = side == .left ? drag.translation.width : -drag.translation.width
                dragWidth = min(max(start + delta, Self.range.lowerBound), Self.range.upperBound)
            }
            .onEnded { _ in
                if let dragWidth { store.sidebarWidth = Double(dragWidth) }
                dragStart = nil
                dragWidth = nil
            }
    }

    private func hover(_ inside: Bool) {
        guard inside != hovering else { return }
        hovering = inside
        if inside { NSCursor.resizeLeftRight.push() } else { NSCursor.pop() }
    }
}

private struct WindowObserver: NSViewRepresentable {
    let onKey: @MainActor () -> Void
    let onAttach: @MainActor (NSWindow) -> Void
    let onClose: @MainActor () -> Void

    func makeNSView(context: Context) -> WindowObserverView {
        let view = WindowObserverView()
        updateNSView(view, context: context)
        return view
    }

    func updateNSView(_ view: WindowObserverView, context: Context) {
        view.onKey = onKey
        view.onAttach = onAttach
        view.onClose = onClose
        if let window = view.window { onAttach(window) }
        if view.window?.isKeyWindow == true { onKey() }
    }
}

final class WindowObserverView: NSView {
    var onKey: (@MainActor () -> Void)?
    var onAttach: (@MainActor (NSWindow) -> Void)?
    var onClose: (@MainActor () -> Void)?

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        if let window {
            NotificationCenter.default.removeObserver(self, name: NSWindow.didBecomeKeyNotification, object: window)
            NotificationCenter.default.removeObserver(self, name: NSWindow.willCloseNotification, object: window)
        }
        if let newWindow {
            NotificationCenter.default.addObserver(self, selector: #selector(windowDidBecomeKey), name: NSWindow.didBecomeKeyNotification, object: newWindow)
            NotificationCenter.default.addObserver(self, selector: #selector(windowWillClose), name: NSWindow.willCloseNotification, object: newWindow)
        }
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if let window { onAttach?(window) }
        if window?.isKeyWindow == true { onKey?() }
    }

    @objc private func windowDidBecomeKey(_ notification: Notification) {
        onKey?()
    }

    @objc private func windowWillClose(_ notification: Notification) {
        onClose?()
    }
}

struct ProjectSessionKey: FocusedValueKey {
    typealias Value = ProjectSession
}

struct WorkspaceWindowIDKey: FocusedValueKey {
    typealias Value = String
}

extension FocusedValues {
    var projectSession: ProjectSession? {
        get { self[ProjectSessionKey.self] }
        set { self[ProjectSessionKey.self] = newValue }
    }

    var workspaceWindowID: String? {
        get { self[WorkspaceWindowIDKey.self] }
        set { self[WorkspaceWindowIDKey.self] = newValue }
    }
}
