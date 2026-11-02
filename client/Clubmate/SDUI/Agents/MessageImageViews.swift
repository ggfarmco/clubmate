import AppKit
import ImageIO
import SwiftUI

import ClubmateKit

struct MessageImages: View {
    let message: String
    let images: [ImageRef]

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            ForEach(images) { image in
                MessageImageRow(key: ImageExpansionState.key(message: message, path: image.path), image: image)
            }
        }
        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
    }
}

private struct MessageImageRow: View {
    static let maxHeight: CGFloat = 480

    let key: String
    let image: ImageRef

    @State private var loaded: TranscriptImage?
    @State private var hovering = false
    @State private var keyboardFocus = false
    @FocusState private var focused: Bool
    @Environment(\.theme) private var theme

    var body: some View {
        let open = ImageExpansion.shared.state.isOpen(key)
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            row(open)
            if open {
                opened
                    .task(id: TranscriptImages.key(image.path)) { loaded = await TranscriptImages.shared.image(path: image.path) }
            }
        }
    }

    private func row(_ open: Bool) -> some View {
        Button { ImageExpansion.shared.toggle(key) } label: {
            HStack(spacing: theme.spacing.s) {
                Image(systemName: "photo")
                    .font(theme.glyph)
                    .foregroundStyle(theme.muted)
                Text(image.name)
                    .font(theme.ui)
                    .foregroundStyle(hovering ? theme.foreground : theme.text2)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .layoutPriority(1)
                if image.rel != image.name {
                    Text(image.rel)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                DisclosureChevron(expanded: open)
                    .foregroundStyle(theme.muted)
            }
            .padding(.horizontal, theme.spacing.s)
            .padding(.vertical, theme.spacing.xs)
            .contentShape(Rectangle())
            .overlay {
                if keyboardFocus && focused {
                    RoundedRectangle(cornerRadius: theme.radius.m)
                        .stroke(theme.accent, lineWidth: theme.focusRing)
                }
            }
        }
        .buttonStyle(.plain)
        .focusable(interactions: .activate)
        .focusEffectDisabled()
        .focused($focused)
        .onChange(of: focused) { _, now in
            keyboardFocus = now && !ButtonNodeView.mouseEvents.contains(NSApp.currentEvent?.type ?? .keyDown)
        }
        .onKeyPress(keys: [.space, .return]) { _ in
            ImageExpansion.shared.toggle(key)
            return .handled
        }
        .onHover { hovering = $0 }
        .help(image.path)
        .accessibilityLabel(image.name)
        .accessibilityValue(open ? "Expanded" : "Collapsed")
    }

    @ViewBuilder private var opened: some View {
        let shown = TranscriptImages.shared.cached(path: image.path) ?? loaded
        switch shown {
        case .missing:
            Text("Image not found")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .padding(.leading, theme.spacing.s)
        case .image(let cg):
            frame(aspect: Double(cg.width) / Double(max(cg.height, 1))) {
                Image(decorative: cg, scale: 1)
                    .resizable()
                    .scaledToFit()
            }
        case nil:
            if let aspect = image.aspect {
                frame(aspect: aspect) { ProgressView().controlSize(.small) }
            } else {
                ProgressView()
                    .controlSize(.small)
                    .frame(height: theme.rowHeight * 4)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    private func frame(aspect: Double, @ViewBuilder content: () -> some View) -> some View {
        Color.clear
            .aspectRatio(aspect, contentMode: .fit)
            .frame(maxWidth: Self.maxHeight * aspect)
            .overlay { content() }
            .agentCard()
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

@MainActor @Observable final class ImageExpansion {
    static let shared = ImageExpansion()

    private(set) var state = ImageExpansionState()

    func toggle(_ key: String) {
        Motion.disclose { state.toggle(key) }
    }

    func set(_ key: String, open: Bool) {
        if state.isOpen(key) != open { toggle(key) }
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
