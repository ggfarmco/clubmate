import AppKit
import CoreImage
import IOSurface
import WebKit

@MainActor
enum WindowImage {
    private static let images = CIContext()

    static func pages(_ window: NSWindow) async -> [(NSView, CGImage)] {
        guard let root = window.contentView else { return [] }
        var pages: [(NSView, CGImage)] = []
        for view in root.descendants(WKWebView.self) where !view.isHiddenOrHasHiddenAncestor {
            if let image = try? await view.takeSnapshot(configuration: nil), let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) {
                pages.append((view, cgImage))
            }
        }
        return pages
    }

    static func bitmap(_ window: NSWindow, titlebar: Bool = false, pages: [(NSView, CGImage)] = []) -> NSBitmapImageRep? {
        guard let root = titlebar ? window.contentView?.superview ?? window.contentView : window.contentView, root.bounds.width > 0, root.bounds.height > 0,
              let rep = root.bitmapImageRepForCachingDisplay(in: root.bounds) else { return nil }
        root.cacheDisplay(in: root.bounds, to: rep)
        if window.sheetParent != nil { fillBackground(rep, of: window) }
        let surfaces = terminals(in: root)
        guard !surfaces.isEmpty || !pages.isEmpty, let context = NSGraphicsContext(bitmapImageRep: rep) else { return rep }
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = context
        func rect(_ view: NSView) -> NSRect {
            var rect = root.convert(view.bounds, from: view)
            if root.isFlipped { rect.origin.y = root.bounds.height - rect.maxY }
            return rect
        }
        for (view, image) in pages {
            context.cgContext.draw(image, in: rect(view))
        }
        context.cgContext.setBlendMode(.destinationOver)
        for view in surfaces {
            if let image = view.layer.flatMap(surfaceImage) {
                context.cgContext.draw(image, in: rect(view))
            }
        }
        NSGraphicsContext.restoreGraphicsState()
        return rep
    }

    static func png(_ window: NSWindow, titlebar: Bool = false, pages: [(NSView, CGImage)] = []) -> Data? {
        bitmap(window, titlebar: titlebar, pages: pages)?.representation(using: .png, properties: [:])
    }

    private static func fillBackground(_ rep: NSBitmapImageRep, of window: NSWindow) {
        guard let context = NSGraphicsContext(bitmapImageRep: rep) else { return }
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = context
        window.effectiveAppearance.performAsCurrentDrawingAppearance {
            NSColor.windowBackgroundColor.setFill()
            NSRect(origin: .zero, size: rep.size).fill(using: .destinationOver)
        }
        NSGraphicsContext.restoreGraphicsState()
    }

    static func terminals(in view: NSView) -> [TerminalSurfaceView] {
        guard !view.isHidden else { return [] }
        if let surface = view as? TerminalSurfaceView { return [surface] }
        return view.subviews.flatMap(terminals)
    }

    private static func surfaceImage(_ layer: CALayer) -> CGImage? {
        if let contents = layer.contents, CFGetTypeID(contents as CFTypeRef) == IOSurfaceGetTypeID() {
            let image = CIImage(ioSurface: contents as! IOSurfaceRef)
            return images.createCGImage(image, from: image.extent)
        }
        if let contents = layer.contents, CFGetTypeID(contents as CFTypeRef) == CGImage.typeID {
            return (contents as! CGImage)
        }
        return layer.sublayers?.lazy.compactMap(surfaceImage).first
    }
}
