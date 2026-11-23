import SwiftUI

import CompanionKit

struct ChatPanel: View {
    let connection: ComputerConnection
    @State private var ui: WorkspaceUI
    @State private var owner = UUID()
    @Environment(AppNavigation.self) private var navigation
    static let width: CGFloat = 400
    static let rowHeight: CGFloat = 56
    private static let slot = WorkspaceUI.chatSlot(WikiPluginInfo.id)

    init(connection: ComputerConnection, workspace: String) {
        self.connection = connection
        _ui = State(initialValue: connection.workspaceUI(workspace))
    }

    static func subtitle(in tree: SDUINode?) -> String? {
        guard let tree else { return nil }
        if StackNode.chromeBar(tree) {
            return tree.children.first { $0.type == "text" && $0.string("style") == "muted" }?.string("text").flatMap { $0.isEmpty ? nil : $0 }
        }
        return tree.children.lazy.compactMap { subtitle(in: $0) }.first
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Text("Chat")
                    .font(.headline)
                if let subtitle = Self.subtitle(in: ui.view(Self.slot)?.tree) {
                    Text(subtitle)
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                        .accessibilityIdentifier("chat.subtitle")
                }
                Spacer()
                Button {
                    withAnimation(.easeInOut(duration: 0.3)) { navigation.toggleChat() }
                } label: {
                    Image(systemName: "xmark")
                        .font(.body.weight(.semibold))
                        .foregroundStyle(Color.captionInk)
                        .frame(width: 44, height: 44)
                        .contentShape(.rect)
                }
                .accessibilityLabel("Close Chat")
                .accessibilityIdentifier("chat.close")
            }
            .padding(.leading, 16)
            .padding(.trailing, 4)
            Divider()
            if ui.absent.contains(Self.slot) {
                ContentUnavailableView("Chat Isn't on This Mac Yet", systemImage: "bubble.left", description: Text("Update Clubmate on your Mac to chat here."))
            } else {
                SectionSlot(ui: ui, slot: Self.slot, connection: connection)
            }
        }
        .background(Color(.systemBackground))
        .environment(\.sduiRowHeight, Self.rowHeight)
        .modifier(TabShownWatch(ui: ui, slots: [Self.slot], owner: owner))
        .task {
            guard navigation.takeChatOpen() else { return }
            try? await ui.openChat(section: WikiPluginInfo.id)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("chat.panel")
    }
}

struct ChatDock<Content: View>: View {
    let tab: CompanionTab
    let connection: ComputerConnection
    @ViewBuilder let content: Content
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        HStack(spacing: 0) {
            content
            if tab == .wiki, navigation.chatDocked, !navigation.readingView, let workspace = navigation.workspace(for: connection) {
                Divider()
                ChatPanel(connection: connection, workspace: workspace.id)
                    .id(workspace.id)
                    .frame(width: ChatPanel.width)
                    .transition(.move(edge: .trailing))
            }
        }
    }
}

struct ChatButton: ViewModifier {
    let connection: ComputerConnection
    let workspace: String
    let visible: Bool
    @Environment(AppNavigation.self) private var navigation

    func body(content: Content) -> some View {
        content
            .toolbar {
                if UIDevice.current.userInterfaceIdiom == .pad {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            withAnimation(.easeInOut(duration: 0.3)) { navigation.toggleChat() }
                        } label: {
                            Image(systemName: "bubble.left.and.text.bubble.right")
                                .symbolVariant(navigation.chatShown ? .fill : .none)
                        }
                        .accessibilityLabel("Chat")
                        .accessibilityIdentifier("wiki.chat")
                    }
                }
            }
            .sheet(isPresented: Binding(get: { visible && navigation.chatShown && !navigation.chatDocked }, set: { if !$0, navigation.chatShown { navigation.toggleChat() } })) {
                ChatPanel(connection: connection, workspace: workspace)
                    .presentationDetents([.medium, .large])
            }
    }
}
