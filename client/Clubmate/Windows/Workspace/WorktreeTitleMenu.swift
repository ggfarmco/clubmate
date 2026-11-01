import SwiftUI

import ClubmateKit

struct WorktreeTitleMenu: View {
    let session: WorkspaceSession
    let workspace: String
    let remote: RemoteStatus?

    @State private var shown = false
    @Environment(\.theme) private var theme

    private static let maxWidth: CGFloat = 360

    var body: some View {
        Button { shown.toggle() } label: {
            TitlePill(pressed: shown) {
                if let remote, remote.titleDot {
                    MacDot(colour: remote.computer?.colour)
                        .help(remote.computer.map { "\($0.name) · \(Theme.identityName($0.colour))" } ?? "")
                }
                Text(workspace)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .layoutPriority(1)
                if let branch = session.worktrees.current?.name {
                    Text("/")
                        .foregroundStyle(theme.faint)
                        .fixedSize()
                    Image(systemName: "arrow.triangle.branch")
                        .foregroundStyle(theme.muted)
                    Text(branch)
                        .foregroundStyle(theme.foreground)
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
        }
        .buttonStyle(.plain)
        .fixedSize()
        .help(session.worktrees.current.map { "\(workspace) / \($0.name)" } ?? "Worktrees")
        .popover(isPresented: $shown, arrowEdge: .bottom) {
            WorktreeSwitcher(session: session) { shown = false }
        }
        .testPresenter("worktree", $shown)
    }
}

private struct WorktreeSwitcher: View {
    let session: WorkspaceSession
    let close: () -> Void

    @State private var model: FuzzyPickerModel
    @Environment(\.theme) private var theme

    init(session: WorkspaceSession, close: @escaping () -> Void) {
        self.session = session
        self.close = close
        _model = State(initialValue: FuzzyPickerModel(placeholder: "Switch to worktree or branch", source: session.worktrees.switcherSource))
    }

    var body: some View {
        let worktrees = session.worktrees
        VStack(spacing: 0) {
            FuzzyPickerList(model: model, choose: switchTo, dismiss: close)
            theme.border.frame(height: 1)
            VStack(spacing: 0) {
                action("New Worktree…") { session.newWorktree(chooseFolder: false) }
                if !worktrees.linked.isEmpty {
                    action("Remove Worktree…") { session.chooseWorktreeToRemove() }
                }
            }
            .padding(.vertical, theme.spacing.s)
        }
        .frame(width: 360)
        .background(theme.background)
        .task {
            await worktrees.load()
            model.update(model.query)
            await worktrees.loadBranches()
            model.update(model.query)
        }
    }

    private func switchTo(_ item: PickerItem) {
        session.pickWorktree(item)
    }

    private func action(_ title: String, perform: @escaping () -> Void) -> some View {
        SwitcherAction(title: title) {
            close()
            perform()
        }
    }
}

private struct SwitcherAction: View {
    let title: String
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(theme.ui)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, theme.gutter * 1.5)
                .frame(height: theme.rowHeight + 2)
                .background(hovering ? theme.hover : .clear, ignoresSafeAreaEdges: [])
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}
