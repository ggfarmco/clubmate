import AVFoundation
import CoreTransferable
import PhotosUI
import SwiftUI

import CompanionKit

struct AttachmentsRow: View {
    @Binding var attachments: [ComposerAttachment]
    let importing: Int

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                ForEach(Array(attachments.enumerated()), id: \.offset) { _, attachment in
                    if let data = attachment.images.first, let image = UIImage(data: data) {
                        Image(uiImage: image)
                            .resizable()
                            .scaledToFill()
                            .frame(width: 64, height: 64)
                            .clipShape(.rect(cornerRadius: 12))
                            .overlay(alignment: .bottomLeading) {
                                if case .video(_, _, let duration) = attachment {
                                    Label(Duration.seconds(duration).formatted(.time(pattern: .minuteSecond)), systemImage: "play.fill")
                                        .font(.caption2.weight(.semibold).monospacedDigit())
                                        .foregroundStyle(.white)
                                        .shadow(radius: 2)
                                        .padding(5)
                                }
                            }
                            .overlay(alignment: .topTrailing) {
                                Button("Remove", systemImage: "xmark.circle.fill") { attachments.removeAll { $0 == attachment } }
                                    .labelStyle(.iconOnly)
                                    .symbolRenderingMode(.palette)
                                    .foregroundStyle(.white, .black.opacity(0.6))
                                    .padding(4)
                            }
                    }
                }
                ForEach(0..<importing, id: \.self) { _ in
                    ProgressView()
                        .frame(width: 64, height: 64)
                        .background(.fill.secondary, in: .rect(cornerRadius: 12))
                }
            }
            .padding(.horizontal, 4)
        }
        .scrollIndicators(.hidden)
    }
}

enum ComposerMedia {
    static func load(_ item: PhotosPickerItem) async -> ComposerAttachment? {
        if item.supportedContentTypes.contains(where: { $0.conforms(to: .movie) }) {
            guard let movie = try? await item.loadTransferable(type: PickedMovie.self) else { return nil }
            defer { try? FileManager.default.removeItem(at: movie.url) }
            return await VideoFrames.extract(movie.url)
        }
        guard let data = try? await item.loadTransferable(type: Data.self), let jpeg = PhotoEncoder.jpeg(data) else { return nil }
        return .photo(jpeg)
    }
}

struct PickedMovie: Transferable {
    let url: URL

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(importedContentType: .movie) { received in
            let copy = URL.temporaryDirectory.appending(path: UUID().uuidString).appendingPathExtension(received.file.pathExtension)
            try FileManager.default.copyItem(at: received.file, to: copy)
            return PickedMovie(url: copy)
        }
    }
}

enum VideoFrames {
    static let maxFrames = 8
    static let maxSide: CGFloat = 1024

    static func extract(_ url: URL) async -> ComposerAttachment? {
        let asset = AVURLAsset(url: url)
        guard let duration = try? await asset.load(.duration).seconds, duration.isFinite, duration > 0 else { return nil }
        let count = min(maxFrames, max(2, Int(duration.rounded(.up))))
        let times = (0..<count).map { CMTime(seconds: duration * (Double($0) + 0.5) / Double(count), preferredTimescale: 600) }
        let generator = AVAssetImageGenerator(asset: asset)
        generator.appliesPreferredTrackTransform = true
        generator.maximumSize = CGSize(width: maxSide, height: maxSide)
        generator.requestedTimeToleranceBefore = CMTime(seconds: 0.5, preferredTimescale: 600)
        generator.requestedTimeToleranceAfter = CMTime(seconds: 0.5, preferredTimescale: 600)
        var frames: [(time: Double, jpeg: Data)] = []
        for await result in generator.images(for: times) {
            guard let image = try? result.image, let time = try? result.actualTime, let jpeg = UIImage(cgImage: image).jpegData(compressionQuality: 0.7) else { continue }
            frames.append((time.seconds, jpeg))
        }
        guard !frames.isEmpty else { return nil }
        frames.sort { $0.time < $1.time }
        return .video(frames: frames.map(\.jpeg), times: frames.map(\.time), duration: duration)
    }
}

enum PhotoEncoder {
    static let maxSide: CGFloat = 1568

    static func jpeg(_ data: Data) -> Data? {
        guard let image = UIImage(data: data) else { return nil }
        let scale = min(1, maxSide / max(image.size.width, image.size.height))
        let size = CGSize(width: (image.size.width * scale).rounded(), height: (image.size.height * scale).rounded())
        let format = UIGraphicsImageRendererFormat.default()
        format.scale = 1
        let resized = UIGraphicsImageRenderer(size: size, format: format).image { _ in image.draw(in: CGRect(origin: .zero, size: size)) }
        return resized.jpegData(compressionQuality: 0.8)
    }
}
