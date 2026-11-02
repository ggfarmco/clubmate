import Foundation
import Observation

@MainActor
@Observable
public final class DocumentZoom {
    public nonisolated static let range: ClosedRange<Double> = 0.5...3
    public nonisolated static let factor = 1.25

    public private(set) var scale: Double = 1
    public internal(set) var lightbox = false
    @ObservationIgnored weak var controller: DocumentController?

    public init() {}

    public func apply(_ action: ZoomAction) {
        scale = Self.step(scale, action)
        if action == .fit { controller?.resetMagnification() }
    }

    nonisolated static func step(_ scale: Double, _ action: ZoomAction) -> Double {
        let next: Double
        switch action {
        case .fit: return 1
        case .zoomIn: next = scale * factor
        case .zoomOut: next = scale / factor
        }
        return min(range.upperBound, max(range.lowerBound, (next * 100).rounded() / 100))
    }
}
