import SwiftUI

import ClubmateKit

struct WorktreeTitleMenu: View {
    let session: ProjectSession
    let title: String

    @State private var shown = false
    @Environment(\.theme) private var theme

    private static let maxWidth: CGFloat = 360

    var body: some View {
        Button { shown.toggle() } label: {
            Text(title)
                .font(.headline)
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(maxWidth: Self.maxWidth)
                .padding(theme.spacing.capsule)
                .onHover { inside in
                    if inside { session.worktreeMenuOpened() }
                }
        }
        .buttonStyle(.plain)
        .fixedSize()
        .help("Worktrees")
        .popover(isPresented: $shown, arrowEdge: .bottom) {
            WorktreeSwitcher(session: session) { shown = false }
        }
        .testPresenter("worktree", $shown)
    }
}

private struct WorktreeSwitcher: View {
    let session: ProjectSession
    let close: () -> Void

    @State private var model: FuzzyPickerModel
    @Environment(\.theme) private var theme

    init(session: ProjectSession, close: @escaping () -> Void) {
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
                .background(hovering ? theme.foreground.opacity(0.06) : .clear, ignoresSafeAreaEdges: [])
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}
