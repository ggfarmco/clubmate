import SwiftUI
import UIKit

import CompanionKit

extension EnvironmentValues {
    @Entry var imageStore: AgentsStore? = nil
    @Entry var imageSession = ""
    @Entry var imageMaxHeight = 480.0
}

@MainActor
private enum DecodedImages {
    static let cache = NSCache<NSString, UIImage>()

    static func image(_ data: Data, key: String) -> UIImage? {
        if let cached = cache.object(forKey: key as NSString) { return cached }
        guard let image = UIImage(data: data) else { return nil }
        cache.setObject(image, forKey: key as NSString)
        return image
    }
}

struct TranscriptImageRow: View {
    let ref: ImageRef
    let entry: String
    let session: String
    let store: AgentsStore
    @Environment(\.imageMaxHeight) private var maxHeight
    @State private var width = 0.0

    var body: some View {
        let open = store.isOpen(image: ref.path, entry: entry, in: session)
        VStack(alignment: .leading, spacing: 0) {
            Button {
                Motion.disclose { store.toggle(image: ref.path, entry: entry, in: session) }
            } label: {
                header(open: open)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(ref.name)
            .accessibilityValue(open ? "Open" : "Folded")
            if open {
                content
                    .task(id: session + "|" + entry + "|" + ref.path) { await store.loadImage(ref.path, entry: entry, in: session) }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.surface1)
        .clipShape(.rect(cornerRadius: 20))
        .onGeometryChange(for: Double.self) { $0.size.width } action: { width = $0 }
    }

    private func header(open: Bool) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "photo")
                .foregroundStyle(.secondary)
                .frame(width: 20)
            VStack(alignment: .leading, spacing: 2) {
                Text(ref.name)
                    .font(.callout)
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if ref.rel != ref.name {
                    Text(ref.rel)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .layoutPriority(1)
            Image(systemName: "chevron.right")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.tertiary)
                .rotationEffect(.degrees(open ? 90 : 0))
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(minHeight: 44)
        .contentShape(.rect)
    }

    @ViewBuilder
    private var content: some View {
        let key = session + "|" + entry + "|" + ref.path
        switch store.image(ref.path, entry: entry, in: session) {
        case .loaded(let data):
            if let image = DecodedImages.image(data, key: key) {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFit()
                    .frame(maxWidth: .infinity)
                    .frame(height: height(ref.aspect ?? Double(image.size.width / max(image.size.height, 1))))
                    .accessibilityLabel(ref.name)
            } else {
                missing
            }
        case .missing:
            missing
        case .loading, nil:
            ProgressView()
                .frame(maxWidth: .infinity)
                .frame(height: height(ref.aspect ?? 4.0 / 3.0))
        }
    }

    private var missing: some View {
        Text("Image not found")
            .font(.footnote)
            .foregroundStyle(.secondary)
            .padding(.horizontal, 14)
            .padding(.bottom, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func height(_ aspect: Double) -> Double {
        guard width > 0, aspect > 0 else { return min(240, maxHeight) }
        return min(width / aspect, maxHeight)
    }
}
