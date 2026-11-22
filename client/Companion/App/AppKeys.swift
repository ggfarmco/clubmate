import SwiftUI

import CompanionKit

struct AppCommands: Commands {
    let model: CompanionModel
    let navigation: AppNavigation

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button(state.createTitle) { navigation.create(model) }
                .keyboardShortcut("n", modifiers: .command)
                .disabled(!state.canCreate)
        }
        CommandGroup(replacing: .sidebar) {
            Button(state.listShown ? "Hide List" : "Show List") { withAnimation(.easeInOut(duration: 0.3)) { navigation.toggleList() } }
                .keyboardShortcut("s", modifiers: [.control, .command])
                .disabled(!state.wide)
            Button(state.chatShown ? "Hide Chat" : "Show Chat") { withAnimation(.easeInOut(duration: 0.3)) { navigation.toggleChat() } }
                .keyboardShortcut("b", modifiers: .command)
                .disabled(!state.chatShown && !state.canChat)
            Button(state.readingView ? "Leave Reading View" : "Reading View") { withAnimation(.easeInOut(duration: 0.3)) { navigation.toggleReading() } }
                .keyboardShortcut("f", modifiers: [.control, .command])
                .disabled(!state.readingView && !state.canRead)
            Button("Close") {
                guard navigation.canEscape else { return }
                withAnimation(.easeInOut(duration: 0.3)) { navigation.escape() }
            }
                .keyboardShortcut(.cancelAction)
                .disabled(!state.canEscape)
        }
        CommandMenu("Go") {
            Button("Back") { navigation.back() }
                .keyboardShortcut("[", modifiers: .command)
                .disabled(!state.canGoBack)
            Divider()
            ForEach(Array(CompanionTab.keyed.enumerated()), id: \.element) { index, tab in
                Button(tab.title) {
                    guard navigation.shows(tab, model) else { return }
                    navigation.root = .section(tab)
                }
                    .keyboardShortcut(KeyEquivalent(Character(String(index + 1))), modifiers: .command)
                    .disabled(!state.shownTabs.contains(tab))
            }
            Divider()
            Button("Inbox") {
                guard navigation.computer != nil else { return }
                navigation.openInbox()
            }
                .keyboardShortcut("i", modifiers: [.command, .shift])
                .disabled(!state.hasComputer)
        }
    }

    private var state: MenuState {
        navigation.menu.state
    }
}

extension CompanionTab {
    static let keyed: [CompanionTab] = [.agents, .backlog, .wiki, .more]
}

extension AppNavigation {
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

