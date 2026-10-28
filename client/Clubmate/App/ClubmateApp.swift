import AppKit
import SwiftUI

import ClubmateKit

@main
struct ClubmateApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var app = AppModel()

    private let gallery = CommandLine.arguments.contains("--sdui-gallery")

    var body: some Scene {
        Window("Projects", id: "projects") {
            Group {
                if gallery {
                    GalleryView().eventBanner()
                } else {
                    ProjectsWindow()
                }
            }
            .environment(app)
            .modifier(SettingsTheme(settings: app.settings))
        }
        .defaultSize(width: 560, height: 420)
        .keyboardShortcut("o", modifiers: [.command, .shift])
        .commands { ProjectCommands(app: app) }

        WindowGroup("Project", for: String.self) { $projectID in
            ProjectWindow(projectID: projectID)
                .environment(app)
                .modifier(SettingsTheme(settings: app.settings))
        }
        .defaultSize(width: 1100, height: 700)

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
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        AppDelegate.willQuit?()
        return .terminateNow
    }

    @MainActor static var willQuit: (() -> Void)?
}

private struct ProjectCommands: Commands {
    let app: AppModel

    @FocusedValue(\.projectSession) private var session

    private var store: ProjectUIStore? {
        session?.isApp == false ? session?.store : nil
    }

    var body: some Commands {
        CommandGroup(replacing: .appInfo) {
            Button("About Clubmate") { showAbout() }
            Button("Check for Updates…") { app.updates.check() }
                .disabled(!app.updates.canCheck)
        }
        CommandGroup(replacing: .newItem) {
            Button("New") { run { .new(section: $0.selectedSection, commands: $1) } }
                .keyboardShortcut("n")
                .disabled(store == nil)
            Button("Close Session") {
                run { .close(section: $0.selectedSection, main: $0.selectedSection.flatMap($0.mainTree), commands: $1) }
            }
            .keyboardShortcut("w", modifiers: [.command, .shift])
            .disabled(store == nil)
        }
        CommandGroup(replacing: .saveItem) {
            Button(session?.isApp == false ? "Close Terminal" : "Close Window") { closeTerminal() }
                .keyboardShortcut("w")
        }
        CommandGroup(before: .toolbar) {
            Button("Command Palette") { session?.paletteShown.toggle() }
                .keyboardShortcut("k")
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
        CommandMenu("Section") {
            ForEach(1..<10, id: \.self) { index in
                Button(sectionTitle(index)) { selectSection(index) }
                    .keyboardShortcut(KeyEquivalent(Character("\(index)")))
                    .disabled(section(index) == nil)
            }
        }
    }

    private func closeTerminal() {
        guard let session, !session.isApp else {
            NSApp.keyWindow?.performClose(nil)
            return
        }
        guard let store = session.store, store.selectedSection == TerminalsSection.id,
              let close = TerminalsSection.close(sidebar: store.slots["\(TerminalsSection.id).sidebar"]?.tree, main: store.mainTree(TerminalsSection.id), commands: app.commands)
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
