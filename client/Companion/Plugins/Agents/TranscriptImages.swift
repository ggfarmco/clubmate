import SwiftUI
import UIKit

import ClubmateDocument
import CompanionKit

extension EnvironmentValues {
    @Entry var imageStore: AgentsStore? = nil
    @Entry var imageSession = ""
    @Entry var openImage: Opener? = nil
}

@MainActor
private enum DecodedImages {
    static let cache = {
        let cache = NSCache<NSString, UIImage>()
        cache.countLimit = 64
        cache.totalCostLimit = 256 << 20
        return cache
    }()

    static func image(_ data: Data, key: String) -> UIImage? {
        if let cached = cache.object(forKey: key as NSString) { return cached }
        guard let decoded = UIImage(data: data) else { return nil }
        let image = decoded.imageOrientation == .up ? decoded : upright(decoded)
        cache.setObject(image, forKey: key as NSString, cost: Int(image.size.width * image.scale * image.size.height * image.scale) * 4)
        return image
    }

    private static func upright(_ image: UIImage) -> UIImage {
        let format = UIGraphicsImageRendererFormat()
        format.scale = image.scale
        return UIGraphicsImageRenderer(size: image.size, format: format).image { _ in image.draw(at: .zero) }
    }
}

private struct LoadedImage {
    let ref: ImageRef
    let entry: String
    let session: String
    let store: AgentsStore
    var side: Int?

    var key: String { AgentsStore.imageKey(ref.path, entry: entry, in: session, side: side) }

    @MainActor
    var state: TranscriptImage? { store.image(ref.path, entry: entry, in: session, side: side) }

    @MainActor
    var decoded: UIImage? {
        guard case .loaded(let data) = state else { return nil }
        return DecodedImages.image(data, key: key)
    }

    @MainActor
    var missing: Bool {
        switch state {
        case .missing: true
        case .loaded: decoded == nil
        case .loading, nil: false
        }
    }

    func load() async {
        await store.loadImage(ref.path, entry: entry, in: session, side: side)
    }

    @MainActor
    func show(_ shown: Bool) {
        store.showImage(ref.path, entry: entry, in: session, side: side, shown: shown)
        if shown, state == nil || state == .missing {
            Task { await load() }
        }
    }
}

struct ImageTiles: View {
    let images: [ImageRef]
    let entry: String
    let session: String
    let store: AgentsStore
    let open: (ImageRef) -> Void
    @State private var width = 0.0
    @Environment(\.displayScale) private var displayScale

    var body: some View {
        FlowLayout(spacing: 8) {
            ForEach(images) { ref in
                ImageTile(image: LoadedImage(ref: ref, entry: entry, session: session, store: store, side: ImageTile.side(displayScale)), maxWidth: width) { open(ref) }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: Double.self) { $0.size.width } action: { width = $0 }
    }
}

private struct ImageTile: View {
    static let height = 112.0

    static func side(_ scale: Double) -> Int {
        Int((height * scale).rounded(.up))
    }

    let image: LoadedImage
    let maxWidth: Double
    let open: () -> Void
    @Environment(\.displayScale) private var displayScale

    var body: some View {
        let decoded = image.decoded
        let aspect = image.ref.aspect ?? decoded.map { Double($0.size.width / max($0.size.height, 1)) } ?? 4.0 / 3.0
        let width = min(Self.height * min(max(aspect, 1), 2), maxWidth > 0 ? maxWidth : .infinity)
        Button(action: open) {
            VStack(alignment: .leading, spacing: 6) {
                Color.surface2
                    .overlay { thumbnail(decoded, fits: decoded.map { $0.size.width <= width && $0.size.height <= Self.height } ?? false) }
                    .frame(width: width, height: Self.height)
                    .clipShape(.rect(cornerRadius: 12))
                    .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(Color(.separator), lineWidth: 1 / displayScale) }
                Text(image.ref.name)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .frame(width: width, alignment: .leading)
            }
            .contentShape(.rect)
        }
        .buttonStyle(ImageTilePressStyle())
        .accessibilityLabel(image.ref.name)
        .accessibilityHint("Opens the image")
        .onScrollVisibilityChange(threshold: 0.01) { image.show($0) }
    }

    @ViewBuilder
    private func thumbnail(_ decoded: UIImage?, fits: Bool) -> some View {
        if let decoded, fits {
            Image(uiImage: decoded)
        } else if let decoded {
            Color.clear.overlay(alignment: .top) {
                Image(uiImage: decoded)
                    .resizable()
                    .scaledToFill()
            }
        } else if image.missing {
            ImageMissing()
        } else {
            ProgressView()
        }
    }
}

private struct ImageTilePressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.6 : 1)
    }
}

private struct ImageMissing: View {
    var body: some View {
        VStack(spacing: 6) {
            Image(systemName: "photo")
                .font(.title3)
            Text("Image not found")
                .font(.footnote)
                .lineLimit(2)
                .multilineTextAlignment(.center)
        }
        .foregroundStyle(Color.captionInk)
        .padding(8)
    }
}

struct ImageScreen: View {
    let ref: ImageRef
    let entry: String
    let session: String
    let store: AgentsStore
    @Environment(\.displayScale) private var displayScale

    var body: some View {
        let image = LoadedImage(ref: ref, entry: entry, session: session, store: store)
        let thumb = LoadedImage(ref: ref, entry: entry, session: session, store: store, side: ImageTile.side(displayScale))
        ZoomScreen(title: ref.name) {
            if let decoded = image.decoded, let cg = decoded.cgImage {
                ZoomableImage(image: cg, scale: decoded.scale)
            } else if image.missing {
                ImageMissing()
            } else if let preview = thumb.decoded {
                Image(uiImage: preview)
                    .resizable()
                    .scaledToFit()
                    .overlay { ProgressView() }
            } else {
                ProgressView()
            }
        }
        .task(id: image.key) { await image.load() }
    }
}

struct ZoomScreen<Content: View>: View {
    let title: String
    @ViewBuilder let content: Content
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color(.systemBackground))
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
    }
}
