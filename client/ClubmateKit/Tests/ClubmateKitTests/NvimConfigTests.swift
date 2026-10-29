import XCTest

import ClubmateKit

@MainActor
final class NvimConfigTests: XCTestCase {
    func testChooseClubmateCopies() async throws {
        let core = FakeNvimCore(statuses: [
            .object(["copied": .number(5), "total": .number(10), "done": .bool(false)]),
            .object(["copied": .number(10), "total": .number(10), "done": .bool(true)]),
        ])
        let model = makeModel(core)

        await model.load()
        await model.choose(.clubmate)
        XCTAssertEqual(model.sheet, .offer)
        await model.copy()

        XCTAssertEqual(core.mutations, [
            Call(workspace: nil, CommandInvocation(id: "editor-nvim.config-copy", args: .object(["data": .bool(true)]))),
            Call(workspace: nil, CommandInvocation(id: "editor-nvim.config", args: .object(["mode": .string("clubmate")]))),
            Call(workspace: "w1", CommandInvocation(id: "editor-nvim.restart", args: .object([:]))),
        ])
        XCTAssertEqual(core.polls, 2)
        XCTAssertEqual(model.progress, NvimCopyProgress(copied: 10, total: 10))
        XCTAssertEqual(model.info?.mode, .clubmate)
        XCTAssertNil(model.sheet)
        XCTAssertFalse(model.needsRestart)
    }

    func testCancelKeepsShared() async throws {
        let core = FakeNvimCore(statuses: [])
        let model = makeModel(core)

        await model.load()
        await model.choose(.clubmate)
        model.dismiss()

        XCTAssertEqual(core.mutations, [])
        XCTAssertEqual(model.info?.mode, .shared)
        XCTAssertNil(model.sheet)
    }

    func testCopyKeepsExistingData() async throws {
        let core = FakeNvimCore(statuses: [], dataExists: true)
        let model = makeModel(core)

        await model.load()
        await model.choose(.clubmate)
        await model.copy()

        XCTAssertEqual(core.mutations.first, Call(workspace: nil, CommandInvocation(id: "editor-nvim.config-copy", args: .object(["data": .bool(false)]))))
    }

    func testRestartConfirmsUnsavedBuffers() async throws {
        let core = FakeNvimCore(statuses: [], configExists: true, unsaved: true)
        var asked: [String] = []
        let model = makeModel(core) { asked.append($0); return true }

        await model.load()
        await model.choose(.clubmate)

        XCTAssertEqual(asked, ["Neovim in “one” has unsaved buffers: a.go. Restart and discard the changes?"])
        XCTAssertEqual(core.mutations.last, Call(workspace: "w1", CommandInvocation(id: "editor-nvim.restart", args: .object(["force": .bool(true)]))))
        XCTAssertFalse(model.needsRestart)
    }

    private func makeModel(_ core: FakeNvimCore, confirm: @escaping @MainActor (String) async -> Bool = { _ in false }) -> NvimConfigModel {
        NvimConfigModel(run: core.run, workspaces: { [NvimWorkspace(id: "w1", name: "one")] }, confirm: confirm)
    }
}

private struct Call: Equatable {
    let workspace: String?
    let invocation: CommandInvocation

    init(workspace: String?, _ invocation: CommandInvocation) {
        self.workspace = workspace
        self.invocation = invocation
    }
}

@MainActor
private final class FakeNvimCore {
    private var statuses: [JSONValue]
    private(set) var mutations: [Call] = []
    private(set) var polls = 0
    private var mode = "shared"
    private let configExists: Bool
    private let dataExists: Bool
    private let unsaved: Bool

    init(statuses: [JSONValue], configExists: Bool = false, dataExists: Bool = false, unsaved: Bool = false) {
        self.statuses = statuses
        self.configExists = configExists
        self.dataExists = dataExists
        self.unsaved = unsaved
    }

    func run(_ workspace: String?, _ invocation: CommandInvocation) async -> Result<JSONValue, ClubmateError> {
        switch invocation.id {
        case "editor-nvim.config" where invocation.args.objectValue?["mode"] == nil:
            return .success(.object([
                "mode": .string(mode), "app": .string("clubmate-dev"),
                "configDir": .string("/h/.config/clubmate-dev"), "dataDir": .string("/h/.local/share/clubmate-dev"),
                "configExists": .bool(configExists), "dataExists": .bool(dataExists),
                "sourceConfigBytes": .number(10), "sourceDataBytes": .number(-1),
            ]))
        case "editor-nvim.config-copy-status":
            polls += 1
            return .success(statuses.isEmpty ? .object(["done": .bool(true)]) : statuses.removeFirst())
        case "editor-nvim.config-copy":
            mutations.append(Call(workspace: workspace, invocation))
            return .success(.object(["id": .string("copy-1")]))
        case "editor-nvim.restart" where unsaved && invocation.args.objectValue?["force"] == nil:
            return .failure(.rpc(code: -32013, message: "plugin editor-nvim command.run: unsaved buffers: a.go"))
        default:
            mutations.append(Call(workspace: workspace, invocation))
            if let stored = invocation.args.objectValue?["mode"]?.stringValue { mode = stored }
            return .success(.null)
        }
    }
}
