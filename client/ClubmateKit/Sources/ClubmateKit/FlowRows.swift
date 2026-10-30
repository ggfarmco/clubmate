import CoreGraphics
import Foundation

public enum FlowRows {
    public static func origins(sizes: [CGSize], width: CGFloat, spacing: CGFloat) -> [CGPoint] {
        var origins: [CGPoint] = []
        var x: CGFloat = 0
        var y: CGFloat = 0
        var rowHeight: CGFloat = 0
        for size in sizes {
            if x > 0, x + size.width > width {
                x = 0
                y += rowHeight + spacing
                rowHeight = 0
            }
            origins.append(CGPoint(x: x, y: y))
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
        return origins
    }

    public static func size(sizes: [CGSize], width: CGFloat, spacing: CGFloat) -> CGSize {
        let origins = origins(sizes: sizes, width: width, spacing: spacing)
        let frames = zip(origins, sizes).map { CGRect(origin: $0, size: $1) }
        return CGSize(width: frames.map(\.maxX).max() ?? 0, height: frames.map(\.maxY).max() ?? 0)
    }
}
