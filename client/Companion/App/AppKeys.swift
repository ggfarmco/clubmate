import SwiftUI

import CompanionKit

struct AppCommands: Commands {
    let model: CompanionModel
    let navigation: AppNavigation

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button(navigation.createTitle) { navigation.create(model) }
                .keyboardShortcut("n", modifiers: .command)
                .disabled(!navigation.canCreate(model))
        }
        CommandGroup(replacing: .sidebar) {
            Button(navigation.listShown ? "Hide List" : "Show List") { withAnimation(.easeInOut(duration: 0.3)) { navigation.toggleList() } }
                .keyboardShortcut("s", modifiers: [.control, .command])
                .disabled(navigation.layout == .compact)
            Button(navigation.readingView ? "Leave Reading View" : "Reading View") { withAnimation(.easeInOut(duration: 0.3)) { navigation.toggleReading() } }
                .keyboardShortcut("f", modifiers: [.control, .command])
                .disabled(!navigation.readingView && !navigation.canRead)
            Button("Close") { withAnimation(.easeInOut(duration: 0.3)) { navigation.escape() } }
                .keyboardShortcut(.cancelAction)
                .disabled(!navigation.canEscape)
        }
        CommandMenu("Go") {
            Button("Back") { navigation.back() }
                .keyboardShortcut("[", modifiers: .command)
                .disabled(!navigation.canGoBack)
            Divider()
            ForEach(Array(CompanionTab.keyed.enumerated()), id: \.element) { index, tab in
                Button(tab.title) { navigation.root = .section(tab) }
                    .keyboardShortcut(KeyEquivalent(Character(String(index + 1))), modifiers: .command)
                    .disabled(!navigation.shows(tab, model))
            }
            Divider()
            Button("Inbox") { navigation.openInbox() }
                .keyboardShortcut("i", modifiers: [.command, .shift])
                .disabled(navigation.computer == nil)
        }
    }
}

extension CompanionTab {
    static let keyed: [CompanionTab] = [.agents, .backlog, .wiki, .more]
}

extension AppNavigation {
    var keyState: [Bool] {
        [layout != .compact, listShown, readingView, canRead, canEscape, canGoBack, tab == .agents || tab == .backlog]
    }

    func shows(_ tab: CompanionTab, _ model: CompanionModel) -> Bool {
        tabs(model).contains(tab)
    }

    var createTitle: String {
        switch tab {
        case .agents: "New Session"
        case .backlog: "New Task"
        default: "New"
        }
    }

    func canCreate(_ model: CompanionModel) -> Bool {
        guard tab == .agents || tab == .backlog, let computer, let connection = model.connection(computer) else { return false }
        return connection.canWrite && workspace(for: connection) != nil
    }

    func create(_ model: CompanionModel) {
        guard canCreate(model), let computer, let connection = model.connection(computer) else { return }
        let context = PluginContext(model: model, connection: connection, navigation: self)
        if tab == .agents {
            AgentsPlugin.startNew(context)
        } else {
            BacklogPlugin.startNew(context)
        }
    }

    func openInbox() {
        readingView = false
        if !isTop(.inbox) { push(.inbox) }
    }
}

