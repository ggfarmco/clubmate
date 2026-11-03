import CoreGraphics
import Foundation

public struct GraphViewState: Sendable, Equatable {
    public static let zoomRange: ClosedRange<CGFloat> = 0.5...2
    public static let fitFloor: CGFloat = 0.8
    public static let step: CGFloat = 1.25

    private var zooms: [String: CGFloat] = [:]
    private var open: [String: Bool] = [:]

    public init() {}

    public static func fit(width: CGFloat, content: CGFloat) -> CGFloat {
        guard width > 0, content > 0 else { return 1 }
        return max(fitFloor, min(1, width / content))
    }

    public static func clamp(_ zoom: CGFloat) -> CGFloat {
        min(zoomRange.upperBound, max(zoomRange.lowerBound, zoom))
    }

    public func zoom(_ id: String) -> CGFloat? {
        zooms[id]
    }

    public mutating func zoom(_ id: String, steps: Int, from current: CGFloat) {
        zooms[id] = Self.clamp(current * pow(Self.step, CGFloat(steps)))
    }

    public mutating func setZoom(_ id: String, _ zoom: CGFloat) {
        zooms[id] = Self.clamp(zoom)
    }

    public mutating func fit(_ id: String) {
        zooms[id] = nil
    }

    public func historyOpen(_ id: String, default initial: Bool = false) -> Bool {
        open[id] ?? initial
    }

    public mutating func toggleHistory(_ id: String, default initial: Bool = false) {
        open[id] = !historyOpen(id, default: initial)
    }
}
