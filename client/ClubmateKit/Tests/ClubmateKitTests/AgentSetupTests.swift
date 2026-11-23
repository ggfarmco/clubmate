import Foundation
import XCTest

import ClubmateKit

@MainActor
final class AgentSetupTests: XCTestCase {
    func testFixFailureShowsUntilTheStateChanges() async {
        var reply: Result<JSONValue, ClubmateError> = .failure(.rpc(code: -32000, message: "plugin agents command.run: no Home workspace to run the fix in"))
        let model = AgentSetupModel { _ in reply }
        model.receive(.object(["state": .string("git_missing"), "title": .string("git is needed")]))

        await model.fix()
        XCTAssertEqual(model.fixError, "no Home workspace to run the fix in")

        model.receive(.object(["state": .string("ok")]))
        XCTAssertNil(model.fixError)
        XCTAssertEqual(model.state?.shown, false)

        reply = .success(.null)
        await model.fix()
        XCTAssertNil(model.fixError)
    }
}
