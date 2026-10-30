import SwiftUI

import CompanionKit

struct RootView: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @Environment(PairingFlow.self) private var pairing
    @State private var visibility: NavigationSplitViewVisibility = .all

    var body: some View {
        @Bindable var navigation = navigation
        NavigationSplitView(columnVisibility: $visibility, preferredCompactColumn: $navigation.preferredColumn) {
            SidebarView()
        } content: {
            content
        } detail: {
            detail
        }
        .sheet(isPresented: $navigation.showingAddComputer) {
            AddComputerView()
        }
        .sheet(item: Binding(get: { navigation.showingAddComputer ? nil : pairing.review }, set: { pairing.review = $0 })) { review in
            PairingConfirmView(review: review)
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

    private var context: PluginContext? {
        guard let item = navigation.sidebar, let connection = model.connection(item.computer) else { return nil }
        return PluginContext(model: model, connection: connection, navigation: navigation)
    }

    private func selectDefault() {
        if let item = navigation.sidebar, model.connection(item.computer) != nil { return }
        navigation.sidebar = nil
        guard model.computers.count == 1, let computer = model.computers.first else { return }
        navigation.sidebar = SidebarItem(computer: computer.id, plugin: AgentsPluginInfo.id)
        navigation.preferredColumn = .content
    }
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
