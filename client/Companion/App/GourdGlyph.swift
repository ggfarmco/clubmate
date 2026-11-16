import SwiftUI

struct GourdGlyph: View {
    let awake: Bool
    let size: CGFloat

    var body: some View {
        let s = size / 16
        let line = StrokeStyle(lineWidth: 1.5 * s, lineCap: .round, lineJoin: .round)
        ZStack {
            if awake {
                Self.filledBody(s).fill()
                Self.leaf(s).fill()
                Self.leaf(s).stroke(style: line)
            } else {
                Self.body(s).stroke(style: line)
            }
            Self.rimAndBombilla(s).stroke(style: line)
        }
        .frame(width: size, height: size)
    }

    @MainActor private static var images: [String: Image] = [:]

    @MainActor
    static func image(awake: Bool, size: CGFloat) -> Image {
        let key = "\(awake) \(size)"
        if let image = images[key] {
            return image
        }
        let renderer = ImageRenderer(content: GourdGlyph(awake: awake, size: size).foregroundStyle(.black))
        renderer.scale = 3
        guard let cgImage = renderer.cgImage else { return Image(systemName: "circle") }
        let image = Image(cgImage, scale: 3, label: Text(awake ? "Awake" : "Keep Awake")).renderingMode(.template)
        images[key] = image
        return image
    }

    private static func point(_ x: CGFloat, _ y: CGFloat, _ s: CGFloat) -> CGPoint {
        CGPoint(x: x * s, y: y * s)
    }

    private static func body(_ s: CGFloat) -> Path {
        Path { p in
            p.move(to: point(5.7, 5.6, s))
            p.addCurve(to: point(3.2, 10.4, s), control1: point(4.2, 6.7, s), control2: point(3.2, 8.4, s))
            p.addCurve(to: point(8, 15, s), control1: point(3.2, 13, s), control2: point(5.3, 15, s))
            p.addCurve(to: point(12.8, 10.4, s), control1: point(10.7, 15, s), control2: point(12.8, 13, s))
            p.addCurve(to: point(10.3, 5.6, s), control1: point(12.8, 8.4, s), control2: point(11.8, 6.7, s))
        }
    }

    private static func filledBody(_ s: CGFloat) -> Path {
        Path { p in
            p.move(to: point(3.7, 7.2, s))
            p.addCurve(to: point(2.5, 10.4, s), control1: point(3, 8.1, s), control2: point(2.5, 9.2, s))
            p.addCurve(to: point(8, 15.7, s), control1: point(2.5, 13.4, s), control2: point(4.9, 15.7, s))
            p.addCurve(to: point(13.5, 10.4, s), control1: point(11.1, 15.7, s), control2: point(13.5, 13.4, s))
            p.addCurve(to: point(12.3, 7.2, s), control1: point(13.5, 9.2, s), control2: point(13, 8.1, s))
            p.closeSubpath()
        }
    }

    private static func leaf(_ s: CGFloat) -> Path {
        Path { p in
            p.move(to: point(5.5, 4.8, s))
            p.addCurve(to: point(3.1, 3.5, s), control1: point(4.7, 5.1, s), control2: point(3.6, 4.6, s))
            p.addCurve(to: point(5.5, 4.8, s), control1: point(4.2, 3.1, s), control2: point(5.2, 3.7, s))
            p.closeSubpath()
        }
    }

    private static func rimAndBombilla(_ s: CGFloat) -> Path {
        Path { p in
            p.move(to: point(5, 5.6, s))
            p.addLine(to: point(11, 5.6, s))
            p.move(to: point(9.3, 5.6, s))
            p.addLine(to: point(11.6, 1.3, s))
        }
    }
}
