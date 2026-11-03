import AppKit
import Observation
import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class OnboardingModel {
    enum Editor: String, CaseIterable {
        case neovim = "editor-nvim"
        case vscode = "editor-vscode"
    }

    enum Step {
        case choose
        case vscode
    }

    static let vscodeSlot = "editor-vscode.settings"

    private(set) var show = false
    private(set) var window: String?
    private(set) var step = Step.choose
    var choice = Editor.neovim
    private(set) var busy = false
    private(set) var error: String?
    private(set) var store: WorkspaceUIStore?

    @ObservationIgnored private var client: CoreClient?

    func load(_ client: CoreClient) async {
        self.client = client
        guard let show = try? await client.onboardingState(), show else { return }
        self.show = true
    }

    func claim(_ windowID: String) {
        if window == nil { window = windowID }
    }

    func presented(in windowID: String?) -> Bool {
        show && windowID != nil && window == windowID
    }

    func present(_ shown: Bool, in windowID: String?) {
        if shown, let windowID {
            window = windowID
            show = true
        } else if !shown {
            close()
        }
    }

    func move(_ delta: Int) {
        let all = Editor.allCases
        let index = all.firstIndex(of: choice) ?? 0
        choice = all[(index + delta + all.count) % all.count]
    }

    func finish(_ editor: Editor) async {
        guard !busy else { return }
        busy = true
        defer { busy = false }
        error = nil
        do {
            if let client { try await client.onboardingFinish(editor: editor.rawValue) }
        } catch {
            self.error = error.localizedDescription
            return
        }
        guard editor == .vscode, let client else {
            close()
            return
        }
        step = .vscode
        let store = WorkspaceUIStore(client: client, workspaceID: "")
        self.store = store
        try? await store.start()
    }

    func close() {
        show = false
        let store = store
        self.store = nil
        Task { await store?.stop() }
    }
}

struct OnboardingSheet: View {
    @Bindable var model: OnboardingModel

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            switch model.step {
            case .choose: choose
            case .vscode: vscode
            }
        }
        .padding(theme.spacing.xxl)
        .frame(width: 520, alignment: .topLeading)
        .background(theme.background)
        .keyMonitor(active: true, handle: key)
    }

    @ViewBuilder private var choose: some View {
        header("Choose your editor", "Clubmate opens files, diffs and agent edits in it. You can switch later in Settings.")
        VStack(spacing: theme.spacing.s) {
            OnboardingOption(title: "Neovim", detail: "Comes with Clubmate and runs in the terminal. Nothing to set up.", icon: "terminal", selected: model.choice == .neovim) { model.choice = .neovim }
            OnboardingOption(title: "VS Code", detail: "The VS Code you know, in its own section. Uses the VS Code on this Mac and Microsoft's VS Code Server (about 600 MB, downloaded once).", icon: "curlybraces", selected: model.choice == .vscode) { model.choice = .vscode }
        }
        errorLine
        HStack(spacing: theme.spacing.s) {
            Text("↑↓ choose · ↩ continue · esc keep Neovim")
                .font(theme.uiSmall)
                .foregroundStyle(theme.faint)
                .lineLimit(1)
            Spacer(minLength: theme.spacing.m)
            CardButton(label: model.busy ? "Turning on VS Code…" : "Continue", shortcut: "↩", tone: .primary, disabled: model.busy, isDefault: true) {
                Task { await model.finish(model.choice) }
            }
        }
    }

    @ViewBuilder private var vscode: some View {
        header("Set up VS Code", "VS Code is on and opens your files. Accepting the licence downloads its server; then bring over the extensions of your desktop VS Code.")
        Group {
            if let store = model.store, let view = store.slot(OnboardingModel.vscodeSlot) {
                VStack(alignment: .leading, spacing: theme.spacing.m) {
                    SDUISettingsRows(tree: view.tree, slot: OnboardingModel.vscodeSlot, rev: view.rev) { [store] event in
                        await store.send(event)
                    }
                }
                .environment(\.theme, theme.withSystemFonts(size: NSFont.systemFontSize).withoutGutter())
            } else {
                HStack(spacing: theme.spacing.s) {
                    ProgressView().controlSize(.small)
                    Text("Starting VS Code…")
                        .font(theme.ui)
                        .foregroundStyle(theme.muted)
                }
            }
        }
        .padding(theme.spacing.l)
        .frame(maxWidth: .infinity, alignment: .leading)
        .agentCard()
        HStack(spacing: theme.spacing.s) {
            Text("Downloads and imports go on after you close this.")
                .font(theme.uiSmall)
                .foregroundStyle(theme.faint)
                .lineLimit(2)
            Spacer(minLength: theme.spacing.m)
            CardButton(label: "Done", shortcut: "↩", tone: .primary, isDefault: true) { model.close() }
        }
    }

    private func header(_ title: String, _ detail: String) -> some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            Text(title)
                .font(theme.heading)
                .foregroundStyle(theme.foreground)
            Text(detail)
                .font(theme.ui)
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    @ViewBuilder private var errorLine: some View {
        if let error = model.error {
            Text(error)
                .font(theme.uiSmall)
                .foregroundStyle(theme.danger)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func key(_ event: NSEvent) -> Bool {
        guard event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return false }
        switch (model.step, event.keyCode) {
        case (.choose, 126):
            model.move(-1)
        case (.choose, 125):
            model.move(1)
        case (.choose, 36), (.choose, 76):
            Task { await model.finish(model.choice) }
        case (.choose, 53):
            Task { await model.finish(.neovim) }
        case (.vscode, 36), (.vscode, 76), (.vscode, 53):
            model.close()
        default:
            return false
        }
        return true
    }
}

private struct OnboardingOption: View {
    let title: String
    let detail: String
    let icon: String
    let selected: Bool
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(alignment: .top, spacing: theme.spacing.m) {
                Image(systemName: SymbolName.resolve(icon))
                    .font(theme.heading)
                    .foregroundStyle(selected ? theme.accentInk : theme.muted)
                    .frame(width: 24)
                VStack(alignment: .leading, spacing: theme.spacing.xs) {
                    Text(title)
                        .font(theme.strong)
                        .foregroundStyle(theme.foreground)
                    Text(detail)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 0)
                Image(systemName: selected ? "largecircle.fill.circle" : "circle")
                    .foregroundStyle(selected ? theme.accentInk : theme.faint)
            }
            .padding(theme.spacing.l)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(selected ? theme.selected : hovering ? theme.hover : theme.surface2))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(selected ? theme.accent : theme.border, lineWidth: selected ? 1.5 : 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}
