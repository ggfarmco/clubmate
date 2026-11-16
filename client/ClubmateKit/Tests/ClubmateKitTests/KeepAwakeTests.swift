import Foundation
import XCTest

import ClubmateKit

@MainActor
final class KeepAwakeTests: XCTestCase {
    private var calls: [CommandInvocation] = []
    private var reply: Result<JSONValue, ClubmateError> = .success(.null)

    private func state(on: Bool, text: String = "", manual: Bool = false) -> JSONValue {
        .object([
            "on": .bool(on),
            "text": .string(text),
            "detail": .string(on ? "Kept awake while 2 agents work, on power only." : "The Mac sleeps as usual."),
            "manual": .bool(manual),
            "until": .number(0),
            "holds": .array([]),
        ])
    }

    private func makeModel() -> KeepAwakeModel {
        KeepAwakeModel { [unowned self] invocation in
            calls.append(invocation)
            return reply
        }
    }

    func testLoadDecodesState() async {
        reply = .success(state(on: true, text: "Awake · 2 agents"))
        let model = makeModel()

        await model.load()

        XCTAssertEqual(calls.map(\.id), ["awake.state"])
        XCTAssertEqual(model.state, KeepAwake(on: true, text: "Awake · 2 agents", detail: "Kept awake while 2 agents work, on power only.", manual: false))
    }

    func testSetSendsOnAndMinutesAndAppliesTheReply() async {
        let model = makeModel()

        reply = .success(state(on: true, text: "Awake until 18:30", manual: true))
        await model.keepAwake(minutes: 60)
        await model.keepAwake(minutes: nil)
        reply = .success(state(on: false))
        await model.turnOff()

        XCTAssertEqual(calls.map(\.id), ["awake.set", "awake.set", "awake.set"])
        XCTAssertEqual(calls.map(\.args), [
            .object(["on": .bool(true), "minutes": .number(60)]),
            .object(["on": .bool(true)]),
            .object(["on": .bool(false)]),
        ])
        XCTAssertEqual(model.state?.on, false)
    }

    func testBusPayloadUpdatesWithoutACall() {
        let model = makeModel()

        model.receive(state(on: true, text: "Awake", manual: true))

        XCTAssertEqual(model.state?.manual, true)
        XCTAssertTrue(calls.isEmpty)
    }

    func testFailedLoadClearsStateAndFailedSetKeepsIt() async {
        reply = .success(state(on: true, text: "Awake"))
        let model = makeModel()
        await model.load()

        reply = .failure(.protocolError("plugin down"))
        await model.turnOff()
        XCTAssertEqual(model.state?.on, true)
        XCTAssertNotNil(model.error)

        await model.load()
        XCTAssertNil(model.state)
    }
}
