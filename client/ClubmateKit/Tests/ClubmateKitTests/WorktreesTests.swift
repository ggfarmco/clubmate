import Foundation
import XCTest

import ClubmateKit

@MainActor
final class WorktreesTests: XCTestCase {
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

    func testLoadRetriesUntilTheGitPluginIsUp() async throws {
        let core = FakeGitCore()
        core.failures = 1
        let model = WorktreeModel(run: core.run, project: { nil }, confirm: { _, _ in false }, report: { XCTFail("unexpected banner: \($0)") }, retryDelays: [.milliseconds(1)])

        await model.load(retrying: true)

        XCTAssertEqual(core.calls.map(\.id), ["git.worktrees", "git.worktrees"])
        XCTAssertFalse(model.worktrees.isEmpty)
    }

    func testReviewLabelNamesTheDefaultBase() async throws {
        let core = FakeGitCore()
        let model = makeModel(core)
        await model.load()

        XCTAssertEqual(model.reviewLabel, "Review against origin/main")
    }

    func testSwitcherGroupsBranchesAfterWorktreesAndPickDispatches() async throws {
        let core = FakeGitCore()
        var banners: [String] = []
        let model = WorktreeModel(run: core.run, project: { nil }, confirm: { _, _ in false }, report: { banners.append($0) })
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
        XCTAssertEqual(banners, ["Warning: you are leaving 1 commit behind"])
    }

    func testMenuOpenedReloadsList() async throws {
        let core = FakeGitCore()
        let model = makeModel(core)
        await model.load()
        core.trees.append(FakeGitCore.tree(repo: "/r", path: "/r/.worktrees/new"))

        await model.menuOpened()

        XCTAssertEqual(model.worktrees.map(\.path), ["/r", "/r/.worktrees/feat", "/r/.worktrees/new"])
    }

    func testRemoveConfirmsThenForcesOnUnsaved() async throws {
        let core = FakeGitCore(unsaved: true)
        var asked: [String] = []
        let model = makeModel(core) { message, detail in
            asked.append([message, detail].joined(separator: "|"))
            return true
        }
        await model.load()

        await model.remove(repo: "/r", path: "/r/.worktrees/feat")

        XCTAssertEqual(asked, [
            "Remove worktree feat? Its branch is kept.|Terminals still in it: zsh, server",
            "Worktree feat has unsaved buffers: a.go. Remove it and discard the changes?|",
        ])
        XCTAssertEqual(core.calls.filter { $0.id == "git.worktree-remove" }.map(\.args), [
            .object(["repo": .string("/r"), "path": .string("/r/.worktrees/feat")]),
            .object(["repo": .string("/r"), "path": .string("/r/.worktrees/feat"), "force": .bool(true)]),
        ])

        let declined = FakeGitCore()
        let cautious = makeModel(declined) { _, _ in false }
        await cautious.load()
        await cautious.remove(repo: "/r", path: "/r/.worktrees/feat")
        XCTAssertFalse(declined.calls.contains { $0.id == "git.worktree-remove" })
    }

    func testMissingBannerOnceBackToFolderCheckout() async throws {
        let core = FakeGitCore()
        let folder = "/r/.worktrees/x"
        core.trees = [
            FakeGitCore.tree(repo: folder, path: "/r", main: true),
            FakeGitCore.tree(repo: folder, path: folder, current: true),
        ]
        var banners: [String] = []
        let project = try decodeProject(repo: folder, missing: ["/r/.worktrees/gone"])
        let model = WorktreeModel(run: core.run, project: { project }, confirm: { _, _ in false }, report: { banners.append($0) })

        await model.load()
        await model.load()

        XCTAssertEqual(banners, ["Worktree gone no longer exists — back on main"])
        XCTAssertEqual(core.calls.filter { $0.id == "git.worktree-switch" }.map(\.args), [
            .object(["repo": .string(folder), "path": .string(folder)]),
        ])
    }

    private func makeModel(_ core: FakeGitCore, confirm: @escaping @MainActor (String, String) async -> Bool = { _, _ in false }) -> WorktreeModel {
        WorktreeModel(run: core.run, project: { nil }, confirm: confirm, report: { XCTFail("unexpected banner: \($0)") })
    }

    private func decodeProject(repo: String, missing: [String]) throws -> Project {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let paths = missing.map { "\"\($0)\"" }.joined(separator: ",")
        return try decoder.decode(Project.self, from: Data(#"{"id":"p","name":"r","repos":["\#(repo)"],"missing":[\#(paths)],"createdAt":"2026-01-01T00:00:00Z"}"#.utf8))
    }
}

@MainActor
private final class FakeGitCore {
    private(set) var calls: [CommandInvocation] = []
    var trees = [tree(repo: "/r", path: "/r", main: true, current: true), tree(repo: "/r", path: "/r/.worktrees/feat")]
    var failures = 0
    private let unsaved: Bool

    init(unsaved: Bool = false) {
        self.unsaved = unsaved
    }

    func run(_ invocation: CommandInvocation) async -> Result<JSONValue, ClubmateError> {
        calls.append(invocation)
        let args = invocation.args.objectValue ?? [:]
        switch invocation.id {
        case "git.worktrees":
            if failures > 0 {
                failures -= 1
                return .failure(.rpc(code: -32000, message: "plugin not running"))
            }
            return .success(.array(trees))
        case "git.worktree-sessions":
            return .success(.array([
                .object(["id": .string("s1"), "title": .string("zsh")]),
                .object(["id": .string("s2"), "title": .string("server")]),
            ]))
        case "git.branches":
            return .success(.array([
                .object(["repo": .string("/r"), "name": .string("feature/x")]),
                .object(["repo": .string("/r"), "name": .string("origin/fetch"), "remote": .bool(true)]),
                .object(["repo": .string("/r"), "name": .string("old")]),
            ]))
        case "git.branch-switch":
            return .success(.object(["id": .string("op-3")]))
        case "git.worktree-remove":
            return .success(.object(["id": .string(args["force"] == nil ? "op-1" : "op-2")]))
        case "git.worktree-op-status" where unsaved && args["id"] == .string("op-1"):
            return .success(.object(["done": .bool(true), "error": .string("plugin editor-nvim command.run: unsaved buffers: a.go")]))
        case "git.worktree-op-status" where args["id"] == .string("op-3"):
            return .success(.object(["done": .bool(true), "notice": .string("Warning: you are leaving 1 commit behind")]))
        case "git.worktree-op-status":
            return .success(.object(["done": .bool(true)]))
        default:
            return .success(.null)
        }
    }

    static func tree(repo: String, path: String, main: Bool = false, current: Bool = false) -> JSONValue {
        .object([
            "repo": .string(repo), "path": .string(path), "branch": .string(main ? "main" : URL(fileURLWithPath: path).lastPathComponent),
            "head": .string("abc1234"), "main": .bool(main), "current": .bool(current), "defaultBase": .string("origin/main"),
        ])
    }
}
