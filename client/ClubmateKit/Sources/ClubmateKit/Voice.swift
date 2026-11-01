import Foundation
import Observation

public struct VoicePointer: Encodable, Sendable, Equatable {
    public struct Selection: Encodable, Sendable, Equatable {
        public let session: String?
        public let task: Int?
        public let file: String?
        public let line: Int?

        public init(session: String? = nil, task: Int? = nil, file: String? = nil, line: Int? = nil) {
            self.session = session
            self.task = task
            self.file = file
            self.line = line
        }
    }

    public let device: String
    public let section: String
    public let selection: Selection?

    public init(device: String = "mac", section: String, selection: Selection? = nil) {
        self.device = device
        self.section = section
        self.selection = selection
    }
}

public struct VoiceState: Decodable, Sendable, Equatable {
    public enum Phase: String, Decodable, Sendable {
        case loading, listening, thinking, confirm, done, error, cancelled

        var capturing: Bool { self == .loading || self == .listening }
        var finished: Bool { self == .done || self == .error || self == .cancelled }
    }

    public struct Confirm: Decodable, Sendable, Equatable {
        public let title: String
        public let command: String
        public let args: JSONValue?

        public var line: String {
            guard case .object(let fields)? = args, !fields.isEmpty else { return command }
            return ([command] + fields.keys.sorted().map { "\($0)=\(Self.text(fields[$0] ?? .null))" }).joined(separator: " ")
        }

        private static func text(_ value: JSONValue) -> String {
            switch value {
            case .bool(let flag): String(flag)
            case .number(let number): number.rounded() == number ? String(Int(number)) : String(number)
            default: (try? encoder.encode(value)).map { String(decoding: $0, as: UTF8.self) } ?? ""
            }
        }

        private static var encoder: JSONEncoder {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
            return encoder
        }
    }

    public let utterance: String
    public let phase: Phase
    public let partial: String?
    public let heard: String?
    public let plan: String?
    public let result: String?
    public let error: String?
    public let confirm: Confirm?
    public let session: String?
    public let rev: Int
}

@MainActor
public protocol VoiceAudioSource: AnyObject {
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
    public static let tapLimit: TimeInterval = 0.25
    public static let confirmArmDelay: TimeInterval = 0.6
    static let meterBars = 16

    public private(set) var state: VoiceState?
    public private(set) var partial = ""
    public private(set) var active = false
    public private(set) var visible = false
    public private(set) var micDenied = false
    public private(set) var levels: [Double] = Array(repeating: 0, count: VoiceController.meterBars)
    private var failure: String?
    private var confirming = false

    @ObservationIgnored private let run: Run
    @ObservationIgnored private let source: VoiceAudioSource
    @ObservationIgnored private let pointer: @MainActor () -> VoicePointer
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
        pointer: @escaping @MainActor () -> VoicePointer
    ) {
        self.run = run
        self.source = source
        self.uptime = uptime
        self.pointer = pointer
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

    public var status: String {
        switch phase {
        case .loading: "Loading the speech model…"
        case .listening, nil: "Listening"
        case .thinking: "Thinking"
        case .confirm: "Confirm"
        case .done: "Done"
        case .error: "Didn't work"
        case .cancelled: "Cancelled"
        }
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
    }

    public func release(at time: TimeInterval) {
        defer {
            pressedAt = nil
            swallowRelease = false
        }
        guard !swallowRelease, let pressedAt, time - pressedAt >= Self.tapLimit else { return }
        finish()
    }

    public func begin() {
        guard !active else { return }
        generation += 1
        state = nil
        partial = ""
        failure = nil
        micDenied = false
        confirming = false
        levels = Array(repeating: 0, count: Self.meterBars)
        utterance = nil
        failed = false
        accepting = true
        active = true
        visible = true
        let pointer = pointer()
        let generation = generation
        enqueue { await self.start(pointer, generation) }
    }

    public func send(_ pcm: Data) {
        guard accepting, !failed else { return }
        levels = Array((levels + [Self.level(pcm)]).suffix(Self.meterBars))
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

    public func confirm(_ approve: Bool) {
        guard acceptsConfirmKeys, let id = utterance else { return }
        confirming = true
        Task {
            if case .failure(let error) = await run(CommandInvocation(id: "voice.confirm", args: .object(["utterance": .string(id), "approve": .bool(approve)]))) {
                failure = error.commandText
            }
        }
    }

    public func confirmKey(_ approve: Bool, at time: TimeInterval) {
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

    private func start(_ pointer: VoicePointer, _ generation: Int) async {
        guard generation == self.generation else { return }
        guard await source.permission() else {
            micDenied = true
            await fail(Self.micDeniedMessage)
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
        let args: JSONValue = .object(["pointer": (try? JSONValue.encoding(pointer)) ?? .null])
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
        if next.phase == .confirm, state?.phase != .confirm {
            confirmShownAt = uptime()
            visible = true
        }
        state = next
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

    static func level(_ pcm: Data) -> Double {
        let samples = pcm.count / 2
        guard samples > 0 else { return 0 }
        let sum = pcm.withUnsafeBytes { raw in
            raw.bindMemory(to: Int16.self).reduce(0.0) { total, sample in
                let value = Double(Int16(littleEndian: sample)) / Double(Int16.max)
                return total + value * value
            }
        }
        let rms = (sum / Double(samples)).squareRoot()
        guard rms > 0 else { return 0 }
        return min(1, max(0, (20 * log10(rms) + 50) / 50))
    }
}
