import SwiftUI

import ClubmateKit

struct InboxView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @State private var kind: NotificationKind?

    private static let width: CGFloat = 380
    private static let height: CGFloat = 480
    private static let tick: TimeInterval = 30

    var body: some View {
        let notifications = app.notifications
        let pinned = notifications.hub.pinned
        let history = notifications.hub.inbox.history(kind)
        VStack(alignment: .leading, spacing: 0) {
            header(notifications)
            Divider()
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    if !pinned.isEmpty {
                        caption("Waiting for you")
                        ForEach(pinned) { pin in
                            switch pin {
                            case .permission(let alert):
                                row(
                                    kind: .permission,
                                    title: alert.title,
                                    message: alert.body,
                                    source: HubNotification(alert).source(workspaceName: app.project(alert.workspace)?.name),
                                    date: nil,
                                    unread: true
                                ) { open { notifications.open(alert) } }
                            case .input(let entry):
                                entryRow(entry, notifications)
                            }
                        }
                        Divider()
                        caption("History")
                    }
                    if history.isEmpty {
                        Text("No notifications")
                            .font(theme.ui)
                            .foregroundStyle(theme.muted)
                            .padding(theme.gutter)
                    }
                    ForEach(history) { entry in
                        entryRow(entry, notifications)
                    }
                }
            }
            if notifications.systemOff {
                Divider()
                footer(notifications)
            }
        }
        .frame(width: Self.width)
        .frame(maxHeight: Self.height)
    }

    private func header(_ notifications: Notifications) -> some View {
        HStack(spacing: theme.gutter) {
            Text("Notifications")
                .font(theme.title)
                .lineLimit(1)
                .layoutPriority(1)
            Spacer(minLength: 0)
            Menu {
                Picker("Kind", selection: $kind) {
                    Text("All").tag(NotificationKind?.none)
                    ForEach(NotificationKind.allCases, id: \.self) { kind in
                        Text(kind.label).tag(Optional(kind))
                    }
                }
                .labelsHidden()
                .pickerStyle(.inline)
            } label: {
                Text(kind?.label ?? "All").font(theme.ui)
            }
            .menuStyle(.borderlessButton)
            .fixedSize()
            .help("Show kind")
            .testMenu("inbox.kind", ["All"].map { LayoutMenuItem(title: $0) } + NotificationKind.allCases.map { LayoutMenuItem(title: $0.label) })
            ViewThatFits(in: .horizontal) {
                Button("Mark All Read") { notifications.handleAll() }
                Button { notifications.handleAll() } label: { Image(systemName: "checkmark.circle") }
                    .help("Mark All Read")
                    .accessibilityLabel("Mark All Read")
            }
            .disabled(notifications.hub.inbox.unhandled.isEmpty)
        }
        .padding(theme.gutter)
    }

    private func caption(_ text: String) -> some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(theme.muted)
            .padding(.horizontal, theme.gutter)
            .padding(.top, theme.spacing.l)
            .padding(.bottom, theme.spacing.s)
    }

    private func entryRow(_ entry: InboxEntry, _ notifications: Notifications) -> some View {
        row(
            kind: entry.note.kind,
            title: entry.note.what,
            message: entry.note.message,
            source: entry.note.source(workspaceName: app.project(entry.note.workspace)?.name),
            date: entry.date,
            unread: !entry.handled
        ) { open { notifications.open(entry) } }
    }

    private func row(kind: NotificationKind, title: String, message: String, source: String, date: Date?, unread: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(alignment: .top, spacing: theme.gutter) {
                Image(systemName: kind.symbol)
                    .foregroundStyle(color(kind))
                    .frame(width: theme.indent + theme.spacing.s)
                VStack(alignment: .leading, spacing: theme.spacing.xs) {
                    HStack(spacing: theme.spacing.m) {
                        Text(title)
                            .font(theme.ui)
                            .foregroundStyle(theme.foreground)
                            .lineLimit(1)
                        Spacer(minLength: theme.spacing.s)
                        if let date {
                            TimelineView(.periodic(from: .now, by: Self.tick)) { context in
                                Text(date.formatted(.relative(presentation: .named, unitsStyle: .abbreviated)))
                                    .id(context.date)
                                    .font(theme.uiSmall)
                                    .foregroundStyle(theme.muted)
                            }
                        }
                        if unread {
                            Circle().fill(theme.accent).frame(width: theme.spacing.m, height: theme.spacing.m)
                        }
                    }
                    Text(message)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.foreground)
                        .lineLimit(2)
                    Text(source)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
            }
            .padding(.horizontal, theme.gutter)
            .padding(.vertical, theme.spacing.m)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(unread ? "Unread, " : "")\(kind.label): \(title). \(message). \(source)")
    }

    private func footer(_ notifications: Notifications) -> some View {
        HStack(spacing: theme.gutter) {
            Image(systemName: "bell.slash")
            Text("macOS notifications are off")
                .font(theme.uiSmall)
            Spacer()
            Button("Open System Settings") { notifications.openSystemSettings() }
        }
        .foregroundStyle(theme.muted)
        .padding(theme.gutter)
    }

    private func open(_ action: () -> Void) {
        app.hideInbox()
        action()
    }

    private func color(_ kind: NotificationKind) -> Color {
        switch kind {
        case .needsInput, .permission: theme.warning
        case .stuck: theme.danger
        case .done: theme.success
        case .info: theme.muted
        }
    }
}
