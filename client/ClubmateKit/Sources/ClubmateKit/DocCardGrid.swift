import CoreGraphics
import Foundation

public struct DocCardGrid: Equatable {
    public let columns: Int
    public let width: CGFloat
    public let cardWidth: CGFloat

    public init(count: Int, proposed: CGFloat?, spacing: CGFloat, minWidth: CGFloat, maxWidth: CGFloat) {
        let width = proposed.flatMap { $0.isFinite ? $0 : nil } ?? maxWidth * CGFloat(count)
        let fit = Int(max(1, min(CGFloat(max(count, 2)), (width + spacing) / (minWidth + spacing))))
        self.width = width
        guard fit > 1 else {
            columns = 1
            cardWidth = width
            return
        }
        columns = max(1, min(count, fit))
        cardWidth = min(maxWidth, (width - spacing * CGFloat(columns - 1)) / CGFloat(columns))
    }
}
