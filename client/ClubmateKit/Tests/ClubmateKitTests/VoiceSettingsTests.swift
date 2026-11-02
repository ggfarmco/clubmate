import Foundation
import XCTest

import ClubmateKit

@MainActor
final class VoiceSettingsTests: XCTestCase {
    private var calls: [CommandInvocation] = []
    private var reply: JSONValue = .null

    private func settings(enabled: Bool, model: String) -> JSONValue {
        .object([
            "enabled": .bool(enabled),
            "model": .string(model),
            "models": .array([
                .object(["id": .string("nemotron"), "title": .string("Nemotron 0.6B"), "detail": .string("about 2.5 GB")]),
                .object(["id": .string("whisper-small"), "title": .string("Whisper small"), "detail": .string("Least memory · about 0.9 GB")]),
            ]),
        ])
    }

    private func makeModel() -> VoiceSettingsModel {
        VoiceSettingsModel { [unowned self] invocation in
            calls.append(invocation)
            return .success(reply)
        }
    }

    func testLoadDecodesSettings() async {
        reply = settings(enabled: false, model: "whisper-small")
        let model = makeModel()

        await model.load()

        XCTAssertEqual(calls.map(\.id), ["voice.settings"])
        XCTAssertEqual(model.enabled, false)
        XCTAssertEqual(model.models.map(\.id), ["nemotron", "whisper-small"])
        XCTAssertEqual(model.chosen?.detail, "Least memory · about 0.9 GB")
    }

    func testSettersSendOnlyTheChangedField() async {
        reply = settings(enabled: false, model: "nemotron")
        let model = makeModel()
        await model.load()

        reply = settings(enabled: true, model: "nemotron")
        await model.setEnabled(true)
        reply = settings(enabled: true, model: "whisper-small")
        await model.setModel("whisper-small")
        await model.setModel("whisper-small")

        XCTAssertEqual(calls.dropFirst().map(\.id), ["voice.settings-set", "voice.settings-set"])
        XCTAssertEqual(calls.dropFirst().map(\.args), [.object(["enabled": .bool(true)]), .object(["model": .string("whisper-small")])])
        XCTAssertTrue(model.isOn)
        XCTAssertEqual(model.model, "whisper-small")
    }

    func testBusPayloadUpdatesWithoutACall() {
        let model = makeModel()

        model.receive(settings(enabled: true, model: "nemotron"))

        XCTAssertTrue(model.isOn)
        XCTAssertTrue(calls.isEmpty)
    }

    func testFailureKeepsStateAndShowsError() async {
        reply = settings(enabled: false, model: "nemotron")
        let model = VoiceSettingsModel { invocation in
            invocation.id == "voice.settings" ? .success(self.reply) : .failure(.protocolError("unknown voice model"))
        }
        await model.load()

        await model.setModel("bogus")

        XCTAssertEqual(model.model, "nemotron")
        XCTAssertNotNil(model.error)
    }
}
