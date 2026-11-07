import Foundation
import XCTest

import ClubmateKit

@MainActor
final class WorktreesTests: XCTestCase {
    private var testCore: TestCore?

    override func tearDown() async throws {
        await testCore?.stop()
        testCore = nil
    }

    func testFollowReportsChangedWorkspace() async throws {
        let core = try await TestCore.start()
        testCore = core
        let client = try await CoreClient.connect(paths: core.paths)
        var changed: [String] = []
        let follower = Task { await WorktreeModel.follow(client) { changed.append($0.workspace) } }
        defer { follower.cancel() }

        try await waitUntil {
            try? core.cli("bus", "publish", WorktreeModel.changedTopic, #"{"workspace":"prj_a"}"#)
            return !changed.isEmpty
        }

        XCTAssertEqual(changed.first, "prj_a")
        await client.close()
    }

    func testFollowReportsCodeMovingToAnotherFolder() async throws {
        let core = try await TestCore.start()
        testCore = core
        let client = try await CoreClient.connect(paths: core.paths)
        var moved: [String] = []
        let follower = Task { await WorktreeModel.follow(client) { moved.append($0.topic) } }
        defer { follower.cancel() }

        try await waitUntil {
            try? core.cli("bus", "publish", "editor-nvim.root", #"{"workspace":"prj_a"}"#)
            return !moved.isEmpty
        }

        XCTAssertEqual(moved.first, "editor-nvim.root")
        await client.close()
    }

    func testSwitchCallsCommandForTheFolder() async throws {
        let core = FakeGitCore()
        core.trees = ["/r/a", "/r/b"].flatMap { repo in
            [FakeGitCore.tree(repo: repo, path: "/r", main: true, current: true), FakeGitCore.tree(repo: repo, path: "/r/.worktrees/feat")]
        }
        let model = makeModel(core)
        await model.load()

        await model.switchTo(repo: "/r/b", path: "/r/.worktrees/feat")

        XCTAssertEqual(core.calls.map(\.id), ["git.worktrees", "git.worktree-switch", "git.worktrees"])
        XCTAssertEqual(core.calls[1].args, .object(["repo": .string("/r/b"), "path": .string("/r/.worktrees/feat")]))
    }

    func testPickingACurrentWorktreeOfAnotherFolderMovesCode() async throws {
        let core = FakeGitCore()
        core.trees = ["/a", "/b"].map { FakeGitCore.tree(repo: $0, path: $0, main: true, current: true) }
        core.codeFolder = "/a"
        let model = makeModel(core)
        await model.load()
        XCTAssertEqual(model.current?.repo, "/a")
        XCTAssertEqual(model.currentFolder, "a")

        await model.open(repo: "/b", path: "/b")

        XCTAssertEqual(core.moves, ["/b"])
        XCTAssertFalse(core.calls.map(\.id).contains("git.worktree-switch"))
        XCTAssertEqual(model.currentFolder, "b")
    }

    func testLoadRetriesUntilTheGitPluginIsUp() async throws {
        let core = FakeGitCore()
        core.failures = 1
        let model = WorktreeModel(run: core.run, confirm: { _, _ in false }, report: { XCTFail("unexpected banner: \($0)") }, retryDelays: [.milliseconds(1)])

        await model.load(retrying: true)

        XCTAssertEqual(core.calls.map(\.id), ["git.worktrees", "git.worktrees"])
        XCTAssertFalse(model.worktrees.isEmpty)
    }

    func testSwitcherGroupsBranchesAfterWorktreesAndPickDispatches() async throws {
        let core = FakeGitCore()
        let model = makeModel(core)
        await model.menuOpened()

        let picker = FuzzyPickerModel(placeholder: "", debounce: .zero, source: model.switcherSource)
        picker.update("fe")
        _ = await picker.choose()
        XCTAssertEqual(picker.items.map(\.label), ["feat", "feature/x", "origin/fetch"])
        XCTAssertEqual((0..<3).map { picker.header(at: $0) }, ["Worktrees", "Branches", nil])

        await model.pick(picker.items[2])
        await model.pick(picker.items[0])

        XCTAssertEqual(core.calls.filter { $0.id == "git.branch-switch" }.map(\.args), [
            .object(["repo": .string("/r"), "branch": .string("origin/fetch")]),
        ])
        XCTAssertEqual(core.calls.filter { $0.id == "git.worktree-switch" }.map(\.args), [
            .object(["repo": .string("/r"), "path": .string("/r/.worktrees/feat")]),
        ])
    }

    func testDetachedWorktreeReadsDetachedWithItsShortSha() async throws {
        let core = FakeGitCore()
        core.trees.append(.object(["repo": .string("/r"), "path": .string("/r/.worktrees/x"), "branch": .string(""), "head": .string("03a9ff5f")]))
        let model = makeModel(core)
        await model.load()

        let items = model.switcherItems("")
        XCTAssertEqual(items.map(\.label), ["main", "feat", "detached · 03a9ff5f"])
        XCTAssertEqual(items.map(\.faint), [false, false, true])
    }

    func testMenuOpenedReloadsList() async throws {
        let core = FakeGitCore()
        let model = makeModel(core)
        await model.load()
        core.trees.append(FakeGitCore.tree(repo: "/r", path: "/r/.worktrees/new"))

        await model.menuOpened()

        XCTAssertEqual(model.worktrees.map(\.path), ["/r", "/r/.worktrees/feat", "/r/.worktrees/new"])
    }

    func testRemoveAsksThePluginsPromptThenRunsItsCommand() async throws {
        let core = FakeGitCore()
        var asked: [String] = []
        let model = makeModel(core) { message, detail in
            asked.append([message, detail].joined(separator: "|"))
            return true
        }

        await model.remove(repo: "/r", path: "/r/.worktrees/feat")

        XCTAssertEqual(asked, ["Remove worktree feat? Its branch is kept.|Terminals still in it: zsh"])
        XCTAssertEqual(core.calls.filter { $0.id == "git.worktree-remove" }.map(\.args), [
            .object(["repo": .string("/r"), "path": .string("/r/.worktrees/feat")]),
        ])

        let declined = FakeGitCore()
        await makeModel(declined).remove(repo: "/r", path: "/r/.worktrees/feat")
        XCTAssertFalse(declined.calls.contains { $0.id == "git.worktree-remove" })
    }

    func testOutcomeReportsMessageAsksConfirmAndReloads() async throws {
        let core = FakeGitCore()
        var banners: [String] = []
        var asked: [String] = []
        let model = WorktreeModel(run: core.run, confirm: { message, _ in
            asked.append(message)
            return true
        }, report: { banners.append($0) })
        let force: JSONValue = .object(["repo": .string("/r"), "path": .string("/r/.worktrees/feat"), "force": .bool(true)])

        await model.receive(BusEvent(seq: 1, topic: WorktreeModel.outcomeTopic, source: "plugin:git", payload: .object(["workspace": .string("p"), "message": .string("Worktree gone no longer exists — back on main")])))
        await model.receive(BusEvent(seq: 2, topic: WorktreeModel.outcomeTopic, source: "cli", payload: .object(["workspace": .string("p"), "message": .string("forged")])))
        await model.receive(BusEvent(seq: 3, topic: WorktreeModel.outcomeTopic, source: "plugin:git", payload: .object([
            "workspace": .string("p"),
            "confirm": .object(["title": .string("Kill?"), "command": .string("terminals.kill"), "args": .object([:])]),
        ])))
        await model.receive(BusEvent(seq: 4, topic: WorktreeModel.outcomeTopic, source: "plugin:git", payload: .object([
            "workspace": .string("p"),
            "confirm": .object(["title": .string("Discard?"), "command": .string("git.worktree-remove"), "args": force]),
        ])))

        XCTAssertEqual(banners, ["Worktree gone no longer exists — back on main"])
        XCTAssertEqual(asked, ["Discard?"])
        XCTAssertEqual(core.calls.map(\.id), ["git.worktrees", "git.worktrees", "git.worktrees", "git.worktree-remove", "git.worktrees"])
        XCTAssertEqual(core.calls[3].args, force)
        XCTAssertFalse(model.worktrees.isEmpty)
    }

    private func makeModel(_ core: FakeGitCore, confirm: @escaping @MainActor (String, String) async -> Bool = { _, _ in false }) -> WorktreeModel {
        WorktreeModel(run: core.run, confirm: confirm, report: { XCTFail("unexpected banner: \($0)") })
    }
}

@MainActor
private final class FakeGitCore {
    private(set) var calls: [CommandInvocation] = []
    var trees = [tree(repo: "/r", path: "/r", main: true, current: true), tree(repo: "/r", path: "/r/.worktrees/feat")]
    var failures = 0
    var codeFolder = ""
    private(set) var moves: [String] = []

    func run(_ invocation: CommandInvocation) async -> Result<JSONValue, ClubmateError> {
        if invocation.id == WorktreeModel.codeRootCommand {
            let folder = invocation.args.objectValue?["folder"]?.stringValue
            if folder != nil { moves.append(folder ?? "") }
            codeFolder = folder ?? codeFolder
            return .success(.object(["folder": .string(codeFolder)]))
        }
        calls.append(invocation)
        let args = invocation.args.objectValue ?? [:]
        switch invocation.id {
        case "git.worktrees":
            if failures > 0 {
                failures -= 1
                return .failure(.rpc(code: -32000, message: "plugin not running"))
            }
            return .success(.array(trees))
        case "git.worktree-remove-prompt":
            return .success(.object([
                "title": .string("Remove worktree feat? Its branch is kept."), "detail": .string("Terminals still in it: zsh"),
                "command": .string("git.worktree-remove"), "args": .object(["repo": args["repo"] ?? .null, "path": args["path"] ?? .null]),
            ]))
        case "git.branches":
            return .success(.array([
                .object(["repo": .string("/r"), "name": .string("feature/x")]),
                .object(["repo": .string("/r"), "name": .string("origin/fetch"), "remote": .bool(true)]),
                .object(["repo": .string("/r"), "name": .string("old")]),
            ]))
        default:
            return .success(.null)
        }
    }

    static func tree(repo: String, path: String, main: Bool = false, current: Bool = false) -> JSONValue {
        .object([
            "repo": .string(repo), "path": .string(path), "branch": .string(main ? "main" : URL(fileURLWithPath: path).lastPathComponent),
            "head": .string("abc1234"), "main": .bool(main), "current": .bool(current),
        ])
    }
}
