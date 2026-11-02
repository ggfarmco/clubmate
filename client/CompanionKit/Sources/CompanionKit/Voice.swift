import Foundation
import Observation

public struct VoiceTarget: Equatable, Sendable {
    public let computer: String
    public let workspace: String
    public let pointer: VoicePointer

    public init(computer: String, workspace: String, pointer: VoicePointer) {
        self.computer = computer
        self.workspace = workspace
        self.pointer = pointer
    }

    public static func phone(computer: String, workspace: String, plugin: String?, detail: String?) -> VoiceTarget {
        let section: String
        var selection: VoicePointer.Selection?
        switch plugin ?? AgentsPluginInfo.id {
        case AgentsPluginInfo.id:
            section = AgentsPluginInfo.id
            if let detail, detail != AgentsPluginInfo.newSession { selection = .init(session: detail) }
        case BacklogPluginInfo.id:
            section = BacklogPluginInfo.id
            selection = detail.flatMap(Int.init).map { .init(task: $0) }
        case MorePluginInfo.id:
            section = detail ?? MorePluginInfo.id
        case let other:
            section = other
        }
        return VoiceTarget(computer: computer, workspace: workspace, pointer: VoicePointer(device: "phone", section: section, selection: selection))
    }
}

public enum VoicePermission: Sendable {
    case granted, denied, prompted
}

@MainActor
public protocol VoiceAudioSource: AnyObject {
    func permission() async -> VoicePermission
    func start(onChunk: @escaping @MainActor (Data) -> Void, onEnd: @escaping @MainActor () -> Void) throws
    func stop() async
}

@MainActor
@Observable
public final class VoiceSession {
    public typealias Runner = @MainActor (String) -> (any CommandRunner)?

    public static let changedTopic = "voice.changed"
    public static let micDeniedMessage = "Microphone access is off — Settings → Privacy & Security → Microphone"
    public static let nothingHeard = "nothing heard"
    public static let tapLimit: TimeInterval = 0.6
    public static let confirmArmDelay: TimeInterval = 0.6
    public static let sendDelay: TimeInterval = 1
    static let meterBars = 16

    public private(set) var state: VoiceState?
    public private(set) var partial = ""
    public private(set) var active = false
    public private(set) var visible = false
    public private(set) var holding = false
    public private(set) var micDenied = false
    public private(set) var target: VoiceTarget?
    public private(set) var levels: [Double] = Array(repeating: 0, count: VoiceSession.meterBars)
    public private(set) var sendingSince: TimeInterval?
    private var failure: String?
    private var confirming = false

    @ObservationIgnored private let runner: Runner
    @ObservationIgnored private let source: VoiceAudioSource
    @ObservationIgnored private let current: @MainActor () -> VoiceTarget?
    @ObservationIgnored private let uptime: @MainActor () -> TimeInterval
    @ObservationIgnored private var confirmShownAt: TimeInterval = 0
    @ObservationIgnored private var chain: Task<Void, Never>?
    @ObservationIgnored private var utterance: String?
    @ObservationIgnored private var buffered = Data()
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var accepting = false
    @ObservationIgnored private var failed = false
    @ObservationIgnored private var capturing = false
    @ObservationIgnored private var pressedAt: TimeInterval?
    @ObservationIgnored private var swallowRelease = false

    public init(
        runner: @escaping Runner,
        source: VoiceAudioSource,
        uptime: @escaping @MainActor () -> TimeInterval = { ProcessInfo.processInfo.systemUptime },
        target: @escaping @MainActor () -> VoiceTarget?
    ) {
        self.runner = runner
        self.source = source
        self.uptime = uptime
        current = target
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

    public var autoHides: Bool {
        switch phase {
        case .done, .cancelled: true
        case .error: error == Self.nothingHeard
        default: false
        }
    }

    public func sendProgress(at time: TimeInterval) -> Double {
        guard phase == .sending, let sendingSince else { return 0 }
        return min(1, max(0, (time - sendingSince) / Self.sendDelay))
    }

    public var acceptsConfirm: Bool {
        state?.phase == .confirm && failure == nil && !confirming
    }

    public var status: String {
        switch phase {
        case .loading: "Loading the speech model…"
        case .listening, nil: "Listening"
        case .sending: "Sending"
        case .thinking: "Thinking"
        case .confirm: "Confirm"
        case .done: "Done"
        case .error: "Didn't work"
        case .cancelled: "Cancelled"
        }
    }

    public var hint: String {
        holding ? "Release to send" : "Tap the mic to send"
    }

    public func press(at time: TimeInterval) {
        if active {
            swallowRelease = true
            pressedAt = nil
            finish()
            return
        }
        swallowRelease = false
        pressedAt = time
        begin()
        holding = active
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

    public func begin() {
        guard !active, let target = current() else { return }
        generation += 1
        self.target = target
        state = nil
        partial = ""
        failure = nil
        micDenied = false
        confirming = false
        sendingSince = nil
        levels = Array(repeating: 0, count: Self.meterBars)
        utterance = nil
        buffered = Data()
        failed = false
        accepting = true
        active = true
        visible = true
        let generation = generation
        enqueue { await self.start(target, generation) }
    }

    public func send(_ pcm: Data) {
        guard accepting, !failed else { return }
        levels = Array((levels + [VoiceAudio.level(pcm)]).suffix(Self.meterBars))
        buffered.append(pcm)
        let generation = generation
        enqueue { await self.drain(generation) }
    }

    public func finish() {
        finish(then: nil)
    }

    public func background(hold: @MainActor () -> @MainActor () -> Void) {
        guard active else { return }
        finish(then: hold())
    }

    private func finish(then done: (@MainActor () -> Void)?) {
        guard active else { return }
        active = false
        holding = false
        let generation = generation
        enqueue {
            await self.stopCapture()
            self.enqueue {
                await self.end(generation)
                done?()
            }
        }
    }

    public func cancel() {
        let id = utterance
        let target = target
        let pending = active || !(state?.phase.finished ?? true)
        generation += 1
        let cancelled = generation
        active = false
        holding = false
        accepting = false
        visible = false
        buffered = Data()
        enqueue {
            await self.stopCapture()
            guard pending, let target, let id = id ?? (self.generation == cancelled ? self.utterance : nil) else { return }
            _ = try? await self.runner(target.computer)?.command("voice.cancel", workspace: target.workspace, args: .object(["utterance": .string(id)]))
        }
    }

    public func dismiss() {
        guard !active else { return }
        visible = false
    }

    public func close() {
        if active || phase == .sending {
            cancel()
        } else if phase == .confirm {
            confirm(false)
        } else {
            dismiss()
        }
    }

    public func confirm(_ approve: Bool) {
        guard acceptsConfirm, let id = utterance, let target else { return }
        confirming = true
        var args: [String: JSONValue] = ["utterance": .string(id), "approve": .bool(approve)]
        if let shown = state?.confirm?.id { args["confirm"] = .number(Double(shown)) }
        Task {
            if case .failure(let failed) = await call(target, "voice.confirm", args) { failure = failed.message }
        }
    }

    public func answer(_ approve: Bool, at time: TimeInterval) {
        guard !approve || time - confirmShownAt >= Self.confirmArmDelay else { return }
        confirm(approve)
    }

    public func handle(_ signal: ConnectionSignal, from computer: String) {
        guard case .event(let event) = signal, event.topic == Self.changedTopic, computer == target?.computer,
              let id = utterance, !failed, event.payload["utterance"]?.stringValue == id,
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

    private func call(_ target: VoiceTarget, _ id: String, _ args: [String: JSONValue]) async -> Result<JSONValue, VoiceFailure> {
        guard let runner = runner(target.computer) else { return .failure(VoiceFailure("The Mac is no longer paired")) }
        do {
            return .success(try await runner.command(id, workspace: target.workspace, args: .object(args)))
        } catch {
            return .failure(VoiceFailure((error as? RemoteError)?.message ?? error.localizedDescription))
        }
    }

    private func start(_ target: VoiceTarget, _ generation: Int) async {
        guard generation == self.generation else { return }
        switch await source.permission() {
        case .granted:
            break
        case .denied:
            micDenied = true
            await fail(Self.micDeniedMessage)
            return
        case .prompted:
            guard generation == self.generation else { return }
            active = false
            holding = false
            accepting = false
            visible = false
            buffered = Data()
            return
        }
        guard generation == self.generation else { return }
        do {
            try source.start(onChunk: { [weak self] pcm in self?.send(pcm) }, onEnd: { [weak self] in self?.finish() })
            capturing = true
        } catch {
            await fail(error.localizedDescription)
            return
        }
        switch await call(target, "voice.start", ["pointer": (try? JSONValue.encoding(target.pointer)) ?? .null]) {
        case .success(let value):
            guard let id = value["utterance"]?.stringValue else { return await fail("voice.start returned no utterance") }
            utterance = id
            if generation == self.generation { await refresh(id) }
        case .failure(let failure):
            await fail(failure.message)
        }
    }

    private func drain(_ generation: Int) async {
        while generation == self.generation, !failed, let id = utterance, let target, !buffered.isEmpty {
            let batch = Data(buffered.prefix(VoiceAudio.maxCallBytes))
            buffered.removeFirst(batch.count)
            switch await call(target, "voice.audio", ["utterance": .string(id), "pcm": .string(batch.base64EncodedString())]) {
            case .success(let value):
                if let text = value["partial"]?.stringValue, generation == self.generation { partial = text }
            case .failure(let failure):
                accepting = false
                buffered = Data()
                await refresh(id)
                if state?.phase.capturing ?? true { await fail(failure.message) }
                return
            }
        }
    }

    private func end(_ generation: Int) async {
        accepting = false
        guard generation == self.generation, !failed, let id = utterance, let target else { return }
        switch await call(target, "voice.end", ["utterance": .string(id)]) {
        case .success:
            await refresh(id)
        case .failure(let failure):
            await fail(failure.message)
        }
    }

    private func refresh(_ id: String) async {
        guard let target, case .success(let value) = await call(target, "voice.state", ["utterance": .string(id)]),
              let next = try? value.decode(VoiceState.self),
              next.utterance == utterance, next.rev >= state?.rev ?? -1 else { return }
        if next.phase == .confirm, state?.phase != .confirm || state?.confirm != next.confirm {
            confirmShownAt = uptime()
            confirming = false
            visible = true
        }
        if next.phase == .sending, state?.phase != .sending {
            sendingSince = uptime()
        }
        state = next
        if active, !next.phase.capturing {
            active = false
            holding = false
            accepting = false
            await stopCapture()
        }
    }

    private func fail(_ message: String) async {
        failed = true
        accepting = false
        active = false
        holding = false
        buffered = Data()
        await stopCapture()
        failure = message
    }

    private func stopCapture() async {
        guard capturing else { return }
        capturing = false
        await source.stop()
    }
}

struct VoiceFailure: Error {
    let message: String

    init(_ message: String) {
        self.message = message
    }
}
