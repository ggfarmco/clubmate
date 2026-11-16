import Foundation
import XCTest

import ClubmateKit

@MainActor
final class KeepAwakeTests: XCTestCase {
    private var calls: [CommandInvocation] = []
    private var reply: Result<JSONValue, ClubmateError> = .success(.null)

    private static let labels: JSONValue = .object([
        "section": .string("Keep Awake"),
        "hours": .object([
            "title": .string("For Hours"),
            "active": .string("For 4 Hours"),
            "options": .array([1, 4].map { .object(["hours": .number(Double($0)), "title": .string($0 == 1 ? "1 Hour" : "\($0) Hours")]) }),
        ]),
        "agents": .object(["title": .string("While Agents Work"), "subtitle": .string("Running or waiting for you")]),
        "on": .object(["title": .string("Until Turned Off")]),
        "off": .object(["title": .string("Turn Off")]),
    ])

    private func state(on: Bool = false, armed: Bool = false, text: String = "", mode: String = "", hours: Int = 0, holds: [JSONValue] = []) -> JSONValue {
        .object([
            "on": .bool(on),
            "armed": .bool(armed),
            "text": .string(text),
            "detail": .string(on ? "Kept awake by you until 18:00." : "The Mac sleeps as usual."),
            "mode": .string(mode),
            "hours": .number(Double(hours)),
            "until": .number(0),
            "value": .string("Off"),
            "holds": .array(holds),
            "labels": Self.labels,
            "laptop": .bool(true),
            "onBattery": .bool(false),
            "battery": .bool(false),
            "display": .bool(false),
        ])
    }

    private func makeModel() -> KeepAwakeModel {
        KeepAwakeModel { [unowned self] invocation in
            calls.append(invocation)
            return reply
        }
    }

    func testLoadDecodesState() async {
        let agent: JSONValue = .object(["kind": .string("agent"), "id": .string("ah_1"), "title": .string("release build"), "trailing": .string("Ship · ≤ 20:40"), "session": .string("agt_1"), "workspace": .string("prj_1")])
        reply = .success(state(on: true, text: "Awake until 18:00 +1", mode: "hours", hours: 4, holds: [agent]))
        let model = makeModel()

        await model.load()

        XCTAssertEqual(calls.map(\.id), ["awake.state"])
        let state = model.state
        XCTAssertEqual(state?.look, .awake)
        XCTAssertEqual(state?.mode, .hours)
        XCTAssertEqual(state?.hours, 4)
        XCTAssertEqual(state?.laptop, true)
        XCTAssertEqual(state?.holds, [KeepAwake.Hold(kind: "agent", title: "release build", trailing: "Ship · ≤ 20:40", id: "ah_1", session: "agt_1", workspace: "prj_1")])
        XCTAssertEqual(state?.labels.hours, [KeepAwake.HourOption(hours: 1, title: "1 Hour"), KeepAwake.HourOption(hours: 4, title: "4 Hours")])
        XCTAssertEqual(state?.labels.agentsSubtitle, "Running or waiting for you")
    }

    func testCommandsSendModeHoursAndToggle() async {
        let model = makeModel()
        reply = .success(state(on: true, text: "Awake", mode: "on"))

        await model.set(mode: .hours, hours: 2)
        await model.set(mode: .agents)
        await model.turnOff()
        await model.toggle()

        XCTAssertEqual(calls.map(\.id), ["awake.set", "awake.set", "awake.set", "awake.toggle"])
        XCTAssertEqual(calls.map(\.args), [
            .object(["mode": .string("hours"), "hours": .number(2)]),
            .object(["mode": .string("agents")]),
            .object(["mode": .string("off")]),
            .object([:]),
        ])
        XCTAssertEqual(model.state?.mode, .on)
    }

    func testFailedLoadClearsStateAndFailedSetKeepsIt() async {
        reply = .success(state(on: true, text: "Awake", mode: "on"))
        let model = makeModel()
        await model.load()

        reply = .failure(.protocolError("plugin down"))
        await model.turnOff()
        XCTAssertEqual(model.state?.on, true)
        XCTAssertNotNil(model.error)

        await model.load()
        XCTAssertNil(model.state)
    }

    func testOffMenuHasNoChecksNoTurnOffAndTheKeyOnUntilTurnedOff() throws {
        let off = try XCTUnwrap(KeepAwake(state()))
        let menu = KeepAwakeMenu(off)

        XCTAssertEqual(off.look, .off)
        XCTAssertEqual(menu.layout.map(\.title), ["The Mac sleeps as usual.", "Keep Awake", "For Hours", "While Agents Work", "Until Turned Off", "Keep Awake Settings…"])
        XCTAssertFalse(menu.items.contains { $0.checked })
        XCTAssertEqual(menu.items.filter(\.toggleKey).map(\.title), ["Until Turned Off"])
    }

    func testHoursMenuNamesTheNumberChecksItAndMovesTheKeyToTurnOff() throws {
        let you: JSONValue = .object(["kind": .string("you"), "title": .string("You"), "trailing": .string("for 4 hours · until 18:00")])
        let agent: JSONValue = .object(["kind": .string("agent"), "title": .string("release build"), "trailing": .string("≤ 20:40"), "session": .string("agt_1"), "workspace": .string("prj_1")])
        let menu = KeepAwakeMenu(try XCTUnwrap(KeepAwake(state(on: true, text: "Awake until 18:00 +1", mode: "hours", hours: 4, holds: [you, agent]))))

        XCTAssertEqual(menu.layout.map(\.title), ["Awake until 18:00 +1", "You\tfor 4 hours · until 18:00", "release build\t≤ 20:40", "Keep Awake", "For 4 Hours", "While Agents Work", "Until Turned Off", "Turn Off", "Keep Awake Settings…"])
        let hours = try XCTUnwrap(menu.items.first { $0.title == "For 4 Hours" })
        XCTAssertTrue(hours.checked)
        XCTAssertEqual(hours.children.filter(\.checked).map(\.action), [.set(.hours, hours: 4)])
        XCTAssertEqual(menu.items.filter(\.toggleKey).map(\.action), [.turnOff])
        XCTAssertEqual(menu.items.filter { $0.kind == .hold }.map(\.action), [nil, .openSession(session: "agt_1", workspace: "prj_1")])
    }

    func testArmedLooksMutedAndBarDropsTextBelow800() throws {
        let armed = try XCTUnwrap(KeepAwake(state(armed: true, mode: "agents")))
        let awake = try XCTUnwrap(KeepAwake(state(on: true, text: "Awake", mode: "on")))

        XCTAssertEqual(armed.look, .armed)
        XCTAssertEqual(armed.headline, "The Mac sleeps as usual.")
        XCTAssertTrue(awake.showsText(barWidth: 800))
        XCTAssertFalse(awake.showsText(barWidth: 799))
    }
}
