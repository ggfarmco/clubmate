import AppKit
import SwiftUI

import ClubmateKit

struct InboxView: View {
    @Environment(AppModel.self) private var app
    @Environment(WindowRouter.self) private var windows
    @Environment(\.theme) private var theme
    @State private var kind: NotificationKind?

    private static let width: CGFloat = 380
    private static let height: CGFloat = 480
    private static let tick: TimeInterval = 1

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
                                    kind: alert.kind,
                                    title: alert.title,
                                    message: alert.body,
                                    source: HubNotification(alert).source(workspaceName: app.workspace(alert.workspace)?.name),
                                    date: alert.date,
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
                            .foregroundStyle(.secondary)
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
        .background(PopoverAppearance(dark: theme.isDark))
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
                Button("Mark all read") { notifications.handleAll() }
                Button { notifications.handleAll() } label: { Image(systemName: "checkmark.circle") }
                    .help("Mark all read")
                    .accessibilityLabel("Mark all read")
            }
            .disabled(notifications.hub.inbox.unhandled.isEmpty)
        }
        .padding(theme.gutter)
    }

    private func caption(_ text: String) -> some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(.secondary)
            .padding(.horizontal, theme.gutter)
            .padding(.top, theme.spacing.l)
            .padding(.bottom, theme.spacing.s)
    }

    private func entryRow(_ entry: InboxEntry, _ notifications: Notifications) -> some View {
        row(
            kind: entry.note.kind,
            title: entry.note.what,
            message: entry.note.message,
            source: entry.note.source(workspaceName: app.workspace(entry.note.workspace)?.name),
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
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                            .truncationMode(.label)
                            .help(title)
                            .layoutPriority(1)
                        Spacer(minLength: theme.spacing.s)
                        if let date {
                            TimelineView(.periodic(from: .now, by: Self.tick)) { context in
                                Text(ShortAge.since(date, now: context.date))
                                    .id(context.date)
                                    .font(theme.uiSmall)
                                    .foregroundStyle(.secondary)
                                    .fixedSize()
                            }
                        }
                        if unread {
                            Circle().fill(Color.accentColor).frame(width: theme.spacing.m, height: theme.spacing.m)
                        }
                    }
                    Text(message)
                        .font(theme.uiSmall)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                    Text(source)
                        .font(theme.uiSmall)
                        .foregroundStyle(.secondary)
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
        .foregroundStyle(.secondary)
        .padding(theme.gutter)
    }

    private func open(_ action: () -> Void) {
        windows.hideInbox()
        action()
    }

    private func color(_ kind: NotificationKind) -> Color {
        switch kind {
        case .needsInput, .permission, .question: .orange
        case .stuck: .red
        case .done: .green
        case .info: .secondary
        }
    }
}

private struct PopoverAppearance: NSViewRepresentable {
    let dark: Bool

    func makeNSView(context: Context) -> PopoverAppearanceView {
        PopoverAppearanceView()
    }

    func updateNSView(_ view: PopoverAppearanceView, context: Context) {
        view.dark = dark
    }
}

private final class PopoverAppearanceView: NSView {
    var dark = false {
        didSet { apply() }
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        apply()
    }

    private func apply() {
        window?.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
    }
}
