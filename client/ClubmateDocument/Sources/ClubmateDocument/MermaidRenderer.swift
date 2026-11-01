import CoreGraphics
import Foundation
import WebKit

@MainActor
public final class MermaidRenderer {
    public static let shared = MermaidRenderer()

    private struct Key: Hashable {
        let source: String
        let theme: DocumentTheme
        let width: Int
        let scale: Int
    }

    private let limit = 64
    private let maxSide: CGFloat = 16384
    private let maxPixels: CGFloat = 24_000_000
    private var cache: [Key: CGImage?] = [:]
    private var order: [Key] = []
    private var running: [Key: Task<CGImage?, Never>] = [:]
    private var queue: Task<Void, Never>?
    private var page: DocumentController?
    private var rev = 0
    private var pixelRatio: CGFloat?
    private(set) var renders = 0

    public init() {}

    public func cached(source: String, theme: DocumentTheme, width: CGFloat, scale: CGFloat = 2) -> CGImage? {
        let key = Self.key(source, theme, width, scale)
        guard let hit = cache[key] else { return nil }
        touch(key)
        return hit
    }

    public func image(source: String, theme: DocumentTheme, width: CGFloat, scale: CGFloat = 2) async -> CGImage? {
        let key = Self.key(source, theme, width, scale)
        if let hit = cache[key] {
            touch(key)
            return hit
        }
        if let task = running[key] { return await task.value }
        let previous = queue
        let task = Task {
            await previous?.value
            let image = await render(key)
            store(key, image)
            running[key] = nil
            return image
        }
        running[key] = task
        queue = Task { _ = await task.value }
        return await task.value
    }

    private static func key(_ source: String, _ theme: DocumentTheme, _ width: CGFloat, _ scale: CGFloat) -> Key {
        Key(source: source, theme: theme, width: max(80, Int((width / 8).rounded(.down)) * 8), scale: max(1, Int(scale.rounded())))
    }

    private func touch(_ key: Key) {
        order.removeAll { $0 == key }
        order.append(key)
    }

    private func store(_ key: Key, _ image: CGImage?) {
        cache[key] = .some(image)
        touch(key)
        while order.count > limit {
            cache[order.removeFirst()] = nil
        }
    }

    private func render(_ key: Key) async -> CGImage? {
        renders += 1
        let page = self.page ?? DocumentController()
        self.page = page
        page.webView.frame = CGRect(x: 0, y: 0, width: CGFloat(key.width), height: 4096)
        rev += 1
        page.show(DocumentContent(path: "mermaid", html: "<pre class=\"mermaid\">\(Self.escape(key.source))</pre>", rev: rev), theme: key.theme, size: .fit)
        await page.settled()
        let script = "const svg = document.querySelector('.mermaid-diagram svg'); if (!svg) return null; const r = svg.getBoundingClientRect(); return [r.x, r.y + window.scrollY, r.width, r.height]"
        guard let box = try? await page.webView.callAsyncJavaScript(script, contentWorld: .page) as? [Double], box.count == 4, box[2] > 0, box[3] > 0 else { return nil }
        let rect = CGRect(x: floor(box[0]), y: floor(box[1]), width: ceil(box[2]), height: ceil(box[3]))
        let scale = min(CGFloat(key.scale), maxSide / rect.width, maxSide / rect.height, (maxPixels / (rect.width * rect.height)).squareRoot())
        guard scale >= 1, rect.maxY <= maxSide else { return nil }
        if rect.maxY > page.webView.frame.height {
            page.webView.frame.size.height = rect.maxY
        }
        if let ratio = pixelRatio {
            return await snapshot(page.webView, rect, width: rect.width * scale / ratio)
        }
        guard let probe = await snapshot(page.webView, rect, width: rect.width) else { return nil }
        let ratio = CGFloat(probe.width) / rect.width
        pixelRatio = ratio
        return abs(ratio - scale) < 0.01 ? probe : await snapshot(page.webView, rect, width: rect.width * scale / ratio)
    }

    private func snapshot(_ webView: WKWebView, _ rect: CGRect, width: CGFloat) async -> CGImage? {
        let configuration = WKSnapshotConfiguration()
        configuration.rect = rect
        configuration.snapshotWidth = NSNumber(value: Double(width))
        configuration.afterScreenUpdates = true
        guard let image = try? await webView.takeSnapshot(configuration: configuration) else { return nil }
        #if os(macOS)
        return image.cgImage(forProposedRect: nil, context: nil, hints: nil)
        #else
        return image.cgImage
        #endif
    }

    private static func escape(_ source: String) -> String {
        source
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
    }
}
