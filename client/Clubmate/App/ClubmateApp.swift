import AppKit
import SwiftUI

import ClubmateKit

@main
struct ClubmateApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var app = AppModel()

    private let gallery = CommandLine.arguments.contains("--sdui-gallery")

    var body: some Scene {
        WindowGroup("Workspace", for: String.self) { $windowID in
            Group {
                if gallery {
                    GalleryView().eventBanner()
                } else {
                    WorkspaceWindow(windowID: $windowID)
                }
            }
            .environment(app)
            .modifier(SettingsTheme(settings: app.settings))
        }
        .defaultSize(width: 1100, height: 700)
        .windowToolbarStyle(.unifiedCompact)
        .commands { ProjectCommands(app: app) }

        Settings {
            SettingsView(settings: app.settings)
                .environment(app)
        }
    }
}

private struct SettingsTheme: ViewModifier {
    let settings: AppSettings

    func body(content: Content) -> some View {
        content.environment(\.theme, GhosttyApp.shared.theme.scaled(settings.interfaceFontSize))
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let closeKey = CloseKeyOwner()

    func applicationDidFinishLaunching(_ notification: Notification) {
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

private struct ProjectCommands: Commands {
    let app: AppModel

    @FocusedValue(\.projectSession) private var session
    @FocusedValue(\.workspaceWindowID) private var windowID

    private var store: ProjectUIStore? {
        session?.store
    }

    private var active: String? {
        windowID.flatMap(app.window)?.active
    }

    private var welcome: Bool {
        windowID != nil && active == nil
    }

    private var recents: [Project] {
        Array(Project.recents(app.projects?.projects ?? [], hiddenBefore: app.settings.recentsClearedAt).prefix(10))
    }

    var body: some Commands {
        CommandGroup(replacing: .appInfo) {
            Button("About Clubmate") { showAbout() }
            Button("Check for Updates…") { app.updates.check() }
                .disabled(!app.updates.canCheck)
            Button("Install “\(CommandLineTool.name(flavor: ClaudeConnector.bundleFlavor()))” Command…") { app.installCommandLineTool() }
        }
        CommandGroup(replacing: .newItem) {
            Button("New") { newItem() }
                .keyboardShortcut("n")
                .disabled(store == nil && !welcome)
            Button("New Home Terminal") { app.newHomeTerminal(in: windowID) }
                .keyboardShortcut("n", modifiers: [.command, .shift])
            Button("Open Folder…") { app.openFolder(in: windowID) }
                .keyboardShortcut("o")
            Menu("Open Recent") {
                ForEach(recents, id: \.id) { project in
                    Button(project.name) { app.open(project.id, in: windowID) }
                }
                Divider()
                Button("Clear Menu") { app.settings.recentsClearedAt = Date() }
                    .disabled(recents.isEmpty)
            }
            Divider()
            Button("Add Folder to Workspace…") { active.map(app.addFolder) }
                .disabled(active == nil)
            Button("Rename Workspace…") { active.map(app.rename) }
                .disabled(active == nil)
            Button("Close Workspace") { closeWorkspace() }
                .disabled(active == nil)
            Button("Remove Workspace…") { active.map(app.remove) }
                .disabled(active == nil)
            Divider()
            Button("Close Session") {
                run { .close(section: $0.selectedSection, main: $0.selectedSection.flatMap($0.mainTree), commands: $1) }
            }
            .keyboardShortcut("w", modifiers: [.command, .shift])
            .disabled(store == nil)
            Button(session != nil ? CloseKeyOwner.terminalTitle : CloseKeyOwner.windowTitle) { closeTerminal() }
                .keyboardShortcut("w")
        }
        CommandGroup(replacing: .printItem) {}
        CommandGroup(before: .toolbar) {
            Button("Command Palette") { shortcut(.palette) }
                .keyboardShortcut("k")
                .disabled(session == nil)
            Button("Go to File…") { shortcut(.goToFile) }
                .keyboardShortcut("p")
                .disabled(session == nil)
            Button("Find in Files…") { shortcut(.findInFiles) }
                .keyboardShortcut("f", modifiers: [.command, .shift])
                .disabled(session == nil)
            Button("Toggle Sidebar") { store?.toggleSidebar(app.settings) }
                .keyboardShortcut("b")
                .disabled(store == nil)
            Divider()
            Button("Increase Font Size") { app.stepTerminalFont(1) }
                .keyboardShortcut("=")
            Button("Decrease Font Size") { app.stepTerminalFont(-1) }
                .keyboardShortcut("-")
            Button("Reset Font Size") { app.settings.terminalFontSize = nil }
                .keyboardShortcut("0")
        }
        CommandGroup(after: .windowArrangement) {
            Button("Next Workspace") { windowID.map(app.next) }
                .keyboardShortcut(.rightArrow, modifiers: [.control, .command])
                .disabled(!canSwitchWorkspace)
            Button("Previous Workspace") { windowID.map(app.previous) }
                .keyboardShortcut(.leftArrow, modifiers: [.control, .command])
                .disabled(!canSwitchWorkspace)
            Button("Workspace Switcher") { windowID.map(app.toggleSwitcher) }
                .keyboardShortcut("w", modifiers: [.control, .command])
                .disabled(windowID == nil)
        }
        CommandMenu("Section") {
            ForEach(1..<10, id: \.self) { index in
                Button(sectionTitle(index)) { selectSection(index) }
                    .keyboardShortcut(KeyEquivalent(Character("\(index)")))
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
        (windowID.flatMap(app.window)?.workspaces.count ?? 0) > 1
    }

    private func newItem() {
        guard store != nil else { return app.newHomeTerminal(in: windowID) }
        run { .new(section: $0.selectedSection, commands: $1) }
    }

    private func closeWorkspace() {
        guard let windowID, let active else { return }
        app.closeWorkspace(active, in: windowID)
    }

    private func closeTerminal() {
        guard let session else {
            NSApp.keyWindow?.performClose(nil)
            return
        }
        guard let store = session.store, store.selectedSection == TerminalsSection.id,
              let close = TerminalsSection.close(sidebar: store.slots["\(TerminalsSection.id).sidebar"]?.tree, commands: app.commands)
        else { return }
        session.closeTerminal(close)
    }

    private func showAbout() {
        NSApplication.shared.orderFrontStandardAboutPanel(options: [.applicationVersion: Bundle.main.clubmateVersion, .version: ""])
    }

    private func run(_ resolve: (ProjectUIStore, [Command]) -> CommandInvocation?) {
        guard let session, let store else { return }
        session.run(resolve(store, app.commands))
    }

    private func section(_ index: Int) -> SectionInfo? {
        guard let sections = store?.sections, sections.indices.contains(index - 1) else { return nil }
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

extension ProjectUIStore {
    func mainTree(_ section: String) -> SDUINode? {
        slots["\(section).main"]?.tree
    }
}
