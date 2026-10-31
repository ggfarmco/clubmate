import Foundation

public enum ComposerAttachment: Sendable, Equatable {
    case photo(Data)
    case video(frames: [Data], times: [Double], duration: Double)

    public var images: [Data] {
        switch self {
        case .photo(let data): [data]
        case .video(let frames, _, _): frames
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
