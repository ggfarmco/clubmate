import SwiftUI

import ClubmateKit

struct WelcomeView: View {
    let windowID: String?

    @Environment(AppModel.self) private var app
    @Environment(WindowRouter.self) private var windows
    @Environment(WorkspaceActions.self) private var actions
    @Environment(\.theme) private var theme

    var body: some View {
        switch app.phase {
        case .connecting:
            EmptyNodeView(text: "Starting core…")
        case .failed(let message):
            VStack(spacing: theme.gutter * 1.5) {
                Text(message)
                    .font(theme.ui)
                    .foregroundStyle(theme.danger)
                    .multilineTextAlignment(.center)
                    .textSelection(.enabled)
                Button("Retry") { app.retry() }
            }
            .padding(theme.gutter * 3)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .ready, .reconnecting:
            welcome
        }
    }

    private var welcome: some View {
        VStack(alignment: .leading, spacing: theme.gutter * 3) {
            Text("Clubmate")
                .font(theme.title)
                .foregroundStyle(theme.foreground)
            HStack(spacing: theme.gutter) {
                WelcomeAction(title: "Open Folder…", icon: "folder", shortcut: "⌘O") { actions.openFolder(in: windowID) }
                WelcomeAction(title: "New Terminal", icon: "terminal", shortcut: "⇧⌘N") { actions.newHomeTerminal(in: windowID) }
            }
            if !recents.isEmpty {
                VStack(alignment: .leading, spacing: theme.gutter / 2) {
                    Text("Recent")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                    ScrollView {
                        LazyVStack(spacing: 0) {
                            ForEach(recents, id: \.id) { workspace in
                                RecentRow(workspace: workspace, open: open.contains(workspace.id)) {
                                    windows.open(workspace.id, in: windowID)
                                } remove: {
                                    actions.remove(workspace.id)
                                }
                            }
                        }
                    }
                    .frame(maxHeight: 360)
                    .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .frame(width: 440)
        .padding(theme.gutter * 3)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var recents: [Workspace] {
        Workspace.recents(app.workspaceStore?.workspaces ?? [])
    }

    private var open: Set<String> {
        windows.openWorkspaces
    }
}

private struct WelcomeAction: View {
    let title: String
    let icon: String
    let shortcut: String
    let action: () -> Void

    @Environment(\.theme) private var theme
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: theme.gutter) {
                Image(systemName: icon)
                Text(title)
                Spacer(minLength: theme.gutter)
                Text(shortcut)
                    .foregroundStyle(theme.muted)
            }
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .padding(.horizontal, theme.gutter * 1.5)
            .frame(maxWidth: .infinity, minHeight: theme.rowHeight * 2)
            .background(RoundedRectangle(cornerRadius: 6).fill(theme.foreground.opacity(hovering ? 0.10 : 0.05)))
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.border))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}

private struct RecentRow: View {
    let workspace: Workspace
    let open: Bool
    let select: () -> Void
    let remove: () -> Void

    @Environment(\.theme) private var theme
    @State private var hovering = false

    var body: some View {
        HStack(spacing: theme.gutter) {
            VStack(alignment: .leading, spacing: theme.spacing.xs) {
                Text(workspace.name)
                    .font(theme.ui)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                Text(folders)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
            }
            Spacer(minLength: theme.gutter)
            if open {
                Text("open")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.accentInk)
            }
            if hovering {
                Button(action: remove) {
                    Image(systemName: "trash")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
                .buttonStyle(.plain)
                .help("Remove Workspace…")
                .accessibilityLabel("Remove Workspace…")
            }
        }
        .padding(.horizontal, theme.gutter)
        .padding(.vertical, theme.gutter / 2)
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(hovering ? theme.hover : .clear))
        .contentShape(Rectangle())
        .onTapGesture(perform: select)
        .onHover { hovering = $0 }
    }

    private var folders: String {
        workspace.folders.map { ($0.path as NSString).abbreviatingWithTildeInPath }.joined(separator: ", ")
    }
}
