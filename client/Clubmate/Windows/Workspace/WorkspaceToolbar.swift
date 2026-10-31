import SwiftUI

extension WorkspaceWindow {
    @ToolbarContentBuilder var toolbarItems: some ToolbarContent {
        if let worktreeSession {
            ToolbarItem(placement: .navigation) {
                WorktreeTitleMenu(session: worktreeSession, title: title)
            }
        }
        ToolbarSpacer(.flexible)
        ToolbarItem(placement: .primaryAction) {
            Button { windowID.map(app.toggleInbox) } label: {
                HStack(spacing: theme.spacing.s) {
                    Image(systemName: inboxTotal > 0 ? "bell.badge" : "bell")
                    UnreadBadge(count: inboxTotal)
                }
                .padding(theme.spacing.capsule)
            }
            .help("Notifications (⇧⌘I)")
            .accessibilityLabel(inboxTotal > 0 ? "Notifications, \(inboxTotal) unread" : "Notifications")
            .disabled(windowID == nil)
            .popover(isPresented: inboxShown, arrowEdge: .bottom) {
                InboxView()
                    .environment(app)
                    .environment(\.theme, theme)
            }
        }
        ToolbarItem(placement: .primaryAction) {
            Button { windowID.map(app.toggleSwitcher) } label: {
                HStack(spacing: theme.spacing.s) {
                    Text("▦ \(workspaceCount)")
                        .monospacedDigit()
                    UnreadBadge(count: otherUnread)
                }
                .padding(theme.spacing.capsule)
            }
            .help("Workspaces (⌃⌘W)")
            .accessibilityLabel(otherUnread > 0 ? "Workspaces, \(workspaceCount), \(otherUnread) unread" : "Workspaces, \(workspaceCount)")
            .disabled(windowID == nil)
        }
        ToolbarSpacer(.fixed, placement: .primaryAction)
    }

    private var workspaceCount: Int {
        windowID.flatMap { app.window($0)?.workspaces.count } ?? 0
    }

    private var inboxTotal: Int {
        app.notifications.hub.bellCount
    }

    private var inboxShown: Binding<Bool> {
        Binding { windowID != nil && app.inboxWindow == windowID } set: { shown in
            if !shown, app.inboxWindow == windowID { app.hideInbox() }
        }
    }

    private var otherUnread: Int {
        let workspaces = windowID.flatMap { app.window($0)?.workspaces } ?? []
        return workspaces.filter { $0 != active }.reduce(0) { $0 + app.notifications.unseen($1) }
    }
}
