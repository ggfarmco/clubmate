import AppKit
import CoreImage
import IOSurface

@MainActor
enum WindowImage {
    private static let images = CIContext()

    static func bitmap(_ window: NSWindow, titlebar: Bool = false) -> NSBitmapImageRep? {
        guard let root = titlebar ? window.contentView?.superview ?? window.contentView : window.contentView, root.bounds.width > 0, root.bounds.height > 0,
              let rep = root.bitmapImageRepForCachingDisplay(in: root.bounds) else { return nil }
        root.cacheDisplay(in: root.bounds, to: rep)
        let surfaces = terminals(in: root)
        guard !surfaces.isEmpty, let context = NSGraphicsContext(bitmapImageRep: rep) else { return rep }
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = context
        for view in surfaces {
            var rect = root.convert(view.bounds, from: view)
            if root.isFlipped { rect.origin.y = root.bounds.height - rect.maxY }
            if let image = view.layer.flatMap(surfaceImage) {
                context.cgContext.draw(image, in: rect)
            }
        }
        NSGraphicsContext.restoreGraphicsState()
        return rep
    }

    static func png(_ window: NSWindow, titlebar: Bool = false) -> Data? {
        bitmap(window, titlebar: titlebar)?.representation(using: .png, properties: [:])
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
