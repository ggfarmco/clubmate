import XCTest

import ClubmateKit

@MainActor
final class WorktreePickerTests: XCTestCase {
    private func entry(_ repo: String, _ branch: String, now: Bool = false, stale: Bool = false, task: (session: String, title: String)? = nil, session: String = "") -> JSONValue {
        var fields: [String: JSONValue] = [
            "repo": .string(repo), "path": .string("\(repo)/\(branch)"), "branch": .string(branch),
            "now": .bool(now), "stale": .bool(stale), "detail": .string("d"),
        ]
        if let task {
            fields["task"] = .object(["session": .string(task.session), "title": .string(task.title), "state": .string("running")])
        }
        if !session.isEmpty { fields["session"] = .object(["id": .string("s"), "title": .string(session)]) }
        return .object(fields)
    }

    private func section(_ id: String, rows: [Int] = [], shown: Int? = nil, more: String = "", fold: String = "") -> JSONValue {
        .object([
            "id": .string(id), "title": .string(id), "rows": .array(rows.map { .number(Double($0)) }),
            "shown": .number(Double(shown ?? rows.count)), "more": .string(more), "fold": .string(fold),
        ])
    }

    private func folder(_ path: String, rows: [Int] = [], sections: [JSONValue] = []) -> JSONValue {
        .object(["path": .string(path), "rows": .array(rows.map { .number(Double($0)) }), "sections": .array(sections), "filter": .string("Filter \(path)")])
    }

    private func payload(folders: [JSONValue], entries: [JSONValue], tasks: [JSONValue] = [], sections: [JSONValue] = [], byFolder: Bool = false, stale: Int = 1) -> JSONValue {
        .object([
            "folders": .array(folders), "worktrees": .array(entries), "tasks": .array(tasks),
            "picker": .object(["byFolder": .bool(byFolder), "sections": .array(sections), "searchStale": .number(Double(stale)), "placeholder": .string("Switch")]),
        ])
    }

    private func model(_ value: JSONValue, branches: [Branch] = []) async -> WorktreePickerModel {
        let model = WorktreePickerModel(run: { _ in .success(value) }, branches: { branches })
        await model.load()
        model.reset()
        return model
    }

    private func ids(_ model: WorktreePickerModel) -> [String] {
        model.rows.map(\.id)
    }

    func testAFewWorktreesListByFolderAndOneFolderHasNoHeader() async {
        let two = await model(payload(folders: [folder("/a", rows: [0]), folder("/b", rows: [1])], entries: [entry("/a", "main", now: true), entry("/b", "main")], byFolder: true))
        XCTAssertEqual(ids(two), ["header:/a", "worktree:/a/main", "header:/b", "worktree:/b/main"])
        XCTAssertEqual(two.selectedRow?.id, "worktree:/a/main")

        let one = await model(payload(folders: [folder("/a", rows: [0])], entries: [entry("/a", "main", now: true)], byFolder: true))
        XCTAssertEqual(ids(one), ["worktree:/a/main"])
        XCTAssertNil(one.actionsTarget())
    }

    func testHomeThenFolderThenBack() async {
        let sections = [section("stale", rows: [1], shown: 0, fold: "show 1"), section("task", rows: [0], shown: 0, more: "1 more · type to find one")]
        let value = payload(folders: [folder("/a", sections: sections), folder("/b")], entries: [entry("/a", "x"), entry("/a", "old", stale: true)], sections: [section("folders")])
        let model = await model(value)
        XCTAssertEqual(ids(model), ["section:folders", "folder:/a", "folder:/b"])
        XCTAssertTrue(model.opensForward)

        _ = model.activate()
        XCTAssertEqual(ids(model), ["section:stale", "fold:stale", "section:task", "note:task"])
        XCTAssertEqual(model.placeholder, "Filter /a")
        XCTAssertEqual(model.caption, "2")

        _ = model.activate()
        XCTAssertEqual(ids(model), ["section:stale", "worktree:/a/old", "section:task", "note:task"])
        XCTAssertEqual(model.selectedRow?.id, "worktree:/a/old")

        XCTAssertTrue(model.back())
        XCTAssertFalse(model.inside)
        XCTAssertFalse(model.back())
    }

    func testOneFolderWithManyWorktreesOpensOnItsSections() async {
        let value = payload(folders: [folder("/a", sections: [section("live", rows: [0])])], entries: [entry("/a", "x")], sections: [section("folders")])
        let model = await model(value)
        XCTAssertEqual(ids(model), ["section:live", "worktree:/a/x"])
        XCTAssertFalse(model.back())
    }

    func testAFolderStaleRowCarriesItsCleanUpWord() async {
        var stale = section("stale", rows: [0], shown: 0, fold: "show 1").objectValue ?? [:]
        stale["clean"] = .string("Clean up 1…")
        let value = payload(folders: [folder("/a", sections: [.object(stale)])], entries: [entry("/a", "x", stale: true)], sections: [section("folders")])
        let model = await model(value)

        XCTAssertEqual(model.cleanWord(fold: "stale"), "Clean up 1…")
        XCTAssertEqual(model.cleanWord(fold: "live"), "")
    }

    func testSearchSpansFolderBranchTaskAndSessionWithATaskAsOneRow() async {
        let task = JSONValue.object(["session": .string("t"), "title": .string("Campaign pause"), "slug": .string("pause"), "worktrees": .array([.string("/a/feat"), .string("/b/feat")])])
        let entries = [
            entry("/a", "feat", task: (session: "t", title: "Campaign pause")),
            entry("/b", "feat", task: (session: "t", title: "Campaign pause")),
            entry("/pause-repo", "main"),
            entry("/c", "pause/ui"),
            entry("/c", "other", session: "Pause review"),
            entry("/c", "unrelated"),
        ]
        let model = await model(payload(folders: [folder("/a"), folder("/b")], entries: entries, tasks: [task]))
        model.update("pause")

        XCTAssertEqual(ids(model), [
            "section:tasks", "task:t", "section:worktrees",
            "worktree:/a/feat", "worktree:/b/feat", "worktree:/pause-repo/main", "worktree:/c/pause/ui", "worktree:/c/other",
        ])
        XCTAssertEqual(model.caption, "6 in 4 folders")
        XCTAssertEqual(model.selectedRow?.id, "task:t")

        _ = model.activate()
        XCTAssertEqual(ids(model), ["section:task:t", "worktree:/a/feat", "worktree:/b/feat"])
    }

    func testTaskWithOneWorktreePicksIt() async {
        let task = JSONValue.object(["session": .string("t"), "title": .string("Pause"), "worktrees": .array([.string("/a/feat")])])
        let model = await model(payload(folders: [folder("/a")], entries: [entry("/a", "feat", task: (session: "t", title: "Pause"))], tasks: [task]))
        model.update("pause")
        guard case .worktree(let picked) = model.activate() else { return XCTFail("expected the worktree") }
        XCTAssertEqual(picked.path, "/a/feat")
    }

    func testSearchPutsStaleLastCappedAndArrowDownShowsThem() async {
        let entries = [entry("/a", "feat-old", stale: true), entry("/a", "feat-older", stale: true), entry("/a", "feat-live"), entry("/a", "feat-last", stale: true)]
        let model = await model(payload(folders: [folder("/a"), folder("/b")], entries: entries))
        model.update("feat")
        XCTAssertEqual(ids(model), ["section:worktrees", "worktree:/a/feat-live", "worktree:/a/feat-old", "fold:search-stale"])

        model.move(1)
        model.move(1)

        XCTAssertEqual(ids(model), ["section:worktrees", "worktree:/a/feat-live", "worktree:/a/feat-old", "worktree:/a/feat-older", "worktree:/a/feat-last"])
        XCTAssertEqual(model.selectedRow?.id, "worktree:/a/feat-older")
    }

    func testSearchFallsBackToFuzzyAndListsBranchesLast() async {
        let branch = Branch(repo: "/a", name: "fix/cpr-branch", remote: false)
        let model = await model(payload(folders: [folder("/a")], entries: [entry("/a", "feat/campaign-pause-reason")]), branches: [branch])
        model.update("cpr")
        XCTAssertEqual(ids(model), ["section:worktrees", "worktree:/a/feat/campaign-pause-reason", "section:branches", branch.id])
    }

    func testAFolderScopeSearchesOnlyThatFolder() async {
        let sections = [section("live", rows: [0])]
        let value = payload(folders: [folder("/a", sections: sections), folder("/b")], entries: [entry("/a", "feat"), entry("/b", "feat")], sections: [section("folders")])
        let model = await model(value)
        _ = model.activate()
        model.update("feat")
        XCTAssertEqual(ids(model), ["section:worktrees", "worktree:/a/feat"])
        XCTAssertEqual(model.actionsTarget()?.path, "/a")
    }

    func testAFailedLoadOrAnOldPluginFallsBack() async {
        let down = WorktreePickerModel(run: { _ in .failure(.notRunning) })
        await down.load()
        XCTAssertTrue(down.unavailable)

        let old = WorktreePickerModel(run: { _ in .success(.object(["folders": .array([])])) })
        await old.load()
        XCTAssertTrue(old.unavailable)
    }

    func testAddReposComeFromThePlugin() async {
        var value = payload(folders: [folder("/kufar")], entries: [], byFolder: true)
        if case .object(var fields) = value, case .object(var picker) = fields["picker"] {
            picker["addRepos"] = .array([.object(["folder": .string("/kufar"), "title": .string("Add repos from kufar…")]), .object(["title": .string("no folder")])])
            fields["picker"] = .object(picker)
            value = .object(fields)
        }
        let model = await model(value)
        XCTAssertEqual(model.index?.addRepos.map(\.title), ["Add repos from kufar…"])
        XCTAssertEqual(model.index?.addRepos.first?.folder, "/kufar")
    }

    func testReloadKeepsTheSelection() async {
        var names = ["a", "b"]
        let build = { [self] in payload(folders: [folder("/r", rows: Array(names.indices)), folder("/s")], entries: names.map { entry("/r", $0) }, byFolder: true) }
        let model = WorktreePickerModel(run: { _ in .success(build()) })
        await model.load()
        model.reset()
        model.move(1)
        XCTAssertEqual(model.selectedRow?.id, "worktree:/r/b")

        names = ["z", "a", "b"]
        await model.load()

        XCTAssertEqual(model.selectedRow?.id, "worktree:/r/b")
    }
}
