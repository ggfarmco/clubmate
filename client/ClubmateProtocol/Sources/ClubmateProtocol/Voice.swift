import Foundation

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
        case loading, listening, sending, thinking, confirm, done, error, cancelled

        public var capturing: Bool { self == .loading || self == .listening }
        public var finished: Bool { self == .done || self == .error || self == .cancelled }
    }

    public struct Confirm: Decodable, Sendable, Equatable {
        public let id: Int?
        public let title: String
        public let command: String
        public let args: JSONValue?

        public var line: String {
            guard case .object(let fields)? = args, !fields.isEmpty else { return command }
            return ([command] + fields.keys.sorted().map { "\(Self.key($0))=\(Self.text(fields[$0] ?? .null))" }).joined(separator: " ")
        }

        private static func key(_ name: String) -> String {
            let plain = !name.isEmpty && name.unicodeScalars.allSatisfy { $0.isASCII && (CharacterSet.alphanumerics.contains($0) || "_.-".unicodeScalars.contains($0)) }
            return plain ? name : text(.string(name))
        }

        private static func text(_ value: JSONValue) -> String {
            switch value {
            case .bool(let flag): String(flag)
            case .number(let number): Int(exactly: number).map(String.init) ?? String(number)
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
    public let mode: String?
    public let phase: Phase
    public let partial: String?
    public let heard: String?
    public let text: String?
    public let sendAt: String?
    public let plan: String?
    public let result: String?
    public let error: String?
    public let confirm: Confirm?
    public let session: String?
    public let rev: Int

    public var dictation: Bool { mode == "dictation" }

    public var sendDate: Date? {
        guard let sendAt else { return nil }
        return (try? Date(sendAt, strategy: Date.ISO8601FormatStyle(includingFractionalSeconds: true)))
            ?? (try? Date(sendAt, strategy: .iso8601))
    }
}

public enum VoiceAudio {
    public static let chunkBytes = 10_240
    public static let maxCallBytes = 65_536

    public static func level(_ pcm: Data) -> Double {
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
