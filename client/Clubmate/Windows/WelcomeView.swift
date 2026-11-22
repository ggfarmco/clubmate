import SwiftUI

import ClubmateKit

struct WelcomeView: View {
    let windowID: String?

    @Environment(AppModel.self) private var app
    @Environment(WindowRouter.self) private var windows
    @Environment(WorkspaceActions.self) private var actions
    @Environment(\.theme) private var theme
    @State private var height: CGFloat = 0
    @State private var watch = StartupWatch()

    static let recentPlaceholders = 8

    var body: some View {
        welcome
            .task(id: starting) {
                guard starting else { return }
                await watch.run()
            }
            .onChange(of: watch.label(.core)) { old, new in
                guard old == nil, let new, starting else { return }
                AccessibilityNotification.Announcement(new).post()
            }
    }

    private var starting: Bool {
        app.workspaceStore == nil && app.startFailure == nil
    }

    private var welcome: some View {
        VStack(alignment: .leading, spacing: theme.gutter * 3) {
            Text("Clubmate")
                .font(theme.title)
                .foregroundStyle(theme.foreground)
            HStack(spacing: theme.gutter) {
                WelcomeAction(title: "Open folder…", icon: "folder", shortcut: "⌘O") { actions.openFolder(in: windowID) }
                WelcomeAction(title: "New terminal", icon: "terminal", shortcut: "⇧⌘N") { actions.newHomeTerminal(in: windowID) }
            }
            if let failure = app.startFailure {
                StartupFailure(message: failure)
            } else if starting, let note = watch.label(.core) {
                StartupNote(text: note, showsLog: watch.showsLog)
            } else if starting, app.launchCache.recentCount > 0 {
                VStack(alignment: .leading, spacing: theme.gutter / 2) {
                    Text("Recent")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                    ForEach(0..<app.launchCache.recentCount, id: \.self) { index in
                        RecentPlaceholder(index: index)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Starting core")
            } else if !recents.isEmpty {
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
        .padding(.top, height * 0.25)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
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

private struct RecentPlaceholder: View {
    let index: Int

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            line(font: theme.ui, width: Self.names[index % Self.names.count])
            line(font: theme.uiSmall, width: Self.folders[index % Self.folders.count])
        }
        .padding(.horizontal, theme.gutter)
        .padding(.vertical, theme.gutter / 2)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func line(font: Font, width: CGFloat) -> some View {
        Text(" ")
            .font(font)
            .hidden()
            .frame(maxWidth: .infinity, alignment: .leading)
            .overlay(alignment: .leading) { SkeletonBar(width: width) }
    }

    private static let names: [CGFloat] = [96, 120, 80, 108]
    private static let folders: [CGFloat] = [220, 180, 240, 200]
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
                .help("Remove workspace…")
                .accessibilityLabel("Remove workspace…")
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
