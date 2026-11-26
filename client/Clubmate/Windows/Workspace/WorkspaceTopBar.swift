import SwiftUI

extension WorkspaceWindow {
    var topBar: some View {
        HStack(spacing: theme.spacing.m) {
            titleItem
            Spacer(minLength: theme.spacing.l)
            Button { windowID.map(windows.toggleInbox) } label: {
                TitlePill {
                    Image(systemName: "bell")
                        .font(theme.ui)
                    CountBadge(count: inboxTotal, needsYou: !app.notifications.hub.pinned.isEmpty)
                }
            }
            .buttonStyle(.plain)
            .help("Notifications (⇧⌘I)")
            .accessibilityLabel(inboxTotal > 0 ? "Notifications, \(inboxTotal) unread" : "Notifications")
            .disabled(windowID == nil)
            .popover(isPresented: inboxShown, arrowEdge: .bottom) {
                InboxView()
                    .environment(app)
                    .environment(windows)
                    .environment(\.theme, theme)
            }
            Button { windowID.map(windows.toggleSwitcher) } label: {
                TitlePill {
                    Image(systemName: "square.on.square")
                        .font(theme.ui)
                    Text("\(workspaceCount)")
                        .monospacedDigit()
                    CountBadge(count: otherUnread, needsYou: false)
                }
            }
            .buttonStyle(.plain)
            .help("Workspaces (⌃⌘W)")
            .accessibilityLabel(otherUnread > 0 ? "Workspaces, \(workspaceCount), \(otherUnread) unread" : "Workspaces, \(workspaceCount)")
            .disabled(windowID == nil)
        }
        .padding(.leading, titleBar.lights + theme.spacing.l)
        .padding(.trailing, theme.spacing.l)
        .frame(maxWidth: .infinity)
        .frame(height: titleBar.height)
        .background(TitleBarArea { titleBar = $0 })
        .background(theme.surface1)
        .overlay(alignment: .bottom) {
            Rectangle().fill(theme.border).frame(height: 1)
        }
    }

    @ViewBuilder private var titleItem: some View {
        if let worktreeSession {
            WorktreeTitleMenu(session: worktreeSession, workspace: workspaceName, home: activeIsHome, remote: app.remoteDevices.status, newFolders: app.newFolders(worktreeSession.workspaceID).count) {
                app.markFoldersSeen(worktreeSession.workspaceID)
            }
        } else if activeIsHome || titleShown {
            HStack(spacing: theme.spacing.m) {
                if activeIsHome {
                    Image(systemName: "house")
                        .font(theme.ui)
                        .foregroundStyle(theme.muted)
                }
                Text(activeIsHome ? workspaceName : title)
                    .font(theme.strong)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
            .allowsHitTesting(false)
        }
    }

    private var workspaceCount: Int {
        windowID.flatMap { windows.window($0)?.workspaces.count } ?? 0
    }

    private var inboxTotal: Int {
        app.notifications.hub.bellCount
    }

    private var inboxShown: Binding<Bool> {
        Binding { windowID != nil && windows.inboxWindow == windowID } set: { shown in
            if !shown, windows.inboxWindow == windowID { windows.hideInbox() }
        }
    }

    private var otherUnread: Int {
        let workspaces = windowID.flatMap { windows.window($0)?.workspaces } ?? []
        return workspaces.filter { $0 != active }.reduce(0) { $0 + app.notifications.unseen($1) }
    }
}
