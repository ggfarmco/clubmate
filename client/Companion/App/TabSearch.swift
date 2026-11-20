import SwiftUI

import CompanionKit

struct TabSearch: ViewModifier {
    let tab: CompanionTab
    @Binding var text: String
    let prompt: String
    @Environment(AppNavigation.self) private var navigation
    @Environment(SDUILiveFocus.self) private var liveFocus: SDUILiveFocus?
    @FocusState private var focused: Bool
    @State private var laidOut = false

    func body(content: Content) -> some View {
        content
            .environment(\.tabListLaidOut, laidOut)
            .onAppear { DispatchQueue.main.async { laidOut = true } }
            .searchable(text: $text, placement: .navigationBarDrawer(displayMode: .automatic), prompt: prompt)
            .searchFocused($focused)
            .onChange(of: navigation.searchRequests) { _, _ in
                if navigation.tab == tab { focused = true }
            }
            .onChange(of: focused) { _, now in
                liveFocus?.field = .null
                liveFocus?.focused = now
            }
            .onChange(of: liveFocus?.dismissals) { _, _ in focused = false }
    }
}

struct TabListRows<Content: View>: View {
    @ViewBuilder let content: () -> Content
    @Environment(\.tabListLaidOut) private var laidOut

    var body: some View {
        if laidOut {
            content()
        }
    }
}

extension EnvironmentValues {
    @Entry var tabListLaidOut = true
}

extension View {
    func tabSearch(_ tab: CompanionTab, text: Binding<String>, prompt: String) -> some View {
        modifier(TabSearch(tab: tab, text: text, prompt: prompt))
    }
}

struct TabSearchCommands: Commands {
    let navigation: AppNavigation

    var body: some Commands {
        CommandGroup(after: .textEditing) {
            Button("Search", systemImage: "magnifyingglass") { navigation.focusSearch() }
                .keyboardShortcut("f", modifiers: [.command, .shift])
                .disabled(!navigation.tabHasSearch)
        }
    }
}
