import CoreGraphics
import SwiftUI

public enum ZoomAction: Equatable, Sendable {
    case fit
    case zoomIn
    case zoomOut
}

public struct ZoomRequest: Equatable, Sendable {
    public let action: ZoomAction
    private let serial: UUID

    public init(_ action: ZoomAction) {
        self.action = action
        serial = UUID()
    }
}

#if os(macOS)
import AppKit

public struct ZoomableImage: NSViewRepresentable {
    private let image: CGImage
    private let scale: CGFloat
    private let request: ZoomRequest?

    public init(image: CGImage, scale: CGFloat, request: ZoomRequest? = nil) {
        self.image = image
        self.scale = scale
        self.request = request
    }

    public func makeNSView(context: Context) -> ZoomScrollView {
        ZoomScrollView()
    }

    public func updateNSView(_ view: ZoomScrollView, context: Context) {
        view.show(image, scale: scale)
        if let request, request != view.handled {
            view.handled = request
            view.apply(request.action)
        }
    }
}

public final class ZoomScrollView: NSScrollView {
    private let imageView = NSImageView()
    private var shown: CGImage?
    private var fitted = false
    fileprivate var handled: ZoomRequest?

    override init(frame: NSRect) {
        super.init(frame: frame)
        contentView = CenteringClipView()
        drawsBackground = false
        contentView.drawsBackground = false
        hasVerticalScroller = true
        hasHorizontalScroller = true
        autohidesScrollers = true
        allowsMagnification = true
        maxMagnification = 4
        imageView.imageScaling = .scaleProportionallyUpOrDown
        documentView = imageView
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    fileprivate func show(_ image: CGImage, scale: CGFloat) {
        guard image !== shown else { return }
        shown = image
        let size = NSSize(width: CGFloat(image.width) / scale, height: CGFloat(image.height) / scale)
        imageView.image = NSImage(cgImage: image, size: size)
        imageView.frame = NSRect(origin: .zero, size: size)
        fitted = false
        needsLayout = true
    }

    override public func layout() {
        super.layout()
        if !fitted, contentSize.width > 0 {
            fitted = true
            apply(.fit)
        }
    }

    fileprivate func apply(_ action: ZoomAction) {
        let fit = fitMagnification
        minMagnification = min(fit, 1) / 2
        switch action {
        case .fit: magnification = fit
        case .zoomIn: setMagnification(min(maxMagnification, magnification * 1.5), centeredAt: center)
        case .zoomOut: setMagnification(max(minMagnification, magnification / 1.5), centeredAt: center)
        }
    }

    private var fitMagnification: CGFloat {
        let size = imageView.frame.size
        guard size.width > 0, size.height > 0 else { return 1 }
        return min(1, contentSize.width / size.width, contentSize.height / size.height)
    }

    private var center: NSPoint {
        NSPoint(x: contentView.bounds.midX, y: contentView.bounds.midY)
    }
}

private final class CenteringClipView: NSClipView {
    override func constrainBoundsRect(_ proposedBounds: NSRect) -> NSRect {
        var rect = super.constrainBoundsRect(proposedBounds)
        guard let document = documentView else { return rect }
        if rect.width > document.frame.width { rect.origin.x = (document.frame.width - rect.width) / 2 }
        if rect.height > document.frame.height { rect.origin.y = (document.frame.height - rect.height) / 2 }
        return rect
    }
}
#else
import UIKit

public struct ZoomableImage: UIViewRepresentable {
    private let image: CGImage
    private let scale: CGFloat
    private let request: ZoomRequest?

    public init(image: CGImage, scale: CGFloat, request: ZoomRequest? = nil) {
        self.image = image
        self.scale = scale
        self.request = request
    }

    public func makeUIView(context: Context) -> ZoomScrollView {
        ZoomScrollView()
    }

    public func updateUIView(_ view: ZoomScrollView, context: Context) {
        view.show(image, scale: scale)
        if let request, request != view.handled {
            view.handled = request
            view.apply(request.action)
        }
    }
}

public final class ZoomScrollView: UIScrollView, UIScrollViewDelegate {
    private let imageView = UIImageView()
    private var shown: CGImage?
    private var fittedWidth: CGFloat = 0
    fileprivate var handled: ZoomRequest?

    override init(frame: CGRect) {
        super.init(frame: frame)
        delegate = self
        maximumZoomScale = 4
        showsVerticalScrollIndicator = false
        showsHorizontalScrollIndicator = false
        contentInsetAdjustmentBehavior = .never
        addSubview(imageView)
        let tap = UITapGestureRecognizer(target: self, action: #selector(toggle(_:)))
        tap.numberOfTapsRequired = 2
        addGestureRecognizer(tap)
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    fileprivate func show(_ image: CGImage, scale: CGFloat) {
        guard image !== shown else { return }
        shown = image
        zoomScale = 1
        imageView.image = UIImage(cgImage: image, scale: scale, orientation: .up)
        imageView.frame = CGRect(x: 0, y: 0, width: CGFloat(image.width) / scale, height: CGFloat(image.height) / scale)
        contentSize = imageView.frame.size
        fittedWidth = 0
        setNeedsLayout()
    }

    override public func layoutSubviews() {
        super.layoutSubviews()
        if bounds.width > 0, bounds.width != fittedWidth {
            fittedWidth = bounds.width
            apply(.fit)
        }
        center()
    }

    fileprivate func apply(_ action: ZoomAction) {
        let fit = fitScale
        minimumZoomScale = min(fit, 1)
        switch action {
        case .fit: setZoomScale(fit, animated: false)
        case .zoomIn: setZoomScale(min(maximumZoomScale, zoomScale * 1.5), animated: true)
        case .zoomOut: setZoomScale(max(minimumZoomScale, zoomScale / 1.5), animated: true)
        }
    }

    private var fitScale: CGFloat {
        let size = imageView.bounds.size
        guard size.width > 0, size.height > 0 else { return 1 }
        return min(1, bounds.width / size.width, bounds.height / size.height)
    }

    @objc private func toggle(_ tap: UITapGestureRecognizer) {
        if zoomScale > fitScale * 1.01 {
            setZoomScale(fitScale, animated: true)
        } else {
            let point = tap.location(in: imageView)
            let size = CGSize(width: bounds.width / 2.5, height: bounds.height / 2.5)
            zoom(to: CGRect(x: point.x - size.width / 2, y: point.y - size.height / 2, width: size.width, height: size.height), animated: true)
        }
    }

    private func center() {
        let x = max(0, (bounds.width - contentSize.width) / 2)
        let y = max(0, (bounds.height - contentSize.height) / 2)
        contentInset = UIEdgeInsets(top: y, left: x, bottom: y, right: x)
    }

    public func viewForZooming(in scrollView: UIScrollView) -> UIView? {
        imageView
    }

    public func scrollViewDidZoom(_ scrollView: UIScrollView) {
        center()
    }
}
#endif
