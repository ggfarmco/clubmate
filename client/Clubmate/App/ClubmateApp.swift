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
            .environment(\.theme, GhosttyApp.shared.theme)
        }
        .defaultSize(width: 560, height: 420)
        .keyboardShortcut("o", modifiers: [.command, .shift])
        .commands { ProjectCommands(app: app) }

        WindowGroup("Project", for: String.self) { $projectID in
            ProjectWindow(projectID: projectID)
                .environment(app)
                .environment(\.theme, GhosttyApp.shared.theme)
        }
        .defaultSize(width: 1100, height: 700)
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }
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
        CommandGroup(before: .toolbar) {
            Button("Command Palette") { session?.paletteShown.toggle() }
                .keyboardShortcut("k")
                .disabled(session == nil)
        }
        CommandMenu("Section") {
            ForEach(1..<10, id: \.self) { index in
                Button(sectionTitle(index)) { selectSection(index) }
                    .keyboardShortcut(KeyEquivalent(Character("\(index)")))
                    .disabled(section(index) == nil)
            }
        }
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

private extension ProjectUIStore {
    func mainTree(_ section: String) -> SDUINode? {
        slots["\(section).main"]?.tree
    }
}
