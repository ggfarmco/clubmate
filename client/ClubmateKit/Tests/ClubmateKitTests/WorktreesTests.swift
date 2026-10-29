import Foundation
import XCTest

import ClubmateKit

@MainActor
final class WorktreesTests: XCTestCase {
    func testSwitchCallsCommand() async throws {
        let core = FakeGitCore()
        let model = makeModel(core)
        await model.load()

        await model.switchTo("/r/.worktrees/feat")

        XCTAssertEqual(core.calls.map(\.id), ["git.worktrees", "git.worktree-switch", "git.worktrees"])
        XCTAssertEqual(core.calls[1].args, .object(["repo": .string("/r"), "path": .string("/r/.worktrees/feat")]))
    }

    func testRemoveConfirmsThenForcesOnUnsaved() async throws {
        let core = FakeGitCore(unsaved: true)
        var asked: [String] = []
        let model = makeModel(core) { message, detail in
            asked.append([message, detail].joined(separator: "|"))
            return true
        }
        await model.load()

        await model.remove("/r/.worktrees/feat")

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
        await cautious.remove("/r/.worktrees/feat")
        XCTAssertFalse(declined.calls.contains { $0.id == "git.worktree-remove" })
    }

    func testMissingBannerOnce() async throws {
        let core = FakeGitCore()
        var banners: [String] = []
        let project = try decodeProject(missing: ["/r/.worktrees/gone"])
        let model = WorktreeModel(run: core.run, project: { project }, confirm: { _, _ in false }, report: { banners.append($0) })

        await model.load()
        await model.load()

        XCTAssertEqual(banners, ["Worktree gone no longer exists — back on main"])
        XCTAssertEqual(core.calls.filter { $0.id == "git.worktree-switch" }.map(\.args), [
            .object(["repo": .string("/r"), "path": .string("/r")]),
        ])
    }

    private func makeModel(_ core: FakeGitCore, confirm: @escaping @MainActor (String, String) async -> Bool = { _, _ in false }) -> WorktreeModel {
        WorktreeModel(run: core.run, project: { nil }, confirm: confirm, report: { XCTFail("unexpected banner: \($0)") })
    }

    private func decodeProject(missing: [String]) throws -> Project {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let paths = missing.map { "\"\($0)\"" }.joined(separator: ",")
        return try decoder.decode(Project.self, from: Data(#"{"id":"p","name":"r","repos":["/r"],"missing":[\#(paths)],"createdAt":"2026-01-01T00:00:00Z"}"#.utf8))
    }
}

@MainActor
private final class FakeGitCore {
    private(set) var calls: [CommandInvocation] = []
    private let unsaved: Bool

    init(unsaved: Bool = false) {
        self.unsaved = unsaved
    }

    func run(_ invocation: CommandInvocation) async -> Result<JSONValue, ClubmateError> {
        calls.append(invocation)
        let args = invocation.args.objectValue ?? [:]
        switch invocation.id {
        case "git.worktrees":
            return .success(.array([
                Self.tree(path: "/r", branch: "main", main: true, current: true),
                Self.tree(path: "/r/.worktrees/feat", branch: "feat", main: false, current: false),
            ]))
        case "git.worktree-sessions":
            return .success(.array([
                .object(["id": .string("s1"), "title": .string("zsh")]),
                .object(["id": .string("s2"), "title": .string("server")]),
            ]))
        case "git.worktree-remove":
            return .success(.object(["id": .string(args["force"] == nil ? "op-1" : "op-2")]))
        case "git.worktree-op-status" where unsaved && args["id"] == .string("op-1"):
            return .success(.object(["done": .bool(true), "error": .string("plugin editor-nvim command.run: unsaved buffers: a.go")]))
        case "git.worktree-op-status":
            return .success(.object(["done": .bool(true)]))
        default:
            return .success(.null)
        }
    }

    private static func tree(path: String, branch: String, main: Bool, current: Bool) -> JSONValue {
        .object([
            "repo": .string("/r"), "path": .string(path), "branch": .string(branch),
            "head": .string("abc1234"), "main": .bool(main), "current": .bool(current),
        ])
    }
}
