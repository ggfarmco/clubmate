import XCTest

import ClubmateKit

@MainActor
final class NvimRestartTests: XCTestCase {
    func testRestartForcesAfterConfirm() async throws {
        var calls: [CommandInvocation] = []
        var asked: [String] = []

        let outcome = await NvimRestart.run(workspace: "one", run: { invocation in
            calls.append(invocation)
            if invocation.args.objectValue?["force"] == nil {
                return .failure(.rpc(code: -32013, message: "plugin editor-nvim command.run: unsaved buffers: unknown (nvim not responding)"))
            }
            return .success(.null)
        }, confirm: { asked.append($0); return true })

        XCTAssertEqual(outcome, .restarted)
        XCTAssertEqual(asked, ["Neovim in “one” has unsaved buffers: unknown (nvim not responding). Restart and discard the changes?"])
        XCTAssertEqual(calls.last, CommandInvocation(id: "editor-nvim.restart", args: .object(["force": .bool(true)])))
    }
}
