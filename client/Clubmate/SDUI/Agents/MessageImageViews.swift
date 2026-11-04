import AppKit
import ImageIO
import SwiftUI

import ClubmateKit

struct MessageImages: View {
    let message: String
    let images: [ImageRef]

    @Environment(\.theme) private var theme

    var body: some View {
        FlowLayout(spacing: theme.spacing.m) {
            ForEach(images) { image in
                ImageThumbnail(image: image) { ImageViewer.shared.open(message: message, path: image.path) }
            }
        }
        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
    }
}

private struct ImageThumbnail: View {
    let image: ImageRef
    let open: () -> Void

    @State private var loaded: TranscriptImage?
    @State private var hovering = false
    @State private var keyboardFocus = false
    @FocusState private var focused: Bool
    @Environment(\.theme) private var theme

    var body: some View {
        let shown = TranscriptImages.shared.cached(path: image.path) ?? loaded
        let height = theme.rowHeight * 4
        let width = Self.width(aspect: aspect(shown), height: height)
        Button(action: open) {
            VStack(alignment: .leading, spacing: theme.spacing.xs) {
                tile(shown)
                    .frame(width: width, height: height)
                    .clipShape(RoundedRectangle(cornerRadius: theme.radius.m))
                    .overlay {
                        RoundedRectangle(cornerRadius: theme.radius.m)
                            .strokeBorder(keyboardFocus && focused ? theme.accent : hovering ? theme.borderStrong : theme.border, lineWidth: keyboardFocus && focused ? theme.focusRing : 1)
                    }
                Text(image.name)
                    .font(theme.uiSmall)
                    .foregroundStyle(hovering ? theme.text2 : theme.muted)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .frame(width: width, alignment: .leading)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .focusable(interactions: .activate)
        .focusEffectDisabled()
        .focused($focused)
        .onChange(of: focused) { _, now in
            keyboardFocus = now && !ButtonNodeView.mouseEvents.contains(NSApp.currentEvent?.type ?? .keyDown)
        }
        .onKeyPress(keys: [.space, .return]) { _ in
            open()
            return .handled
        }
        .onHover { hovering = $0 }
        .help(image.rel)
        .accessibilityLabel(image.name)
        .accessibilityHint("Opens the image")
        .task(id: TranscriptImages.key(image.path)) { loaded = await TranscriptImages.shared.image(path: image.path) }
    }

    static func width(aspect: Double, height: CGFloat) -> CGFloat {
        height * min(max(aspect, 1), 2)
    }

    private func aspect(_ shown: TranscriptImage?) -> Double {
        if let aspect = image.aspect { return aspect }
        if case .image(let cg) = shown { return Double(cg.width) / Double(max(cg.height, 1)) }
        return 4.0 / 3.0
    }

    @ViewBuilder private func tile(_ shown: TranscriptImage?) -> some View {
        switch shown {
        case .image(let cg):
            Color.clear.overlay(alignment: .top) {
                Image(decorative: cg, scale: 1)
                    .resizable()
                    .scaledToFill()
            }
        case .missing:
            VStack(spacing: theme.spacing.xs) {
                Image(systemName: "photo")
                    .font(theme.glyph)
                Text("Image not found")
                    .font(theme.uiSmall)
                    .multilineTextAlignment(.center)
            }
            .foregroundStyle(theme.muted)
            .padding(theme.spacing.s)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(theme.surface2)
        case nil:
            ProgressView()
                .controlSize(.small)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(theme.surface2)
        }
    }
}

struct ImageViewerSheet: View {
    let path: String
    let rel: String?
    let size: CGSize?

    @State private var loaded: TranscriptImage?

    var body: some View {
        let shown = TranscriptImages.shared.cached(path: path) ?? loaded
        let image: CGImage? = if case .image(let cg) = shown { cg } else { nil }
        let name = path.split(separator: "/").last.map(String.init) ?? path
        ZoomSheet(title: name, detail: rel.flatMap { $0 == name ? nil : $0 }, image: image, missing: shown.isMissing, scale: NSScreen.main?.backingScaleFactor ?? 2, size: size)
            .task(id: TranscriptImages.key(path)) { loaded = await TranscriptImages.shared.image(path: path) }
    }
}

@MainActor @Observable final class ImageViewer {
    static let shared = ImageViewer()

    struct Shown: Identifiable, Equatable {
        let message: String
        let path: String

        var id: String { message + "#" + path }
    }

    var shown: Shown?

    func open(message: String, path: String) {
        shown = Shown(message: message, path: path)
    }

    func binding(message: String) -> Binding<Shown?> {
        Binding(
            get: { self.shown?.message == message ? self.shown : nil },
            set: { if $0 == nil, self.shown?.message == message { self.shown = nil } }
        )
    }
}

private extension Optional where Wrapped == TranscriptImage {
    var isMissing: Bool {
        if case .missing = self { true } else { false }
    }
}

enum TranscriptImage: Sendable {
    case image(CGImage)
    case missing
}

@MainActor final class TranscriptImages {
    static let shared = TranscriptImages()

    private final class Entry {
        let image: TranscriptImage
        init(_ image: TranscriptImage) { self.image = image }
    }

    nonisolated private static let maxPixels = 3200
    private let cache: NSCache<NSString, Entry> = {
        let cache = NSCache<NSString, Entry>()
        cache.totalCostLimit = 256 << 20
        return cache
    }()
    private var running: [String: Task<TranscriptImage, Never>] = [:]

    static func key(_ path: String) -> String {
        let modified = (try? FileManager.default.attributesOfItem(atPath: path)[.modificationDate] as? Date)?.timeIntervalSince1970 ?? 0
        return "\(path)|\(modified)"
    }

    func cached(path: String) -> TranscriptImage? {
        cache.object(forKey: Self.key(path) as NSString)?.image
    }

    func image(path: String) async -> TranscriptImage {
        let key = Self.key(path)
        if let hit = cache.object(forKey: key as NSString)?.image { return hit }
        if let task = running[key] { return await task.value }
        let task = Task.detached(priority: .userInitiated) { Self.decode(path) }
        running[key] = task
        let image = await task.value
        running[key] = nil
        let cost = if case .image(let cg) = image { cg.bytesPerRow * cg.height } else { 0 }
        cache.setObject(Entry(image), forKey: key as NSString, cost: cost)
        return image
    }

    nonisolated private static func decode(_ path: String) -> TranscriptImage {
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixels,
        ]
        guard let source = CGImageSourceCreateWithURL(URL(fileURLWithPath: path) as CFURL, nil),
              let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary)
        else { return .missing }
        return .image(image)
    }
}
