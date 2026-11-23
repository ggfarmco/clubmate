import AppKit
import SwiftUI

import ClubmateKit

enum WelcomeEntry: Hashable {
    case openFolder
    case workspace(String)

    static func all(_ workspaces: [Workspace]) -> [WelcomeEntry] {
        [.openFolder]
            + (Workspace.home(in: workspaces).map { [.workspace($0.id)] } ?? [])
            + Workspace.recents(workspaces).map { .workspace($0.id) }
    }
}

struct WelcomeView: View {
    let windowID: String?
    var cursor: WelcomeEntry?
    var picked: (() -> Void)?

    @Environment(AppModel.self) private var app
    @Environment(WindowRouter.self) private var windows
    @Environment(WorkspaceActions.self) private var actions
    @Environment(\.theme) private var theme
    @State private var height: CGFloat = 0
    @State private var watch = StartupWatch()
    @State private var own: WelcomeEntry = .openFolder

    static let recentPlaceholders = 8

    private var entries: [WelcomeEntry] {
        WelcomeEntry.all(app.workspaceStore?.workspaces ?? [])
    }

    private var current: WelcomeEntry? {
        if picked != nil { return cursor }
        return entries.contains(own) ? own : .openFolder
    }

    private func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        guard !flags.contains(.shift),
              let action = ListKeys.welcome(keyCode: event.keyCode, control: flags.contains(.control), command: flags.contains(.command), option: flags.contains(.option))
        else { return false }
        let list = entries
        switch action {
        case .move(let delta):
            guard let index = list.firstIndex(of: current ?? .openFolder) else { return false }
            own = list[SwitcherGrid.step(cursor: index, count: list.count, by: delta)]
        case .open:
            choose(current ?? .openFolder)
        }
        return true
    }

    private var keysActive: Bool {
        picked == nil && !(windowID != nil && windows.switcherWindow == windowID)
    }

    var body: some View {
        welcome
            .keyMonitor(active: keysActive, handle: handle)
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
            VStack(alignment: .leading, spacing: theme.spacing.xs) {
                Text("Clubmate")
                    .font(theme.title)
                    .foregroundStyle(theme.foreground)
                Text("Your workshop, set up your way.")
                    .font(theme.ui)
                    .foregroundStyle(theme.muted)
            }
            WelcomeAction(title: "Open folder…", icon: "folder", shortcut: "⌘O", highlighted: current == .openFolder) { choose(.openFolder) }
            if let setup = app.setup.state, setup.shown {
                SetupRow(setup: setup, failure: app.setup.fixError) {
                    Task { await app.setup.fix() }
                }
            }
            if let home = Workspace.home(in: app.workspaceStore?.workspaces ?? []) {
                HomeRow(workspace: home, open: open.contains(home.id), highlighted: current == .workspace(home.id)) {
                    choose(.workspace(home.id))
                }
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
                    ScrollViewReader { proxy in
                        ScrollView {
                            LazyVStack(spacing: 0) {
                                ForEach(recents, id: \.id) { workspace in
                                    RecentRow(workspace: workspace, open: open.contains(workspace.id), highlighted: current == .workspace(workspace.id)) {
                                        choose(.workspace(workspace.id))
                                    } remove: {
                                        actions.remove(workspace.id)
                                    }
                                    .id(workspace.id)
                                }
                            }
                        }
                        .onChange(of: current) { _, entry in
                            if case .workspace(let id) = entry { proxy.scrollTo(id) }
                        }
                    }
                    .frame(maxHeight: 360)
                    .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .frame(width: 440)
        .padding(theme.gutter * 3)
        .padding(.top, picked == nil ? height * 0.25 : 0)
        .frame(maxWidth: picked == nil ? .infinity : nil, maxHeight: picked == nil ? .infinity : nil, alignment: .top)
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
        .overlay(alignment: .bottom) {
            if picked == nil {
                Text("↑↓ choose · ⏎ open · ⇧⌘N new terminal")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .padding(.bottom, theme.gutter * 1.5)
            }
        }
    }

    private func choose(_ entry: WelcomeEntry) {
        if actions.pick(entry, in: windowID) { picked?() }
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
    var highlighted = false
    let action: () -> Void

    @Environment(\.theme) private var theme
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: theme.gutter) {
                Image(systemName: icon)
                Text(title)
                    .fontWeight(.semibold)
                Spacer(minLength: theme.gutter)
                Text(shortcut)
                    .opacity(0.75)
            }
            .font(theme.ui)
            .foregroundStyle(theme.onAccent)
            .padding(.horizontal, theme.gutter * 1.5)
            .frame(maxWidth: .infinity, minHeight: theme.rowHeight * 1.5)
            .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.accent.opacity(hovering ? 0.9 : 1)))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(highlighted ? theme.foreground : .clear, lineWidth: theme.focusRing))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .testNode("welcome.open", role: "button", label: title)
    }
}

private struct SetupRow: View {
    let setup: AgentSetup
    let failure: String?
    let fix: () -> Void

    private var detail: String { failure ?? setup.detail }

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.m) {
            Image(systemName: "exclamationmark.circle")
                .foregroundStyle(theme.warning)
                .accessibilityHidden(true)
            HStack(spacing: theme.spacing.s) {
                Text(setup.title)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .truncationMode(.tail)
                if !detail.isEmpty {
                    Text("· \(detail)")
                        .foregroundStyle(failure == nil ? theme.muted : theme.danger)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .layoutPriority(failure == nil ? 1 : 0)
                        .fixedSize(horizontal: failure == nil, vertical: false)
                }
            }
            .font(theme.ui)
            .help(detail.isEmpty ? setup.title : "\(setup.title) · \(detail)")
            Spacer(minLength: theme.gutter)
            if !setup.fixLabel.isEmpty {
                CardButton(label: setup.fixLabel, action: fix)
                    .testNode("welcome.setup.fix", role: "button", label: setup.fixLabel)
            }
        }
        .padding(.leading, theme.gutter)
        .padding(.trailing, theme.spacing.s)
        .frame(minHeight: theme.rowHeight)
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.toneBg(theme.warning)))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(theme.toneBorder(theme.warning)))
        .accessibilityElement(children: .contain)
        .testNode("welcome.setup", role: "group", label: setup.title)
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

private struct HomeRow: View {
    let workspace: Workspace
    let open: Bool
    var highlighted = false
    let select: () -> Void

    @Environment(\.theme) private var theme
    @State private var hovering = false

    var body: some View {
        Button(action: select) {
            HStack(spacing: theme.gutter) {
                Image(systemName: "house")
                    .foregroundStyle(theme.muted)
                    .accessibilityHidden(true)
                (Text("Home").foregroundStyle(theme.foreground)
                    + Text(" · agents across all projects").foregroundStyle(theme.muted))
                    .font(theme.ui)
                    .lineLimit(1)
                    .truncationMode(.label)
                Spacer(minLength: theme.gutter)
                if open {
                    Text("open")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.accentInk)
                }
                Text("⇧⌘H")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .padding(.horizontal, theme.spacing.s)
                    .overlay(RoundedRectangle(cornerRadius: theme.radius.s).stroke(theme.border))
            }
            .padding(.horizontal, theme.gutter)
            .frame(minHeight: theme.rowHeight)
            .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(hovering || highlighted ? theme.hover : .clear))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(highlighted ? theme.accent : .clear, lineWidth: theme.focusRing))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .accessibilityLabel(workspace.name)
        .testNode("welcome.home", role: "button", label: workspace.name)
    }
}

private struct RecentRow: View {
    let workspace: Workspace
    let open: Bool
    var highlighted = false
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
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(hovering || highlighted ? theme.hover : .clear))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(highlighted ? theme.accent : .clear, lineWidth: theme.focusRing))
        .contentShape(Rectangle())
        .onTapGesture(perform: select)
        .onHover { hovering = $0 }
    }

    private var folders: String {
        workspace.folders.map { ($0.path as NSString).abbreviatingWithTildeInPath }.joined(separator: ", ")
    }
}
