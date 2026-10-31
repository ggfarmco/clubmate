import AppKit
import Foundation
import Observation
import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class WindowRouter {
    private(set) var switcherWindow: String?
    private(set) var inboxWindow: String?
    private let layout = WindowLayout(defaults: TestMode.defaults)

    @ObservationIgnored private(set) var keyWindowID: String?
    @ObservationIgnored private var openWindow: OpenWindowAction?
    @ObservationIgnored private let hosts = NSMapTable<NSString, NSWindow>.strongToWeakObjects()
    @ObservationIgnored private var captured: [String: ContinuousClock.Instant] = [:]
    @ObservationIgnored private var shownWindows: Set<String> = []
    @ObservationIgnored private var restored = false
    @ObservationIgnored private let sessions: SessionRegistry
    @ObservationIgnored private let opened: (Set<String>) -> Void
    @ObservationIgnored private let touched: (String) -> Void
    @ObservationIgnored private let focused: () -> Void

    init(
        sessions: SessionRegistry,
        opened: @escaping (Set<String>) -> Void,
        touched: @escaping (String) -> Void,
        focused: @escaping () -> Void
    ) {
        self.sessions = sessions
        self.opened = opened
        self.touched = touched
        self.focused = focused
    }

    func launch(_ openWindow: OpenWindowAction) {
        self.openWindow = openWindow
    }

    func terminating() {
        layout.terminating()
    }

    func window(_ id: String) -> LayoutWindow? {
        layout.window(id)
    }

    func host(_ window: String) -> NSWindow? {
        hosts.object(forKey: window as NSString)
    }

    var openWorkspaces: Set<String> {
        layout.workspaces
    }

    var activeWorkspace: String? {
        layout.activeWorkspace(preferring: keyWindowID)
    }

    var keyWorkspace: (projectID: String?, window: NSWindow?) {
        (keyWindowID.flatMap { layout.window($0)?.active }, keyWindowID.flatMap(host))
    }

    var attentionWindow: NSWindow? {
        keyWindowID.flatMap(host) ?? layout.windows.lazy.compactMap { self.host($0.id) }.first
    }

    func toggleSidebar(_ store: ProjectUIStore?, in window: String?) {
        guard let store else { return }
        guard store.togglesChat else { return store.toggleSidebar() }
        if let window { layout.toggleChat(in: window) }
    }

    func showChat(in window: String) {
        layout.showChat(in: window)
    }

    func setChatWidth(_ width: Double, in window: String) {
        layout.setChatWidth(width, in: window)
    }

    func claimWindow() -> String {
        let id = layout.claim(excluding: shownWindows)
        shownWindows.insert(id)
        return id
    }

    func windowShown(_ window: String) {
        shownWindows.insert(window)
    }

    func open(_ projectID: String, in target: String? = nil) {
        let preferred = target ?? keyWindowID ?? layout.windows.last?.id
        snapshot(layout.windows.first { $0.workspaces.contains(projectID) }?.id ?? preferred, leaving: projectID)
        let window = layout.open(projectID, preferring: preferred)
        syncSessions()
        touched(projectID)
        openWindow?(value: window)
        if !TestMode.current { NSApp.activate() }
    }

    func activateFromSwitcher(_ projectID: String, in window: String) {
        hideSwitcher()
        host(window)?.contentView?.layoutSubtreeIfNeeded()
        activate(projectID, in: window)
    }

    func activate(_ projectID: String, in window: String) {
        snapshot(window, leaving: projectID)
        layout.activate(projectID, in: window)
        touchActive(in: window)
    }

    func next(in window: String) {
        guard (layout.window(window)?.workspaces.count ?? 0) > 1 else { return }
        snapshot(window)
        layout.next(in: window)
        touchActive(in: window)
    }

    func previous(in window: String) {
        guard (layout.window(window)?.workspaces.count ?? 0) > 1 else { return }
        snapshot(window)
        layout.previous(in: window)
        touchActive(in: window)
    }

    func closeWorkspace(_ projectID: String, in window: String) {
        layout.close(projectID, in: window)
        syncSessions()
    }

    func moveToNewWindow(_ projectID: String) {
        let window = layout.move(projectID, toNewWindow: true)
        syncSessions()
        openWindow?(value: window)
    }

    func windowBecameKey(_ window: String) {
        keyWindowID = window
        focused()
    }

    func windowAttached(_ window: String, _ host: NSWindow) {
        hosts.setObject(host, forKey: window as NSString)
    }

    func toggleSwitcher(in window: String) {
        guard switcherWindow != window else { return hideSwitcher() }
        snapshot(window, throttled: false)
        switcherWindow = window
    }

    func hideSwitcher() {
        switcherWindow = nil
    }

    func toggleInbox(in window: String) {
        inboxWindow = inboxWindow == window ? nil : window
    }

    func hideInbox() {
        inboxWindow = nil
    }

    func windowClosed(_ window: String) {
        if keyWindowID == window { keyWindowID = nil }
        if switcherWindow == window { switcherWindow = nil }
        if inboxWindow == window { inboxWindow = nil }
        captured[window] = nil
        shownWindows.remove(window)
        layout.closeWindow(window)
        syncSessions()
    }

    func restore(_ listed: [Project]) {
        guard !restored else { return }
        restored = true
        layout.forget(missing: layout.workspaces.subtracting(listed.map(\.id)))
        syncSessions()
        for window in layout.windows where !shownWindows.contains(window.id) {
            openWindow?(value: window.id)
        }
    }

    func forget(_ gone: Set<String>) {
        layout.forget(missing: gone)
        syncSessions()
    }

    func syncSessions() {
        opened(layout.workspaces)
        sessions.sync(layout)
    }

    private func snapshot(_ window: String?, leaving target: String? = nil, throttled: Bool = true) {
        guard let window, switcherWindow != window, let active = layout.window(window)?.active, active != target,
              let session = sessions.existing(active), session.overlay == nil,
              let host = host(window), !PaletteTextField.isShown(in: host) else { return }
        let now = ContinuousClock.now
        if throttled, let last = captured[window], now - last < .milliseconds(200) { return }
        captured[window] = now
        session.thumbnail = WorkspaceThumbnail.capture(host)
    }

    private func touchActive(in window: String) {
        if let active = layout.window(window)?.active { touched(active) }
    }
}
