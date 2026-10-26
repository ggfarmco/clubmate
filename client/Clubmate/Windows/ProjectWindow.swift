import AppKit
import SwiftUI

import ClubmateKit

struct ProjectWindow: View {
    let projectID: String?

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @State private var session: ProjectSession?

    var body: some View {
        content
            .frame(minWidth: 800, minHeight: 500)
            .background(theme.background, ignoresSafeAreaEdges: .all)
            .navigationTitle(title)
            .environment(\.surfaceDeck, session?.deck)
            .eventBanner(session?.banner)
            .focusedSceneValue(\.projectSession, session)
            .background(WindowCloseObserver { [app] in
                if let session { app.close(session) }
            })
            .onAppear {
                app.launch()
                if session == nil, let projectID { session = app.session(for: projectID) }
            }
            .onChange(of: session?.store?.connected) { old, new in
                if old == true, new == false { app.connectionLost() }
            }
    }

    private var title: String {
        guard let projectID else { return "Project" }
        return app.project(projectID)?.name ?? projectID
    }

    @ViewBuilder private var content: some View {
        if let session, let store = session.store, let section = store.selectedSection {
            VStack(spacing: 0) {
                HStack(spacing: 0) {
                    Rail(sections: store.sections, selected: section) { store.selectedSection = $0 }
                    theme.border.frame(width: 1)
                    SidebarPane(store: store, slot: "\(section).sidebar")
                    SlotPane(store: store, slot: "\(section).main")
                }
                StatusBar(connection: connection(store), section: store.sections.first { $0.id == section }?.title ?? section)
            }
        } else {
            EmptyNodeView(text: placeholder)
        }
    }

    private var placeholder: String {
        if let failure = session?.failure { return failure }
        switch app.phase {
        case .starting: return "Starting core…"
        case .failed(let message): return message
        case .ready, .reconnecting: return "Connecting…"
        }
    }

    private func connection(_ store: ProjectUIStore) -> StatusBar.Connection {
        switch app.phase {
        case .ready: store.connected ? .connected : .reconnecting
        case .starting, .reconnecting: .reconnecting
        case .failed: .disconnected
        }
    }
}

private struct SlotPane: View {
    let store: ProjectUIStore
    let slot: String

    var body: some View {
        if let view = store.slots[slot] {
            SDUIView(node: view.tree, slot: slot, projectID: store.projectID) { [store] event in
                await store.send(event)
            }
        } else {
            EmptyNodeView(text: "")
        }
    }
}

private struct SidebarPane: View {
    let store: ProjectUIStore
    let slot: String

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
            SlotPane(store: store, slot: slot)
                .frame(width: width)
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
    }

    private var resize: some Gesture {
        DragGesture(minimumDistance: 1, coordinateSpace: .global)
            .onChanged { drag in
                let start = dragStart ?? width
                dragStart = start
                dragWidth = min(max(start + drag.translation.width, Self.range.lowerBound), Self.range.upperBound)
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
