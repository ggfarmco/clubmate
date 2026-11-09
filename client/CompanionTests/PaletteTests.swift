import SwiftUI
import UIKit
import XCTest

@testable import Companion

final class PaletteTests: XCTestCase {
    func testCaptionInkReachesFourAndAHalfOnEveryCaptionSurface() {
        for style in [UIUserInterfaceStyle.light, .dark] {
            let traits = UITraitCollection(userInterfaceStyle: style)
            let ink = rgb(UIColor(Color.captionInk), traits)
            let surface1 = rgb(UIColor(Color.surface1), traits)
            let surfaces = [
                "surface-1": surface1,
                "surface-2": rgb(UIColor(Color.surface2), traits),
                "grouped": rgb(.systemGroupedBackground, traits),
                "selected row": rgb(UIColor(Color.selectedRow), traits),
                "question card": mix(rgb(UIColor(Color.warning), traits), over: surface1, 0.10),
                "child waiting": mix(rgb(UIColor(Color.warning), traits), over: surface1, 0.18),
            ]
            for (name, surface) in surfaces {
                XCTAssertGreaterThanOrEqual(contrast(ink, surface), 4.5, "\(style == .dark ? "dark" : "light") \(name)")
            }
        }
    }

    private func rgb(_ color: UIColor, _ traits: UITraitCollection) -> [CGFloat] {
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        color.resolvedColor(with: traits).getRed(&r, green: &g, blue: &b, alpha: &a)
        return [r, g, b]
    }

    private func mix(_ top: [CGFloat], over bottom: [CGFloat], _ amount: CGFloat) -> [CGFloat] {
        zip(top, bottom).map { $0 * amount + $1 * (1 - amount) }
    }

    private func contrast(_ a: [CGFloat], _ b: [CGFloat]) -> CGFloat {
        func luminance(_ c: [CGFloat]) -> CGFloat {
            let l = c.map { $0 <= 0.03928 ? $0 / 12.92 : pow(($0 + 0.055) / 1.055, 2.4) }
            return 0.2126 * l[0] + 0.7152 * l[1] + 0.0722 * l[2]
        }
        let (x, y) = (luminance(a), luminance(b))
        return (max(x, y) + 0.05) / (min(x, y) + 0.05)
    }
}
