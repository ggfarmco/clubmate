import AppKit
import SwiftUI
import WebKit

import ClubmateKit

@MainActor
final class PopupWindowHost: NSObject, NSWindowDelegate {
    static let identifier = NSUserInterfaceItemIdentifier("clubmate.popup")
    static let size = NSSize(width: 900, height: 760)

    private let window: NSWindow
    private weak var session: WorkspaceSession?
    private var placed = false

    init(session: WorkspaceSession, app: AppModel) {
        self.session = session
        window = NSWindow(contentRect: NSRect(origin: .zero, size: Self.size), styleMask: [.titled, .closable, .resizable, .miniaturizable], backing: .buffered, defer: false)
        super.init()
        window.identifier = Self.identifier
        window.isReleasedWhenClosed = false
        window.isRestorable = false
        window.tabbingMode = .disallowed
        window.contentMinSize = NSSize(width: 480, height: 360)
        window.delegate = self
        window.contentView = NSHostingView(rootView: PopupWindowRoot(session: session) { [weak self] in
            self?.window.title = $0
        } close: { [weak self] in
            self?.userClosed()
        }
        .modifier(SettingsTheme(settings: app.settings))
        .environment(app))
    }

    func show() {
        if !placed, let visible = NSScreen.main?.visibleFrame {
            placed = true
            let size = NSSize(width: min(Self.size.width, visible.width), height: min(Self.size.height, visible.height))
            window.setFrame(NSRect(x: visible.midX - size.width / 2, y: visible.midY - size.height / 2, width: size.width, height: size.height), display: true)
        }
        window.makeKeyAndOrderFront(nil)
        if !TestMode.current { NSApp.activate() }
        DispatchQueue.main.async { [window] in
            guard let root = window.contentView else { return }
            window.makeFirstResponder(root.descendants(WKWebView.self).first ?? root)
        }
    }

    func hide() {
        window.orderOut(nil)
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        userClosed()
        return false
    }

    private func userClosed() {
        guard let session else { return hide() }
        session.popupPoppedOut = false
        hide()
        if let slot = session.store?.popup { session.closePopup(slot) }
    }
}

private struct PopupWindowRoot: View {
    let session: WorkspaceSession
    let title: @MainActor (String) -> Void
    let close: @MainActor () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        Group {
            if let store = session.store, let slot = store.popup {
                PopupContent(store: store, slot: slot, poppedOut: true) { action in
                    if action == .close { close() }
                }
                .onChange(of: heading(store, slot), initial: true) { _, heading in title(heading) }
            } else {
                EmptyNodeView(text: "")
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(theme.background)
        .keyMonitor(active: true) { event in
            guard event.keyCode == PopupPanel.escape, event.modifierFlags.intersection(.deviceIndependentFlagsMask).subtracting(.capsLock).isEmpty else { return false }
            close()
            return true
        }
    }

    private func heading(_ store: WorkspaceUIStore, _ slot: String) -> String {
        store.slot(slot)?.tree.firstHeader?.string("title") ?? store.popupTitle(slot)
    }
}
