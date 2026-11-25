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
            Button(state.canEscape ? "Close" : "Step Out") { navigation.stepOut() }
                .keyboardShortcut(.cancelAction)
                .disabled(!state.canEscape && !state.listStop)
            Button(state.canEscape ? "Close (Alternate)" : "Step Out (Alternate)") { navigation.stepOut() }
                .keyboardShortcut("[", modifiers: .control)
                .disabled(!state.canEscape && !state.listStop)
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
            Divider()
            Button("Next Session") { jump { SessionJumps.step(SessionJumps.shown($0.shownSections).map(\.id), from: navigation.selected(.agents), by: 1) } }
                .keyboardShortcut(.downArrow, modifiers: [.control, .command])
                .disabled(!state.hasComputer)
            Button("Previous Session") { jump { SessionJumps.step(SessionJumps.shown($0.shownSections).map(\.id), from: navigation.selected(.agents), by: -1) } }
                .keyboardShortcut(.upArrow, modifiers: [.control, .command])
                .disabled(!state.hasComputer)
            Button("Next Session That Needs You") { jump { SessionJumps.step(SessionJumps.needs($0.sessions), from: navigation.selected(.agents), by: 1) } }
                .keyboardShortcut("j", modifiers: [.control, .command])
                .disabled(!state.hasComputer)
            Button("Previous Session That Needs You") { jump { SessionJumps.step(SessionJumps.needs($0.sessions), from: navigation.selected(.agents), by: -1) } }
                .keyboardShortcut("j", modifiers: [.control, .shift, .command])
                .disabled(!state.hasComputer)
            Button("Last Session") { jump { store in navigation.lastSession.flatMap { store.session($0) }.flatMap { $0.isClosed ? nil : $0.id } } }
                .keyboardShortcut(.tab, modifiers: .control)
                .disabled(!state.hasComputer)
            ForEach(1...SessionJumps.limit, id: \.self) { index in
                Button("Session \(index)") { jump { SessionJumps.numbered($0.shownSections).dropFirst(index - 1).first } }
                    .keyboardShortcut(KeyEquivalent(Character(String(index))), modifiers: [.control, .command])
                    .disabled(!state.hasComputer)
            }
        }
    }

    private var state: MenuState {
        navigation.menu.state
    }

    private func jump(_ pick: (AgentsStore) -> String?) {
        guard let store = navigation.agentsStore(model), let id = pick(store) else { return }
        navigation.openSession(id)
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

    func agentsStore(_ model: CompanionModel) -> AgentsStore? {
        guard let computer, let connection = model.connection(computer), let workspace = workspace(for: connection) else { return nil }
        return model.agents(computer: computer, workspace: workspace.id, settled: false)
    }

    func stepOut() {
        let panes = menu.live.keys?.panes
        guard canEscape else {
            if menu.panes.listFocused { panes?.escape() }
            return
        }
        let fromList = menu.panes.listFocused && !readingView
        withAnimation(.easeInOut(duration: 0.3)) { escape() }
        if fromList { panes?.escape() }
    }

    func openInbox() {
        readingView = false
        if !isTop(.inbox) { push(.inbox) }
    }
}

