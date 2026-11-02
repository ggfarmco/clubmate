import Foundation
import XCTest

import CompanionKit

@MainActor
final class VoiceTests: XCTestCase {
    func testPartialFromAudioResponse() async throws {
        let mac = VoiceMac()
        let voice = mac.session()
        voice.begin()
        try await waitUntil { mac.source.started && mac.ids.contains("voice.state") }
        mac.source.emit(Data(count: 640))
        try await waitUntil { voice.partial == "при" }
    }

    func testStateRefetchOnVoiceChanged() async throws {
        let mac = VoiceMac()
        mac.states = [#"{"utterance":"vu_1","phase":"listening","rev":1}"#, #"{"utterance":"vu_1","phase":"thinking","plan":"→ answer","rev":2}"#]
        let voice = mac.session()
        voice.begin()
        try await waitUntil { voice.state?.rev == 1 }
        voice.handle(.event(BusEvent(seq: 1, topic: "voice.changed", payload: .object(["utterance": .string("vu_1"), "rev": .number(1)]))), from: "mac1")
        voice.handle(.event(BusEvent(seq: 2, topic: "voice.changed", payload: .object(["utterance": .string("vu_1"), "rev": .number(2)]))), from: "mac1")
        try await waitUntil { voice.state?.rev == 2 }
        XCTAssertEqual(voice.state?.plan, "→ answer")
        XCTAssertEqual(mac.ids.filter { $0 == "voice.state" }.count, 2)
    }

    func testAudioFailureStopsCapture() async throws {
        let mac = VoiceMac()
        mac.failing = ["voice.audio", "voice.state"]
        let voice = mac.session()
        voice.begin()
        try await waitUntil { mac.source.started && mac.ids.contains("voice.start") }
        mac.source.emit(Data(count: 640))
        try await waitUntil { voice.phase == .error }
        mac.source.emit(Data(count: 640))
        voice.send(Data(count: 640))
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertTrue(mac.source.stopped)
        XCTAssertFalse(voice.active)
        XCTAssertEqual(voice.error, "The network connection was lost.")
        XCTAssertEqual(mac.ids.filter { $0 == "voice.audio" }.count, 1)
    }

    func testBackgroundEndsUtterance() async throws {
        let mac = VoiceMac()
        let voice = mac.session()
        voice.press(at: 0)
        try await waitUntil { mac.ids.contains("voice.state") }
        var held: [String] = []
        voice.background {
            held.append("begin")
            return { held.append("end after \(mac.ids.filter { $0 == "voice.end" }.count) voice.end") }
        }
        try await waitUntil { held.count == 2 }
        XCTAssertEqual(held, ["begin", "end after 1 voice.end"])
        XCTAssertTrue(mac.source.stopped)
        XCTAssertFalse(voice.active)
        voice.release(at: 5)
        try await Task.sleep(for: .milliseconds(30))
        XCTAssertEqual(mac.ids.filter { $0 == "voice.end" }.count, 1)
    }

    func testConfirmSendsShownId() async throws {
        let mac = VoiceMac()
        mac.states = [#"{"utterance":"vu_1","phase":"confirm","confirm":{"id":7,"title":"Remove worktree old?","command":"git.worktree-remove","args":{"name":"old"}},"rev":3}"#]
        let voice = mac.session()
        voice.begin()
        try await waitUntil { voice.phase == .confirm }
        voice.answer(true, at: 0.1)
        voice.answer(true, at: 1)
        try await waitUntil { mac.ids.contains("voice.confirm") }
        let confirm = try XCTUnwrap(mac.calls.first { $0.id == "voice.confirm" })
        XCTAssertEqual(confirm.args, .object(["utterance": .string("vu_1"), "approve": .bool(true), "confirm": .number(7)]))
        XCTAssertEqual(mac.ids.filter { $0 == "voice.confirm" }.count, 1)
    }

    func testUtteranceTargetsCurrentMac() async throws {
        let first = VoiceMac()
        let second = VoiceMac()
        let source = second.source
        let voice = VoiceSession(runner: { ["mac1": first, "mac2": second][$0] }, source: source, uptime: { 0 }, target: {
            VoiceTarget(computer: "mac2", workspace: "w2", pointer: VoicePointer(device: "phone", section: "agents"))
        })
        voice.begin()
        try await waitUntil { second.ids.contains("voice.state") }
        voice.handle(.event(BusEvent(seq: 1, topic: "voice.changed", payload: .object(["utterance": .string("vu_1"), "rev": .number(9)]))), from: "mac1")
        try await Task.sleep(for: .milliseconds(30))
        XCTAssertEqual(first.calls.count, 0)
        XCTAssertEqual(second.calls.map(\.workspace), ["w2", "w2"])
        XCTAssertEqual(second.ids, ["voice.start", "voice.state"])
    }

    func testSlowLinkBatchesChunks() async throws {
        let mac = VoiceMac()
        mac.holdAudio = true
        let voice = mac.session()
        voice.begin()
        try await waitUntil { mac.ids.contains("voice.state") }
        let chunk = Data(repeating: 1, count: VoiceAudio.chunkBytes)
        mac.source.emit(chunk)
        try await waitUntil { mac.ids.contains("voice.audio") }
        for _ in 0..<7 { mac.source.emit(chunk) }
        mac.releaseAudio()
        voice.finish()
        try await waitUntil { mac.ids.contains("voice.end") }
        let sent = mac.calls.filter { $0.id == "voice.audio" }.compactMap { $0.args?["pcm"]?.stringValue.flatMap { Data(base64Encoded: $0) }?.count }
        XCTAssertEqual(sent, [10_240, 65_536, 6_144])
    }

    func testPointerFromPhoneScreen() {
        XCTAssertEqual(VoiceTarget.phone(computer: "m", workspace: "w", plugin: "agents", detail: "agt_1").pointer, VoicePointer(device: "phone", section: "agents", selection: .init(session: "agt_1")))
        XCTAssertEqual(VoiceTarget.phone(computer: "m", workspace: "w", plugin: "backlog", detail: "12").pointer, VoicePointer(device: "phone", section: "backlog", selection: .init(task: 12)))
        XCTAssertEqual(VoiceTarget.phone(computer: "m", workspace: "w", plugin: "more", detail: "cron").pointer, VoicePointer(device: "phone", section: "cron"))
    }
}

@MainActor
private final class FakeVoiceSource: VoiceAudioSource {
    var started = false
    var stopped = false
    private var onChunk: (@MainActor (Data) -> Void)?

    func permission() async -> Bool { true }

    func start(onChunk: @escaping @MainActor (Data) -> Void, onEnd: @escaping @MainActor () -> Void) throws {
        started = true
        self.onChunk = onChunk
    }

    func stop() async {
        stopped = true
        onChunk = nil
    }

    func emit(_ pcm: Data) {
        onChunk?(pcm)
    }
}

@MainActor
private final class VoiceMac: CommandRunner {
    let source = FakeVoiceSource()
    var calls: [(id: String, workspace: String?, args: JSONValue?)] = []
    var states: [String] = [#"{"utterance":"vu_1","phase":"listening","rev":1}"#]
    var failing: Set<String> = []
    var holdAudio = false
    private var audioGate: CheckedContinuation<Void, Never>?

    var ids: [String] {
        calls.map(\.id)
    }

    func session() -> VoiceSession {
        VoiceSession(runner: { [unowned self] _ in self }, source: source, uptime: { 0 }, target: {
            VoiceTarget(computer: "mac1", workspace: "w", pointer: VoicePointer(device: "phone", section: "agents"))
        })
    }

    func releaseAudio() {
        holdAudio = false
        audioGate?.resume()
        audioGate = nil
    }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        calls.append((id, workspace, args))
        if failing.contains(id) { throw RemoteError.unreachable("The network connection was lost.") }
        switch id {
        case "voice.start":
            return .object(["utterance": .string("vu_1")])
        case "voice.audio":
            if holdAudio { await withCheckedContinuation { audioGate = $0 } }
            return .object(["partial": .string("при")])
        case "voice.end":
            return .object(["heard": .string("привет")])
        case "voice.state":
            let raw = states.count > 1 ? states.removeFirst() : states[0]
            return try JSONDecoder().decode(JSONValue.self, from: Data(raw.utf8))
        default:
            return .null
        }
    }
}
