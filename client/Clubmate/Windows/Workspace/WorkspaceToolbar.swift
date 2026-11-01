import SwiftUI

extension WorkspaceWindow {
    @ToolbarContentBuilder var toolbarItems: some ToolbarContent {
        if let worktreeSession {
            ToolbarItem(placement: .navigation) {
                WorktreeTitleMenu(session: worktreeSession, workspace: workspaceName, remote: app.remoteDevices.status)
            }
            .sharedBackgroundVisibility(.hidden)
        }
        ToolbarSpacer(.flexible)
        ToolbarItem(placement: .primaryAction) {
            Button { windowID.map(windows.toggleInbox) } label: {
                TitlePill {
                    Image(systemName: "bell")
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
        }
        .sharedBackgroundVisibility(.hidden)
        ToolbarSpacer(.fixed, placement: .primaryAction)
        ToolbarItem(placement: .primaryAction) {
            Button { windowID.map(windows.toggleSwitcher) } label: {
                TitlePill {
                    Image(systemName: "square.on.square")
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
        .sharedBackgroundVisibility(.hidden)
        ToolbarSpacer(.fixed, placement: .primaryAction)
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
