import Foundation
import XCTest

import ClubmateKit

@MainActor
final class VoiceTests: XCTestCase {
    func testPartialFromAudioResponse() async throws {
        let core = FakeVoiceCore()
        let voice = core.controller()

        voice.begin()
        try await waitUntil { core.source.started }
        core.source.emit(Data(count: 640))

        try await waitUntil { voice.partial == "при" }
        let audio = try XCTUnwrap(core.calls.first { $0.id == "voice.audio" })
        XCTAssertEqual(audio.args["utterance"]?.stringValue, "vu_1")
        XCTAssertEqual(audio.args["pcm"]?.stringValue, Data(count: 640).base64EncodedString())
        let start = try XCTUnwrap(core.calls.first { $0.id == "voice.start" })
        XCTAssertEqual(start.args["pointer"]?["section"]?.stringValue, "agents")
        XCTAssertEqual(start.args["pointer"]?["selection"]?["session"]?.stringValue, "agt_x")
    }

    func testReleaseBeforeStartStillEnds() async throws {
        let core = FakeVoiceCore()
        core.holdStart = true
        let voice = core.controller()

        voice.begin()
        try await waitUntil { core.calls.contains { $0.id == "voice.start" } }
        voice.finish()
        core.releaseStart()

        try await waitUntil { core.calls.contains { $0.id == "voice.end" } }
        XCTAssertEqual(core.calls.first { $0.id == "voice.end" }?.args["utterance"]?.stringValue, "vu_1")
        XCTAssertTrue(core.source.stopped)
        XCTAssertFalse(voice.active)
    }

    func testAudioFailureStopsCapture() async throws {
        let core = FakeVoiceCore()
        core.failing = ["voice.audio", "voice.state"]
        let voice = core.controller()

        voice.begin()
        try await waitUntil { core.source.started }
        core.source.emit(Data(count: 640))
        core.source.emit(Data(count: 640))
        core.source.emit(Data(count: 640))

        try await waitUntil { voice.phase == .error }
        XCTAssertEqual(voice.error, "core is not running")
        XCTAssertFalse(voice.active)
        XCTAssertTrue(core.source.stopped)
        XCTAssertEqual(core.calls.filter { $0.id == "voice.audio" }.count, 1)
    }

    func testLoadingPhaseKeepsSending() async throws {
        let core = FakeVoiceCore()
        core.states = [#"{"utterance":"vu_1","phase":"loading","rev":1}"#]
        let voice = core.controller()

        voice.begin()
        try await waitUntil { voice.phase == .loading }
        XCTAssertEqual(voice.status, "Loading the speech model…")
        core.source.emit(Data(count: 640))

        try await waitUntil { core.calls.contains { $0.id == "voice.audio" } }
    }

    func testConfirmKeysOnlyWhileConfirming() async throws {
        let core = FakeVoiceCore()
        core.states = [
            #"{"utterance":"vu_1","phase":"thinking","heard":"удали ворктри","rev":2}"#,
            #"{"utterance":"vu_1","phase":"confirm","confirm":{"id":7,"title":"Удалить worktree old","command":"git.worktree-remove","args":{"name":"old"}},"rev":3}"#,
        ]
        let voice = core.controller()
        voice.begin()
        try await waitUntil { voice.phase == .thinking }

        XCTAssertFalse(voice.acceptsConfirmKeys)
        voice.confirm(true)
        voice.busEvent(BusEvent(seq: 1, topic: VoiceController.changedTopic, payload: .object(["utterance": .string("vu_1"), "rev": .number(3)])))
        try await waitUntil { voice.phase == .confirm }
        XCTAssertTrue(voice.acceptsConfirmKeys)
        XCTAssertEqual(voice.state?.confirm?.line, #"git.worktree-remove name="old""#)
        voice.confirm(true)

        try await waitUntil { core.calls.contains { $0.id == "voice.confirm" } }
        XCTAssertEqual(core.calls.filter { $0.id == "voice.confirm" }.map { $0.args["approve"] }, [.bool(true)])
        XCTAssertEqual(core.calls.first { $0.id == "voice.confirm" }?.args["confirm"], .number(7))
        XCTAssertFalse(voice.acceptsConfirmKeys)
    }

    func testReturnIgnoredRightAfterConfirmShows() async throws {
        let core = FakeVoiceCore()
        core.states = [
            #"{"utterance":"vu_1","phase":"confirm","confirm":{"title":"Отправить в терминал","command":"session.send","args":{"text":"ls\nrm -rf ~"}},"rev":2}"#,
            #"{"utterance":"vu_1","phase":"confirm","confirm":{"title":"Закрыть","command":"terminals.close","args":{"count":1e300,"a\nb":1}},"rev":3}"#,
        ]
        core.now = 100
        let voice = core.controller()
        voice.begin()
        try await waitUntil { voice.phase == .confirm }

        voice.confirmKey(true, at: 100.2)
        XCTAssertTrue(voice.acceptsConfirmKeys)
        XCTAssertEqual(voice.state?.confirm?.line, #"session.send text="ls\nrm -rf ~""#)
        core.now = 101
        voice.busEvent(BusEvent(seq: 1, topic: VoiceController.changedTopic, payload: .object(["utterance": .string("vu_1"), "rev": .number(3)])))
        try await waitUntil { voice.state?.rev == 3 }
        XCTAssertEqual(voice.state?.confirm?.line, #"terminals.close "a\nb"=1 count=1e+300"#)
        voice.confirmKey(true, at: 101.2)
        XCTAssertFalse(core.calls.contains { $0.id == "voice.confirm" })
        voice.confirmKey(true, at: 102)

        try await waitUntil { core.calls.contains { $0.id == "voice.confirm" } }
        XCTAssertEqual(core.calls.filter { $0.id == "voice.confirm" }.count, 1)
    }

    func testStateRefetchOnVoiceChanged() async throws {
        let core = FakeVoiceCore()
        core.states = [
            #"{"utterance":"vu_1","phase":"listening","rev":1}"#,
            #"{"utterance":"vu_1","phase":"done","heard":"второй вариант","plan":"→ ответ «Pick»: вариант 2","result":"отправлено","session":"agt_v","rev":5}"#,
        ]
        let voice = core.controller()
        voice.begin()
        try await waitUntil { voice.state?.phase == .listening }

        voice.busEvent(BusEvent(seq: 1, topic: VoiceController.changedTopic, payload: .object(["utterance": .string("vu_other"), "rev": .number(9)])))
        voice.busEvent(BusEvent(seq: 2, topic: VoiceController.changedTopic, payload: .object(["utterance": .string("vu_1"), "rev": .number(5)])))

        try await waitUntil { voice.phase == .done }
        XCTAssertEqual(voice.state?.plan, "→ ответ «Pick»: вариант 2")
        XCTAssertEqual(voice.state?.session, "agt_v")
        XCTAssertEqual(core.calls.filter { $0.id == "voice.state" }.count, 2)
        XCTAssertFalse(voice.active)
        XCTAssertTrue(core.source.stopped)
    }

    func testHoldTalksAndTapToggles() async throws {
        let core = FakeVoiceCore()
        let voice = core.controller()

        voice.press(at: 0)
        XCTAssertEqual(voice.sendHint, "Release to send")
        try await waitUntil { core.source.started }
        voice.release(at: 1)
        XCTAssertFalse(voice.active)
        try await waitUntil { core.calls.filter { $0.id == "voice.end" }.count == 1 }

        voice.press(at: 2)
        voice.release(at: 2.1)
        XCTAssertTrue(voice.active)
        XCTAssertEqual(voice.sendHint, "⌥Space to send")
        voice.press(at: 5)
        voice.release(at: 5.05)
        XCTAssertFalse(voice.active)
        try await waitUntil { core.calls.filter { $0.id == "voice.end" }.count == 2 }
    }

    func testMicrophoneDeniedNeverStarts() async throws {
        let core = FakeVoiceCore()
        core.source.allowed = false
        let voice = core.controller()

        voice.begin()

        try await waitUntil { voice.phase == .error }
        XCTAssertTrue(voice.micDenied)
        XCTAssertFalse(core.calls.contains { $0.id == "voice.start" })
    }
}

@MainActor
private final class FakeVoiceSource: VoiceAudioSource {
    var allowed = true
    var started = false
    var stopped = false
    private var onChunk: (@MainActor (Data) -> Void)?

    func permission() async -> Bool { allowed }

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
private final class FakeVoiceCore {
    let source = FakeVoiceSource()
    var calls: [CommandInvocation] = []
    var states: [String] = [#"{"utterance":"vu_1","phase":"listening","rev":1}"#]
    var failing: Set<String> = []
    var holdStart = false
    var now: TimeInterval = 0
    private var startGate: CheckedContinuation<Void, Never>?

    func controller() -> VoiceController {
        VoiceController(
            run: { [unowned self] invocation in await self.run(invocation) },
            source: source,
            uptime: { [unowned self] in self.now },
            pointer: { VoicePointer(section: "agents", selection: .init(session: "agt_x")) }
        )
    }

    func releaseStart() {
        holdStart = false
        startGate?.resume()
        startGate = nil
    }

    private func run(_ invocation: CommandInvocation) async -> Result<JSONValue, ClubmateError> {
        calls.append(invocation)
        if failing.contains(invocation.id) { return .failure(.notRunning) }
        switch invocation.id {
        case "voice.start":
            if holdStart { await withCheckedContinuation { startGate = $0 } }
            return .success(.object(["utterance": .string("vu_1")]))
        case "voice.audio":
            return .success(.object(["partial": .string("при")]))
        case "voice.end":
            return .success(.object(["heard": .string("привет")]))
        case "voice.state":
            let raw = states.count > 1 ? states.removeFirst() : states[0]
            return .success(try! JSONDecoder().decode(JSONValue.self, from: Data(raw.utf8)))
        default:
            return .success(.null)
        }
    }
}
