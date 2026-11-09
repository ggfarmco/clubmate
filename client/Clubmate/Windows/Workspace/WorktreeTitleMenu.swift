import SwiftUI

import ClubmateKit

struct WorktreeTitleMenu: View {
    let session: WorkspaceSession
    let workspace: String
    let remote: RemoteStatus?
    var newFolders = 0
    var opened: () -> Void = {}

    @Environment(\.theme) private var theme

    private static let maxWidth: CGFloat = 360

    private var shown: Binding<Bool> {
        Binding(
            get: { [session] in session.overlay == .worktrees },
            set: { [session] in
                if $0 { session.openWorktreePicker() } else { session.closeWorktreePicker() }
            }
        )
    }

    var body: some View {
        Button { session.toggleWorktreePicker() } label: {
            TitlePill(pressed: shown.wrappedValue) {
                if let remote, remote.titleDot {
                    MacDot(colour: remote.computer?.colour)
                        .help(remote.computer.map { "\($0.name) · \(Theme.identityName($0.colour))" } ?? "")
                }
                Text(workspace)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .layoutPriority(1)
                if newFolders > 0 {
                    Text("+\(newFolders)")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.accentInk)
                        .padding(.horizontal, theme.spacing.m)
                        .background(Capsule().fill(theme.toneBg(theme.accent)))
                        .fixedSize()
                }
                if let current = session.worktrees.current {
                    Text("/")
                        .foregroundStyle(theme.faint)
                        .fixedSize()
                    if let folder = session.worktrees.currentFolder {
                        Image(systemName: "folder")
                            .foregroundStyle(theme.muted)
                        Text(folder)
                            .foregroundStyle(theme.foreground)
                            .lineLimit(1)
                            .truncationMode(.tail)
                            .layoutPriority(1)
                        Text("▸")
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.faint)
                            .fixedSize()
                    } else {
                        Image(systemName: "arrow.triangle.branch")
                            .foregroundStyle(theme.muted)
                    }
                    Text(current.name)
                        .foregroundStyle(current.detached ? theme.faint : theme.foreground)
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .layoutPriority(1)
                }
                Image(systemName: "chevron.down")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
            }
            .frame(maxWidth: Self.maxWidth)
            .onHover { inside in
                if inside { session.worktreeMenuOpened() }
            }
            .onGeometryChange(for: CGFloat.self) { $0.frame(in: .global).minX } action: { [session] in
                session.worktreePillLeading = $0
            }
        }
        .buttonStyle(.plain)
        .fixedSize()
        .help(title)
        .testNode("worktree.pill", role: "button", label: title)
        .testPresenter("worktree", shown)
        .onChange(of: shown.wrappedValue) { _, shown in
            guard shown else { return }
            let opened = opened
            Task { [session] in
                await session.worktreePicker.load()
                opened()
            }
        }
    }

    private var title: String {
        let path = [session.worktrees.currentFolder, session.worktrees.current?.name].compactMap(\.self).joined(separator: " ▸ ")
        return path.isEmpty ? "Worktrees" : "\(workspace) / \(path)"
    }
}
