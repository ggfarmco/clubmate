import AppKit
import SwiftUI

import ClubmateKit

@main
struct ClubmateApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var app = AppModel()

    private let gallery = CommandLine.arguments.contains("--sdui-gallery")

    #if DEBUG
    private let devicesPreview = DevicesPreview.state

    private func preview(_ state: String) -> some View {
        DevicesPreviewView(state: state)
    }
    #else
    private let devicesPreview: String? = nil

    private func preview(_ state: String) -> some View {
        EmptyView()
    }
    #endif

    var body: some Scene {
        WindowGroup("Workspace", for: String.self) { $windowID in
            Group {
                if gallery {
                    GalleryView().eventBanner()
                } else if let state = devicesPreview {
                    preview(state)
                } else {
                    WorkspaceWindow(windowID: $windowID)
                }
            }
            .environment(app)
            .environment(app.windows)
            .environment(app.actions)
            .modifier(SettingsTheme(settings: app.settings))
        }
        .defaultSize(width: 1100, height: 700)
        .windowToolbarStyle(.unifiedCompact)
        .commands { WorkspaceCommands(app: app, windows: app.windows, actions: app.actions) }

        Settings {
            SettingsView(settings: app.settings)
                .environment(app)
                .modifier(SettingsTheme(settings: app.settings))
        }
    }
}

struct SettingsTheme: ViewModifier {
    let settings: AppSettings

    func body(content: Content) -> some View {
        content.environment(\.theme, GhosttyApp.shared.theme.interface(family: settings.interfaceFontFamily, size: settings.interfaceFontSize))
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let closeKey = CloseKeyOwner()

    func applicationDidFinishLaunching(_ notification: Notification) {
        if TestMode.current { TestHarness.shared.start() }
        closeKey.start()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        AppDelegate.willQuit?()
        return .terminateNow
    }

    @MainActor static var willQuit: (() -> Void)?
}

@MainActor
private final class CloseKeyOwner {
    static let terminalTitle = "Close Terminal"
    static let windowTitle = "Close Window"

    private var pending = false

    func start() {
        for name in [NSMenu.didAddItemNotification, NSMenu.didChangeItemNotification] {
            NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.schedule() }
            }
        }
        schedule()
    }

    private func schedule() {
        guard !pending else { return }
        pending = true
        DispatchQueue.main.async { [weak self] in
            self?.pending = false
            self?.apply()
        }
    }

    private func apply() {
        for item in NSApp.mainMenu?.items.compactMap(\.submenu).flatMap(\.items) ?? [] where item.keyEquivalentModifierMask == .command {
            if item.action == #selector(NSWindow.performClose(_:)), item.keyEquivalent == "w" {
                item.keyEquivalent = ""
            } else if [Self.terminalTitle, Self.windowTitle].contains(item.title), item.keyEquivalent != "w" {
                item.keyEquivalent = "w"
            }
        }
    }
}

private struct WorkspaceCommands: Commands {
    let app: AppModel
    let windows: WindowRouter
    let actions: WorkspaceActions

    @FocusedValue(\.workspaceSession) private var session
    @FocusedValue(\.workspaceWindowID) private var windowID

    private var store: WorkspaceUIStore? {
        session?.store
    }

    private var active: String? {
        windowID.flatMap(windows.window)?.active
    }

    private var welcome: Bool {
        windowID != nil && active == nil
    }

    private var recents: [Workspace] {
        Array(Workspace.recents(app.workspaceStore?.workspaces ?? [], hiddenBefore: app.settings.recentsClearedAt).prefix(10))
    }

    var body: some Commands {
        CommandGroup(replacing: .appInfo) {
            Button("About Clubmate") { showAbout() }
            Button("Check for Updates…") { app.updates.check() }
                .disabled(!app.updates.canCheck)
            Button("Install “\(CommandLineTool.name(flavor: ClaudeConnector.bundleFlavor()))” Command…") { CLIInstaller.install() }
        }
        CommandGroup(replacing: .newItem) {
            Button("New") { newItem() }
                .keyboardShortcut(AppShortcuts.new)
                .disabled(store == nil && !welcome)
            Button("New Home Terminal") { actions.newHomeTerminal(in: windowID) }
                .keyboardShortcut(AppShortcuts.newHomeTerminal)
            Button("Open Folder…") { actions.openFolder(in: windowID) }
                .keyboardShortcut(AppShortcuts.openFolder)
            Menu("Open Recent") {
                ForEach(recents, id: \.id) { workspace in
                    Button(workspace.name) { windows.open(workspace.id, in: windowID) }
                }
                Divider()
                Button("Clear Menu") { app.settings.recentsClearedAt = Date() }
                    .disabled(recents.isEmpty)
            }
            Button("Open Doc…") { session?.chooseDoc() }
                .keyboardShortcut(AppShortcuts.openDoc)
                .disabled(session == nil || !app.commands.contains { $0.id == DocsPicker.listCommand })
            Button("Switch Popup Focus") { session?.switchPopupFocus() }
                .keyboardShortcut(AppShortcuts.switchPopupFocus)
                .disabled(store?.popup == nil)
            Divider()
            Button("Add Folder to Workspace…") { active.map(actions.addFolder) }
                .disabled(active == nil)
            Button("Rename Workspace…") { active.map(actions.rename) }
                .disabled(active == nil)
            Button("Close Workspace") { closeWorkspace() }
                .disabled(active == nil)
            Button("Remove Workspace…") { active.map(actions.remove) }
                .disabled(active == nil)
            Divider()
            Button("Close Session") {
                run { store, commands in
                    guard store.selectedSection != AgentsSection.id else {
                        return AgentsSection.close(sidebar: store.slot(AgentsSection.sidebarSlot)?.tree, commands: commands)
                    }
                    return .close(section: store.selectedSection, main: store.selectedSection.flatMap(store.mainTree), commands: commands)
                }
            }
            .keyboardShortcut(AppShortcuts.closeSession)
            .disabled(store == nil)
            Button(session != nil ? CloseKeyOwner.terminalTitle : CloseKeyOwner.windowTitle) { closeTerminal() }
                .keyboardShortcut(AppShortcuts.close)
        }
        CommandGroup(replacing: .printItem) {}
        CommandGroup(before: .toolbar) {
            Button("Command Palette") { shortcut(.palette) }
                .keyboardShortcut(AppShortcuts.palette)
                .disabled(session == nil)
            Button(store?.togglesChat == true ? "Toggle Chat" : "Toggle Sidebar") { store?.toggleSidebar() }
                .keyboardShortcut(AppShortcuts.toggleSidebar)
                .disabled(store == nil)
            Divider()
            Button("Increase Font Size") { app.stepFont(1, panes: session?.panes, store: store) }
                .keyboardShortcut(AppShortcuts.fontIncrease)
            Button("Decrease Font Size") { app.stepFont(-1, panes: session?.panes, store: store) }
                .keyboardShortcut(AppShortcuts.fontDecrease)
            Button("Reset Font Size") { app.resetFont(panes: session?.panes, store: store) }
                .keyboardShortcut(AppShortcuts.fontReset)
        }
        CommandGroup(after: .windowArrangement) {
            Button("Next Workspace") { windowID.map(windows.next) }
                .keyboardShortcut(AppShortcuts.nextWorkspace)
                .disabled(!canSwitchWorkspace)
            Button("Previous Workspace") { windowID.map(windows.previous) }
                .keyboardShortcut(AppShortcuts.previousWorkspace)
                .disabled(!canSwitchWorkspace)
            Button("Workspace Switcher") { windowID.map(windows.toggleSwitcher) }
                .keyboardShortcut(AppShortcuts.workspaceSwitcher)
                .disabled(windowID == nil)
            Button("Notifications") { windowID.map(windows.toggleInbox) }
                .keyboardShortcut(AppShortcuts.notifications)
                .disabled(windowID == nil)
        }
        CommandGroup(after: .help) {
            Button("\(Diagnostics.title)…") { app.collectDiagnostics() }
        }
        CommandMenu("Section") {
            ForEach(1..<10, id: \.self) { index in
                Button(sectionTitle(index)) { selectSection(index) }
                    .keyboardShortcut(AppShortcuts.section, key: AppShortcuts.section(index))
                    .disabled(section(index) == nil)
            }
        }
    }

    private func shortcut(_ overlay: WorkspaceOverlay) {
        guard let session else { return }
        if session.overlay == overlay, let window = NSApp.keyWindow, PaletteTextField.focusIfUnfocused(in: window) { return }
        session.toggle(overlay)
    }

    private var canSwitchWorkspace: Bool {
        (windowID.flatMap(windows.window)?.workspaces.count ?? 0) > 1
    }

    private func newItem() {
        guard store != nil else { return actions.newHomeTerminal(in: windowID) }
        run { .new(section: $0.selectedSection, commands: $1) }
    }

    private func closeWorkspace() {
        guard let windowID, let active else { return }
        windows.closeWorkspace(active, in: windowID)
    }

    private func closeTerminal() {
        guard let session else {
            NSApp.keyWindow?.performClose(nil)
            return
        }
        guard let store = session.store, store.selectedSection == TerminalsSection.id,
              let close = TerminalsSection.close(sidebar: store.slot("\(TerminalsSection.id).sidebar")?.tree, commands: app.commands)
        else { return }
        session.closeTerminal(close)
    }

    private func showAbout() {
        NSApplication.shared.orderFrontStandardAboutPanel(options: [.applicationVersion: Bundle.main.clubmateVersion, .version: ""])
    }

    private func run(_ resolve: (WorkspaceUIStore, [Command]) -> CommandInvocation?) {
        guard let session, let store else { return }
        session.run(resolve(store, app.commands))
    }

    private func section(_ index: Int) -> SectionInfo? {
        guard let sections = store.map({ app.settings.ordered($0.sections) }), sections.indices.contains(index - 1) else { return nil }
        return sections[index - 1]
    }

    private func sectionTitle(_ index: Int) -> String {
        section(index)?.title ?? "Section \(index)"
    }

    private func selectSection(_ index: Int) {
        guard let id = section(index)?.id else { return }
        store?.selectedSection = id
    }
}

extension WorkspaceUIStore {
    func mainTree(_ section: String) -> SDUINode? {
        slot("\(section).main")?.tree
    }
}
