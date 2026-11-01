import AVFoundation
import Foundation

import ClubmateKit

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

    static let chunkBytes = 10_240
    static let chunkInterval = Duration.milliseconds(320)

    private var engine: AVAudioEngine?
    private var converter: PCMConverter?
    private var streamer: Task<Void, Never>?
    private var onChunk: (@MainActor (Data) -> Void)?
    private var configChange: NSObjectProtocol?

    private var file: String? {
        ProcessInfo.processInfo.environment["CLUBMATE_VOICE_FILE"].flatMap { $0.isEmpty ? nil : $0 }
    }

    func permission() async -> Bool {
        if file != nil { return true }
        switch AVCaptureDevice.authorizationStatus(for: .audio) {
        case .authorized: return true
        case .notDetermined: return await AVCaptureDevice.requestAccess(for: .audio)
        default: return false
        }
    }

    func start(onChunk: @escaping @MainActor (Data) -> Void, onEnd: @escaping @MainActor () -> Void) throws {
        self.onChunk = onChunk
        if let file {
            guard let data = FileManager.default.contents(atPath: file) else { throw Failure.unreadable(file) }
            streamer = Task {
                var offset = 0
                while offset < data.count, !Task.isCancelled {
                    let end = min(offset + Self.chunkBytes, data.count)
                    onChunk(Data(data[offset..<end]))
                    offset = end
                    try? await Task.sleep(for: Self.chunkInterval)
                }
                if !Task.isCancelled { onEnd() }
            }
            return
        }
        let engine = AVAudioEngine()
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0, let converter = PCMConverter(from: format, chunk: Self.chunkBytes) else { throw Failure.noInput }
        try input.installAudioTap(onBus: 0, bufferSize: 4096, format: format, tapProvider: Self.tap(converter, onChunk))
        engine.prepare()
        do {
            try engine.start()
        } catch {
            input.removeTap(onBus: 0)
            throw error
        }
        configChange = NotificationCenter.default.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine, queue: .main) { _ in
            MainActor.assumeIsolated { onEnd() }
        }
        self.engine = engine
        self.converter = converter
    }

    func stop() async {
        streamer?.cancel()
        streamer = nil
        if let configChange { NotificationCenter.default.removeObserver(configChange) }
        configChange = nil
        guard let engine else {
            onChunk = nil
            return
        }
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
        self.engine = nil
        await withCheckedContinuation { done in DispatchQueue.main.async { done.resume() } }
        if let rest = converter?.flush(), !rest.isEmpty { onChunk?(rest) }
        converter = nil
        onChunk = nil
    }

    private nonisolated static func tap(_ converter: PCMConverter, _ onChunk: @escaping @MainActor (Data) -> Void) -> @Sendable (AVReadOnlyAudioPCMBuffer, AVAudioTime) -> Void {
        { buffer, _ in
            for chunk in converter.convert(AVAudioPCMBuffer(copying: buffer)) {
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
