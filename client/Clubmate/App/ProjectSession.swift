import AppKit
import Foundation
import Observation

import ClubmateKit

enum WorkspaceOverlay: Equatable {
    case palette
    case pickFile
    case picker
}

struct PickerRequest {
    let model: FuzzyPickerModel
    let choose: @MainActor (PickerItem) -> Void
}

@MainActor
@Observable
final class ProjectSession {
    let projectID: String
    let deck = SurfaceDeck()
    let banner = EventBanner()
    var overlay: WorkspaceOverlay?
    private(set) var picker: PickerRequest?
    private(set) var filePicker: FilePickerModel?
    var thumbnail: NSImage?
    var autoNewRequest = 0
    @ObservationIgnored var autoNew = AutoNewGate()
    private(set) var store: ProjectUIStore?
    @ObservationIgnored private let lookup: @MainActor () -> Project?
    @ObservationIgnored private(set) lazy var worktrees = WorktreeModel(
        run: storeRun,
        project: { [weak self] in self?.lookup() },
        confirm: { message, detail in await Confirmation.ask(message, detail: detail, action: "Remove", destructive: true) },
        report: { [weak self] message in self?.banner.show(message) }
    )

    init(projectID: String, project: @escaping @MainActor () -> Project?) {
        self.projectID = projectID
        lookup = project
    }

    func newWorktree(chooseFolder: Bool) {
        let groups = worktrees.groups
        Task {
            var repo: String?
            if chooseFolder, groups.count > 1 {
                guard let index = await ChoicePrompt.ask("New Worktree in", options: groups.map(\.name), action: "Continue") else { return }
                repo = groups[index].repo
            }
            guard let branch = await TextPrompt.ask("New Worktree", text: "", action: "Create") else { return }
            await worktrees.create(branch, repo: repo)
        }
    }

    private var storeRun: WorktreeModel.Run {
        { [weak self] invocation in
            guard let store = self?.store else { return .failure(.notRunning) }
            return await store.run(invocation)
        }
    }

    func openPicker(_ placeholder: String, source: @escaping FuzzyPickerModel.Source, choose: @escaping @MainActor (PickerItem) -> Void) {
        picker?.model.cancel()
        picker = PickerRequest(model: FuzzyPickerModel(placeholder: placeholder, source: source), choose: choose)
        overlay = .picker
    }

    func chooseWorktree() {
        Task { [weak self] in
            guard let worktrees = self?.worktrees else { return }
            await worktrees.load()
            self?.openPicker("Switch to worktree or branch", source: worktrees.switcherSource) { [weak self] item in
                self?.pickWorktree(item)
            }
            guard let model = self?.picker?.model else { return }
            await worktrees.loadBranches()
            model.update(model.query)
        }
    }

    func chooseWorktreeToRemove() {
        Task {
            await worktrees.menuOpened()
            let trees = worktrees.linked
            guard !trees.isEmpty, let index = await ChoicePrompt.ask("Remove Worktree", options: trees.map(worktrees.label), action: "Continue") else { return }
            await worktrees.remove(repo: trees[index].repo, path: trees[index].path)
        }
    }

    func removeWorktree(_ tree: Worktree) {
        Task { await worktrees.remove(repo: tree.repo, path: tree.path) }
    }

    func pickWorktree(_ item: PickerItem) {
        Task { await worktrees.pick(item) }
    }

    func worktreeMenuOpened() {
        Task { await worktrees.menuOpened() }
    }

    func pickFile(_ target: @escaping @MainActor (String) -> Void) {
        filePicker?.cancel()
        filePicker = FilePickerModel(run: storeRun, dismiss: { [weak self] in self?.close(.pickFile) }, pick: target)
        overlay = .pickFile
    }

    func toggle(_ overlay: WorkspaceOverlay) {
        self.overlay = self.overlay == overlay ? nil : overlay
    }

    func close(_ overlay: WorkspaceOverlay) {
        guard self.overlay == overlay else { return }
        self.overlay = nil
        if overlay == .pickFile {
            filePicker?.cancel()
            filePicker = nil
        }
        if overlay == .picker {
            picker?.model.cancel()
            picker = nil
        }
    }

    func adopt(_ client: CoreClient, _ supervisor: ConnectionSupervisor, _ settings: AppSettings) {
        guard store == nil else { return }
        let store = ProjectUIStore(client: client, projectID: projectID) { [weak settings] in settings?.ordered($0) ?? $0 }
        self.store = store
        supervisor.register(store)
    }

    func run(_ invocation: CommandInvocation?) {
        guard let invocation, let store else { return }
        Task {
            if case .failure(let error) = await store.run(invocation) {
                banner.show(error.message)
            }
        }
    }

    func runConfirmed(_ id: String, commands: [Command]) {
        let message = commands.first { $0.id == id }?.confirm ?? ""
        guard !message.isEmpty else { return run(command: id) }
        Task {
            if await Confirmation.ask(message, action: "Run") { run(command: id) }
        }
    }

    private func run(command id: String) {
        guard id == NvimRestart.command, let store else { return run(CommandInvocation(id: id)) }
        let name = lookup()?.name ?? projectID
        let banner = banner
        Task {
            let outcome = await NvimRestart.run(workspace: name, run: { await store.run($0) }) { message in
                await Confirmation.ask(message, action: "Restart", destructive: true)
            }
            if case .failed(let message) = outcome { banner.show(message) }
        }
    }

    func closeTerminal(_ close: CommandInvocation) {
        guard let store else { return }
        Task {
            switch await store.run(CommandInvocation(id: TerminalsSection.busyCommand, args: close.args)) {
            case .success(let result):
                if let message = TerminalsSection.busyMessage(result), !(await Confirmation.ask(message, action: "Close")) { return }
                run(close)
            case .failure(let error):
                banner.show(error.message)
            }
        }
    }

    func close(_ supervisor: ConnectionSupervisor) async {
        deck.closeAll()
        guard let store else { return }
        supervisor.unregister(store)
        await store.stop()
    }
}
