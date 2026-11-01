import Foundation

public enum ComposerAttachment: Sendable, Equatable {
    case photo(Data)
    case video(frames: [Data], times: [Double], duration: Double)
    case file(name: String, data: Data)

    public static let maxBytes = 10 << 20

    public var images: [Data] {
        switch self {
        case .photo(let data): [data]
        case .video(let frames, _, _): frames
        case .file: []
        }
    }

    public var fileName: String? {
        if case .file(let name, _) = self { name } else { nil }
    }

    public var bytes: Int {
        switch self {
        case .file(_, let data): data.count
        default: images.reduce(0) { $0 + $1.count }
        }
    }

    var payload: [JSONValue] {
        switch self {
        case .file(let name, let data):
            [.object(["kind": .string("file"), "name": .string(name), "data": .string(data.base64EncodedString())])]
        default:
            images.map { .object(["kind": .string("image"), "mimeType": .string("image/jpeg"), "data": .string($0.base64EncodedString())]) }
        }
    }

    public static func promptText(_ text: String, _ attachments: [ComposerAttachment]) -> String {
        let notes = attachments.compactMap { attachment -> String? in
            guard case .video(let frames, let times, let duration) = attachment else { return nil }
            let at = times.map { seconds($0) }.joined(separator: ", ")
            return "[Video, \(seconds(duration)) — \(frames.count) frames in order at \(at)]"
        }
        return ([text] + notes).filter { !$0.isEmpty }.joined(separator: "\n\n")
    }

    private static func seconds(_ value: Double) -> String {
        String(format: "%.1f s", value)
    }
}
