import SwiftUI

import CompanionKit

struct RootView: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @Environment(PairingFlow.self) private var pairing
    @Environment(VoiceSession.self) private var voice
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var width = 0.0
    @State private var sectionUI: WorkspaceUI?
    private static let sectionOwner = "root.section"

    var body: some View {
        @Bindable var navigation = navigation
        NavigationSplitView(columnVisibility: $navigation.visibility, preferredCompactColumn: $navigation.preferredColumn) {
            SidebarView()
                .navigationSplitViewColumnWidth(260)
        } content: {
            content
                .navigationSplitViewColumnWidth(min: 320, ideal: 380, max: 440)
        } detail: {
            detail
        }
        .navigationSplitViewStyle(.balanced)
        .contentMargins(.bottom, navigation.floatingMic(model, listening: voice.active) ? VoiceButton.floating + 16 : 0, for: .scrollContent)
        .overlay { VoiceOverlay() }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in navigation.typing = true }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in navigation.typing = false }
        .onGeometryChange(for: Double.self) { $0.size.width } action: { width = $0 }
        .onChange(of: CompanionLayout(width: width, compactClass: sizeClass == .compact), initial: true) { _, layout in navigation.layout = layout }
        .onChange(of: navigation.layout) { _, _ in navigation.arrange() }
        .onChange(of: navigation.sidebar) { _, _ in navigation.arrange() }
        .onChange(of: navigation.needsYouList) { _, _ in navigation.arrange() }
        .onChange(of: navigation.visibility) { _, _ in navigation.visibilityChanged() }
        .onChange(of: navigation.preferredColumn) { _, column in
            if column == .sidebar, navigation.layout == .compact {
                navigation.macDetails = nil
                navigation.needsYouList = false
            }
        }
        .onChange(of: shownAgents, initial: true) { old, new in
            if let old, old.store != new?.store { model.agents(computer: old.store.computer, workspace: old.store.workspace)?.show(nil) }
            if let new { model.agents(computer: new.store.computer, workspace: new.store.workspace)?.show(new.detail) }
        }
        .onChange(of: shownSection, initial: true) { _, shown in
            sectionUI?.hide(owner: Self.sectionOwner)
            sectionUI = shown.flatMap { model.connection($0.computer)?.workspaceUI($0.workspace) }
            if let shown { sectionUI?.show([shown.section + ".sidebar", shown.section + ".main"], owner: Self.sectionOwner) }
        }
        .sheet(isPresented: $navigation.showingAddComputer, onDismiss: navigation.pairingSheetDismissed) {
            AddComputerView()
                .onAppear { navigation.pairingSheetAppeared() }
        }
        .sheet(item: Binding(get: { navigation.showingAddComputer ? nil : pairing.review }, set: { pairing.review = $0 }), onDismiss: navigation.pairingSheetDismissed) { review in
            PairingConfirmView(review: review)
                .onAppear { navigation.pairingSheetAppeared() }
        }
        .fullScreenCover(item: $navigation.doc) { doc in
            DocumentSheet(doc: doc)
        }
        .overlay {
            if case .pairing(let name) = pairing.phase {
                PairingOverlay(name: name)
            }
        }
        .alert("Couldn't Pair", isPresented: Binding(get: { pairing.failureMessage != nil && !navigation.showingAddComputer }, set: { if !$0 { pairing.dismissFailure() } })) {
            Button("OK") { pairing.dismissFailure() }
        } message: {
            Text(pairing.failureMessage ?? "")
        }
        .sensoryFeedback(.success, trigger: pairing.paired)
        .task { selectDefault() }
        .onChange(of: model.computers) { _, _ in selectDefault() }
    }

    @ViewBuilder
    private var content: some View {
        if let context, let plugin = navigation.sidebar.flatMap({ CompanionPlugins.plugin($0.plugin) }) {
            plugin.makeList(context: context)
        } else if let computer = navigation.macDetails {
            MacDetailsView(computerID: computer)
        } else if navigation.needsYouList {
            NeedsYouListView()
        } else if model.computers.isEmpty {
            WelcomeView()
        } else {
            ContentUnavailableView("Choose a Mac", systemImage: "desktopcomputer", description: Text("Pick Agents or Inbox under one of your Macs."))
        }
    }

    @ViewBuilder
    private var detail: some View {
        if let context, let plugin = navigation.sidebar.flatMap({ CompanionPlugins.plugin($0.plugin) }) {
            plugin.makeDetail(context: context, selection: navigation.detail)
        } else {
            ContentUnavailableView("Clubmate", systemImage: "bubble.left.and.text.bubble.right", description: Text("Control the agents running on your Macs."))
        }
    }

    private var shownAgents: ShownAgents? {
        guard let item = navigation.sidebar, item.plugin == AgentsPluginInfo.id, let connection = model.connection(item.computer), let workspace = navigation.workspace(for: connection) else { return nil }
        let visible = navigation.layout != .compact || navigation.preferredColumn == .detail
        return ShownAgents(store: ShownAgents.Store(computer: item.computer, workspace: workspace.id), detail: visible ? navigation.detail : nil)
    }

    private var shownSection: ShownSection? {
        guard let item = navigation.sidebar, item.plugin == MorePluginInfo.id, let section = navigation.detail, let connection = model.connection(item.computer),
              let workspace = navigation.workspace(for: connection), navigation.layout != .compact || navigation.preferredColumn == .detail else { return nil }
        return ShownSection(computer: item.computer, workspace: workspace.id, section: section)
    }

    private var context: PluginContext? {
        guard let item = navigation.sidebar, let connection = model.connection(item.computer) else { return nil }
        return PluginContext(model: model, connection: connection, navigation: navigation)
    }

    private func selectDefault() {
        if navigation.needsYouList || navigation.pairingSheetUp { return }
        if let item = navigation.sidebar, model.connection(item.computer) != nil { return }
        navigation.sidebar = nil
        guard model.computers.count == 1, let computer = model.computers.first else { return }
        navigation.sidebar = SidebarItem(computer: computer.id, plugin: AgentsPluginInfo.id)
        navigation.preferredColumn = .content
    }
}

struct ShownAgents: Equatable {
    struct Store: Equatable {
        let computer: String
        let workspace: String
    }

    let store: Store
    let detail: String?
}

struct ShownSection: Equatable {
    let computer: String
    let workspace: String
    let section: String
}

struct PairingOverlay: View {
    let name: String

    var body: some View {
        VStack(spacing: 14) {
            ProgressView()
                .controlSize(.large)
            Text("Pairing with \(name)…")
                .font(.headline)
        }
        .padding(28)
        .glassEffect(.regular, in: .rect(cornerRadius: 28))
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(.black.opacity(0.15))
    }
}

struct WelcomeView: View {
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        ContentUnavailableView {
            Label("Pair a Mac", systemImage: "macbook.and.iphone")
        } description: {
            Text("On your Mac, open Clubmate Settings → Devices → Pair Device…, then scan the code. Both devices need Tailscale.")
        } actions: {
            Button("Pair a Mac", systemImage: "qrcode.viewfinder") { navigation.showingAddComputer = true }
                .buttonStyle(.glassProminent)
                .controlSize(.large)
        }
    }
}
