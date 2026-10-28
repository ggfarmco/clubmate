import AppKit
import SwiftUI

import ClubmateKit

struct ProjectWindow: View {
    let projectID: String?

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.openWindow) private var openWindow
    @Environment(\.dismiss) private var dismiss
    @State private var session: ProjectSession?
    @State private var presence = ProjectPresence()
    @State private var focus = FocusRouter()

    var body: some View {
        content
            .frame(minWidth: 800, minHeight: 500)
            .background(theme.background, ignoresSafeAreaEdges: .all)
            .navigationTitle(title)
            .environment(\.surfaceDeck, session?.deck)
            .environment(\.focusRouter, focus)
            .environment(\.sidebarSide, app.settings.sidebarSide)
            .eventBanner(session?.banner)
            .commandPalette(session, entries: paletteEntries, run: perform)
            .focusedSceneValue(\.projectSession, session)
            .background(WindowCloseObserver { [app] in
                if let session { app.close(session) }
            })
            .onAppear {
                app.launch(openWindow)
                if session == nil, let projectID { session = app.session(for: projectID) }
            }
            .onChange(of: session?.store?.focusRequest) { _, request in
                guard let request, !request.slot.isEmpty, request.accepts(side: app.settings.sidebarSide) else { return }
                if request.slot.hasSuffix(Self.sidebarSuffix) {
                    session?.store?.revealSidebar(String(request.slot.dropLast(Self.sidebarSuffix.count)))
                }
                focus.request(request.slot)
            }
            .onChange(of: app.settings.autoHideSidebar) { _, enabled in
                if !enabled { session?.store?.sidebarAutoHidden = false }
            }
            .onChange(of: listed, initial: true) { _, present in
                guard let present, presence.observe(present) else { return }
                dismiss()
            }
    }

    private static let sidebarSuffix = ".sidebar"

    private var listed: Bool? {
        guard let projectID, let projects = app.projects?.projects else { return nil }
        return projects.contains { $0.id == projectID }
    }

    private var title: String {
        guard let projectID else { return "Project" }
        return app.project(projectID)?.name ?? projectID
    }

    @ViewBuilder private var content: some View {
        if let session, let store = session.store, let section = store.selectedSection {
            let side = app.settings.sidebarSide
            let hidden = app.settings.hidesSidebar(section, manual: store.sidebarHidden[section] == true, autoHidden: store.sidebarAutoHidden)
            let sidebar = hidden ? nil : side
            VStack(spacing: 0) {
                HStack(spacing: 0) {
                    Rail(sections: store.sections, selected: section) { store.selectedSection = $0 }
                    theme.border.frame(width: 1)
                    if sidebar == .left {
                        SidebarPane(store: store, slot: "\(section)\(Self.sidebarSuffix)", side: .left)
                    }
                    SlotPane(store: store, slot: "\(section).main")
                        .modifier(MainFocus(slot: "\(section).main", surface: store.slots["\(section).main"]?.tree.surfaceSessionID) {
                            if app.settings.autoHides(section) { store.sidebarAutoHidden = true }
                        })
                        .overlay(alignment: .top) {
                            if let failure = store.failure { BannerLabel(text: failure, edge: .bottom) }
                        }
                    if sidebar == .right {
                        SidebarPane(store: store, slot: "\(section)\(Self.sidebarSuffix)", side: .right)
                    }
                }
                .background {
                    Button("Toggle Sidebar") { store.toggleSidebar(app.settings) }
                        .keyboardShortcut("s", modifiers: [.command, .control])
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
        if let store = session?.store {
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
        CommandPalette.entries(commands: app.commands, sections: session?.store?.sections ?? [], sidebar: true)
    }

    private func perform(_ action: PaletteAction) {
        switch action {
        case .command(let id): confirm(id)
        case .section(let id): session?.store?.selectedSection = id
        case .toggleSidebar: session?.store?.toggleSidebar(app.settings)
        case .openProjects: openWindow(id: "projects")
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
        session?.run(CommandInvocation(id: id))
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
    let store: ProjectUIStore
    let slot: String
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
            SlotPane(store: store, slot: slot)
                .frame(width: width)
            if side == .left { handle }
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

private struct WindowCloseObserver: NSViewRepresentable {
    let onClose: @MainActor () -> Void

    func makeNSView(context: Context) -> WindowCloseView {
        let view = WindowCloseView()
        view.onClose = onClose
        return view
    }

    func updateNSView(_ view: WindowCloseView, context: Context) {
        view.onClose = onClose
    }
}

final class WindowCloseView: NSView {
    var onClose: (@MainActor () -> Void)?

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        if let window {
            NotificationCenter.default.removeObserver(self, name: NSWindow.willCloseNotification, object: window)
        }
        if let newWindow {
            NotificationCenter.default.addObserver(self, selector: #selector(windowWillClose), name: NSWindow.willCloseNotification, object: newWindow)
        }
    }

    @objc private func windowWillClose(_ notification: Notification) {
        onClose?()
    }
}

struct ProjectSessionKey: FocusedValueKey {
    typealias Value = ProjectSession
}

extension FocusedValues {
    var projectSession: ProjectSession? {
        get { self[ProjectSessionKey.self] }
        set { self[ProjectSessionKey.self] = newValue }
    }
}
