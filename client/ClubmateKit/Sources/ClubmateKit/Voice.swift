import Foundation
import Observation

@MainActor
public protocol VoiceAudioSource: AnyObject {
    var permissionPrompts: Bool { get }
    func permission() async -> Bool
    func start(onChunk: @escaping @MainActor (Data) -> Void, onEnd: @escaping @MainActor () -> Void) throws
    func stop() async
}

@MainActor
@Observable
public final class VoiceController {
    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public static let changedTopic = "voice.changed"
    public static let micDeniedMessage = "Microphone access is off — System Settings → Privacy → Microphone"
    public static let tapLimit: TimeInterval = 0.6
    public static let sendGrace: TimeInterval = 1
    public static let confirmArmDelay: TimeInterval = 0.6
    static let meterBars = 16

    public private(set) var state: VoiceState?
    public private(set) var partial = ""
    public private(set) var active = false
    public private(set) var visible = false
    public private(set) var micDenied = false
    public private(set) var holding = false
    public private(set) var target: String?
    public private(set) var levels: [Double] = Array(repeating: 0, count: VoiceController.meterBars)
    private var failure: String?
    private var confirming = false

    @ObservationIgnored private let run: Run
    @ObservationIgnored private let source: VoiceAudioSource
    @ObservationIgnored private let pointer: @MainActor () -> VoicePointer
    @ObservationIgnored private let focusedComposer: @MainActor () -> String?
    @ObservationIgnored private let insert: @MainActor (String, String) -> Void
    @ObservationIgnored private var inserted = false
    @ObservationIgnored private let uptime: @MainActor () -> TimeInterval
    @ObservationIgnored private var confirmShownAt: TimeInterval = 0
    @ObservationIgnored private var chain: Task<Void, Never>?
    @ObservationIgnored private var utterance: String?
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var accepting = false
    @ObservationIgnored private var failed = false
    @ObservationIgnored private var capturing = false
    @ObservationIgnored private var pressedAt: TimeInterval?
    @ObservationIgnored private var swallowRelease = false

    public init(
        run: @escaping Run,
        source: VoiceAudioSource,
        uptime: @escaping @MainActor () -> TimeInterval = { ProcessInfo.processInfo.systemUptime },
        focusedComposer: @escaping @MainActor () -> String? = { nil },
        insert: @escaping @MainActor (String, String) -> Void = { _, _ in },
        pointer: @escaping @MainActor () -> VoicePointer
    ) {
        self.run = run
        self.source = source
        self.uptime = uptime
        self.focusedComposer = focusedComposer
        self.insert = insert
        self.pointer = pointer
    }

    public var dictating: Bool {
        target != nil
    }

    public var title: String {
        dictating ? "Dictation" : "Voice"
    }

    public var phase: VoiceState.Phase? {
        if failure != nil { return .error }
        if let state { return state.phase }
        return active ? .listening : nil
    }

    public var error: String? {
        failure ?? state?.error
    }

    public var heard: String {
        state?.heard ?? partial
    }

    public var level: Double {
        levels.last ?? 0
    }

    public var acceptsConfirmKeys: Bool {
        state?.phase == .confirm && failure == nil && !confirming
    }

    public var acceptsCancelKey: Bool {
        state?.phase == .sending && failure == nil
    }

    public var closeCancels: Bool {
        active || acceptsCancelKey
    }

    public func sendRemaining(at now: Date) -> Double {
        guard acceptsCancelKey, let sendDate = state?.sendDate else { return 0 }
        return min(max(sendDate.timeIntervalSince(now) / Self.sendGrace, 0), 1)
    }

    public var sendHint: String? {
        guard active else { return nil }
        if dictating { return holding ? "Release to insert" : "⌥Space to insert" }
        return holding ? "Release to send" : "⌥Space to send"
    }

    public var status: String {
        switch phase {
        case .loading: "Loading the speech model…"
        case .listening, nil: "Listening"
        case .sending: "Sending"
        case .thinking: dictating ? "Cleaning up" : "Thinking"
        case .confirm: "Confirm"
        case .done: dictating ? "Inserted" : "Done"
        case .error: "Didn't work"
        case .cancelled: "Cancelled"
        }
    }

    public func press(at time: TimeInterval, into composer: String? = nil) {
        if acceptsCancelKey {
            swallowRelease = true
            pressedAt = nil
            holding = false
            cancel()
            return
        }
        if active {
            swallowRelease = true
            pressedAt = nil
            holding = false
            finish()
            return
        }
        swallowRelease = false
        pressedAt = time
        holding = true
        begin(into: composer ?? focusedComposer())
    }

    public func release(at time: TimeInterval) {
        defer {
            pressedAt = nil
            swallowRelease = false
            holding = false
        }
        guard !swallowRelease, let pressedAt, time - pressedAt >= Self.tapLimit else { return }
        finish()
    }

    public func begin(into composer: String? = nil) {
        guard !active else { return }
        if acceptsCancelKey, let pending = utterance {
            enqueue { _ = await self.run(CommandInvocation(id: "voice.cancel", args: .object(["utterance": .string(pending)]))) }
        }
        generation += 1
        state = nil
        partial = ""
        failure = nil
        micDenied = false
        confirming = false
        levels = Array(repeating: 0, count: Self.meterBars)
        utterance = nil
        target = composer
        inserted = false
        failed = false
        accepting = true
        active = true
        visible = true
        let pointer = pointer()
        let generation = generation
        let mode = composer == nil ? nil : Dictation.mode
        enqueue { await self.start(pointer, mode, generation) }
    }

    public func send(_ pcm: Data) {
        guard accepting, !failed else { return }
        levels = Array((levels + [VoiceAudio.level(pcm)]).suffix(Self.meterBars))
        let generation = generation
        enqueue { await self.audio(pcm, generation) }
    }

    public func finish() {
        guard active else { return }
        active = false
        let generation = generation
        enqueue {
            await self.stopCapture()
            self.enqueue { await self.end(generation) }
        }
    }

    public func cancel() {
        let id = utterance
        let pending = active || !(state?.phase.finished ?? true)
        generation += 1
        let cancelled = generation
        active = false
        accepting = false
        visible = false
        enqueue {
            await self.stopCapture()
            if pending, let id = id ?? (self.generation == cancelled ? self.utterance : nil) { _ = await self.run(CommandInvocation(id: "voice.cancel", args: .object(["utterance": .string(id)]))) }
        }
    }

    public func dismiss() {
        guard !active else { return }
        visible = false
    }

    public func close() {
        if closeCancels {
            cancel()
        } else if state?.phase == .confirm {
            confirm(false)
        } else {
            dismiss()
        }
    }

    public func confirm(_ approve: Bool) {
        guard acceptsConfirmKeys, let id = utterance else { return }
        confirming = true
        var args: [String: JSONValue] = ["utterance": .string(id), "approve": .bool(approve)]
        if let shown = state?.confirm?.id { args["confirm"] = .number(Double(shown)) }
        Task {
            if case .failure(let error) = await run(CommandInvocation(id: "voice.confirm", args: .object(args))) {
                failure = error.commandText
            }
        }
    }

    public func confirmKey(_ approve: Bool, at time: TimeInterval) {
        if acceptsCancelKey {
            if !approve { cancel() }
            return
        }
        guard !approve || time - confirmShownAt >= Self.confirmArmDelay else { return }
        confirm(approve)
    }

    public func busEvent(_ event: BusEvent) {
        guard event.topic == Self.changedTopic, let id = utterance, !failed,
              event.payload["utterance"]?.stringValue == id,
              Int(event.payload["rev"]?.numberValue ?? 0) > state?.rev ?? -1 else { return }
        Task { await refresh(id) }
    }

    private func enqueue(_ operation: @escaping @MainActor () async -> Void) {
        let previous = chain
        chain = Task {
            await previous?.value
            await operation()
        }
    }

    private func start(_ pointer: VoicePointer, _ mode: String?, _ generation: Int) async {
        guard generation == self.generation else { return }
        let prompted = source.permissionPrompts
        guard await source.permission() else {
            micDenied = true
            await fail(Self.micDeniedMessage)
            return
        }
        guard generation == self.generation else { return }
        guard active || !prompted else {
            accepting = false
            visible = false
            return
        }
        do {
            try source.start(onChunk: { [weak self] pcm in self?.send(pcm) }, onEnd: { [weak self] in self?.finish() })
            capturing = true
        } catch {
            await fail(error.localizedDescription)
            return
        }
        var fields: [String: JSONValue] = ["pointer": (try? JSONValue.encoding(pointer)) ?? .null]
        if let mode { fields["mode"] = .string(mode) }
        let args: JSONValue = .object(fields)
        switch await run(CommandInvocation(id: "voice.start", args: args)) {
        case .success(let value):
            guard let id = value["utterance"]?.stringValue else { return await fail("voice.start returned no utterance") }
            utterance = id
            if generation == self.generation { await refresh(id) }
        case .failure(let error):
            await fail(error.commandText)
        }
    }

    private func audio(_ pcm: Data, _ generation: Int) async {
        guard generation == self.generation, !failed, let id = utterance else { return }
        switch await run(CommandInvocation(id: "voice.audio", args: .object(["utterance": .string(id), "pcm": .string(pcm.base64EncodedString())]))) {
        case .success(let value):
            if let text = value["partial"]?.stringValue, generation == self.generation { partial = text }
        case .failure(let error):
            accepting = false
            await refresh(id)
            if state?.phase.capturing ?? true { await fail(error.commandText) }
        }
    }

    private func end(_ generation: Int) async {
        accepting = false
        guard generation == self.generation, !failed, let id = utterance else { return }
        switch await run(CommandInvocation(id: "voice.end", args: .object(["utterance": .string(id)]))) {
        case .success:
            await refresh(id)
        case .failure(let error):
            await fail(error.commandText)
        }
    }

    private func refresh(_ id: String) async {
        guard case .success(let value) = await run(CommandInvocation(id: "voice.state", args: .object(["utterance": .string(id)]))),
              let next = try? value.decode(VoiceState.self),
              next.utterance == utterance, next.rev >= state?.rev ?? -1 else { return }
        if next.phase == .confirm, state?.phase != .confirm || state?.confirm != next.confirm {
            confirmShownAt = uptime()
            confirming = false
            visible = true
        }
        state = next
        if next.phase == .done, !inserted, let target, let text = next.text, !text.isEmpty {
            inserted = true
            insert(target, text)
            visible = false
        }
        if active, !next.phase.capturing {
            active = false
            accepting = false
            await stopCapture()
        }
    }

    private func fail(_ message: String) async {
        failed = true
        accepting = false
        active = false
        await stopCapture()
        failure = message
    }

    private func stopCapture() async {
        guard capturing else { return }
        capturing = false
        await source.stop()
    }
}
