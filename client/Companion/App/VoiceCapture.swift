import AVFoundation
import Foundation

import CompanionKit

@MainActor
final class VoiceCapture: VoiceAudioSource {
    enum Failure: LocalizedError {
        case noInput
        case unreadable(String)

        var errorDescription: String? {
            switch self {
            case .noInput: "No microphone found"
            case .unreadable(let path): "Can't read \(path)"
            }
        }
    }

    static let chunkInterval = Duration.milliseconds(320)

    private var engine: AVAudioEngine?
    private var converter: PCMConverter?
    private var streamer: Task<Void, Never>?
    private var onChunk: (@MainActor (Data) -> Void)?
    private var observers: [NSObjectProtocol] = []

    private var file: String? {
        #if DEBUG
        ProcessInfo.processInfo.environment["CLUBMATE_VOICE_FILE"].flatMap { $0.isEmpty ? nil : $0 }
        #else
        nil
        #endif
    }

    func permission() async -> VoicePermission {
        if file != nil { return .granted }
        switch AVAudioApplication.shared.recordPermission {
        case .granted: return .granted
        case .undetermined: return await AVAudioApplication.requestRecordPermission() ? .prompted : .denied
        default: return .denied
        }
    }

    func start(onChunk: @escaping @MainActor (Data) -> Void, onEnd: @escaping @MainActor () -> Void) throws {
        self.onChunk = onChunk
        if let file {
            guard let data = FileManager.default.contents(atPath: file) else { throw Failure.unreadable(file) }
            streamer = Task {
                var offset = 0
                while offset < data.count, !Task.isCancelled {
                    let end = min(offset + VoiceAudio.chunkBytes, data.count)
                    onChunk(Data(data[offset..<end]))
                    offset = end
                    try? await Task.sleep(for: Self.chunkInterval)
                }
                if !Task.isCancelled { onEnd() }
            }
            return
        }
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .default)
        try session.setActive(true)
        let engine = AVAudioEngine()
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0, let converter = PCMConverter(from: format, chunk: VoiceAudio.chunkBytes) else {
            try? session.setActive(false, options: .notifyOthersOnDeactivation)
            throw Failure.noInput
        }
        input.installTap(onBus: 0, bufferSize: 4096, format: format, block: Self.tap(converter, onChunk))
        engine.prepare()
        do {
            try engine.start()
        } catch {
            input.removeTap(onBus: 0)
            try? session.setActive(false, options: .notifyOthersOnDeactivation)
            throw error
        }
        let center = NotificationCenter.default
        observers = [
            center.addObserver(forName: AVAudioSession.interruptionNotification, object: session, queue: .main) { _ in MainActor.assumeIsolated { onEnd() } },
            center.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine, queue: .main) { _ in MainActor.assumeIsolated { onEnd() } },
        ]
        self.engine = engine
        self.converter = converter
    }

    func stop() async {
        streamer?.cancel()
        streamer = nil
        for observer in observers {
            NotificationCenter.default.removeObserver(observer)
        }
        observers = []
        guard let engine else {
            onChunk = nil
            return
        }
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
        self.engine = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        await withCheckedContinuation { done in DispatchQueue.main.async { done.resume() } }
        if let rest = converter?.flush(), !rest.isEmpty { onChunk?(rest) }
        converter = nil
        onChunk = nil
    }

    private nonisolated static func tap(_ converter: PCMConverter, _ onChunk: @escaping @MainActor (Data) -> Void) -> AVAudioNodeTapBlock {
        { buffer, _ in
            for chunk in converter.convert(buffer) {
                DispatchQueue.main.async { MainActor.assumeIsolated { onChunk(chunk) } }
            }
        }
    }
}

private final class PCMConverter: @unchecked Sendable {
    private let converter: AVAudioConverter
    private let output: AVAudioFormat
    private let ratio: Double
    private let chunk: Int
    private let lock = NSLock()
    private var pending = Data()

    init?(from input: AVAudioFormat, chunk: Int) {
        guard let output = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 16_000, channels: 1, interleaved: true),
              let converter = AVAudioConverter(from: input, to: output) else { return nil }
        self.converter = converter
        self.output = output
        self.chunk = chunk
        ratio = output.sampleRate / input.sampleRate
    }

    func convert(_ buffer: AVAudioPCMBuffer) -> [Data] {
        lock.lock()
        defer { lock.unlock() }
        let capacity = AVAudioFrameCount((Double(buffer.frameLength) * ratio).rounded(.up)) + 64
        guard let out = AVAudioPCMBuffer(pcmFormat: output, frameCapacity: capacity) else { return [] }
        var fed = false
        var error: NSError?
        converter.convert(to: out, error: &error) { _, status in
            if fed {
                status.pointee = .noDataNow
                return nil
            }
            fed = true
            status.pointee = .haveData
            return buffer
        }
        guard error == nil, let samples = out.int16ChannelData else { return [] }
        pending.append(Data(bytes: samples[0], count: Int(out.frameLength) * MemoryLayout<Int16>.size))
        var chunks: [Data] = []
        while pending.count >= chunk {
            chunks.append(Data(pending.prefix(chunk)))
            pending.removeFirst(chunk)
        }
        return chunks
    }

    func flush() -> Data {
        lock.lock()
        defer { lock.unlock() }
        let rest = pending
        pending = Data()
        return rest
    }
}
