import AVFoundation
import Foundation

public final class OpusEncoder {
    private let converter: AVAudioConverter
    private let input: AVAudioFormat
    private let output: AVAudioFormat
    private var pending: AVAudioPCMBuffer?
    private var ended = false

    public init?(bitRate: Int = VoiceAudio.opusBitRate) {
        guard let input = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: Double(VoiceAudio.sampleRate), channels: 1, interleaved: true) else { return nil }
        var description = AudioStreamBasicDescription(
            mSampleRate: Double(VoiceAudio.sampleRate),
            mFormatID: kAudioFormatOpus,
            mFormatFlags: 0,
            mBytesPerPacket: 0,
            mFramesPerPacket: UInt32(VoiceAudio.opusFrameSamples),
            mBytesPerFrame: 0,
            mChannelsPerFrame: 1,
            mBitsPerChannel: 0,
            mReserved: 0
        )
        guard let output = AVAudioFormat(streamDescription: &description),
              let converter = AVAudioConverter(from: input, to: output) else { return nil }
        converter.bitRate = bitRate
        self.converter = converter
        self.input = input
        self.output = output
    }

    public func encode(_ pcm: Data) -> [Data] {
        let frames = pcm.count / 2
        guard !ended, frames > 0, let buffer = AVAudioPCMBuffer(pcmFormat: input, frameCapacity: AVAudioFrameCount(frames)) else { return [] }
        buffer.frameLength = AVAudioFrameCount(frames)
        pcm.withUnsafeBytes { raw in
            guard let source = raw.baseAddress, let target = buffer.int16ChannelData?[0] else { return }
            memcpy(target, source, frames * 2)
        }
        pending = buffer
        return drain()
    }

    public func finish() -> [Data] {
        guard !ended else { return [] }
        ended = true
        return drain()
    }

    private func drain() -> [Data] {
        var packets: [Data] = []
        while true {
            let buffer = AVAudioCompressedBuffer(format: output, packetCapacity: 1, maximumPacketSize: max(converter.maximumOutputPacketSize, 1275))
            var failure: NSError?
            let status = converter.convert(to: buffer, error: &failure) { _, outStatus in
                if let pending = self.pending {
                    self.pending = nil
                    outStatus.pointee = .haveData
                    return pending
                }
                outStatus.pointee = self.ended ? .endOfStream : .noDataNow
                return nil
            }
            if status == .error { return packets }
            packets += Self.packets(in: buffer)
            if status != .haveData { return packets }
        }
    }

    private static func packets(in buffer: AVAudioCompressedBuffer) -> [Data] {
        guard let descriptions = buffer.packetDescriptions else { return [] }
        return (0..<Int(buffer.packetCount)).compactMap { index in
            let packet = descriptions[index]
            guard packet.mDataByteSize > 0 else { return nil }
            return Data(bytes: buffer.data.advanced(by: Int(packet.mStartOffset)), count: Int(packet.mDataByteSize))
        }
    }
}
