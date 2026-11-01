import CoreGraphics
import XCTest

@testable import ClubmateDocument

@MainActor
final class MermaidRendererTests: XCTestCase {
    func testRendersCachesAndFailsOnBrokenSource() async throws {
        let renderer = MermaidRenderer()
        let theme = DocumentTheme.mate(dark: true)
        async let flow = renderer.image(source: "graph TD; A-->B", theme: theme, width: 400)
        async let sequence = renderer.image(source: "sequenceDiagram\n  A->>B: hi", theme: theme, width: 400)
        let (first, second) = await (flow, sequence)
        let image = try XCTUnwrap(first)
        XCTAssertNotNil(second)
        XCTAssertGreaterThan(image.width, 0)
        XCTAssertGreaterThan(image.height, 0)
        XCTAssertGreaterThan(Set(Self.pixels(image)).count, 2)
        let broken = await renderer.image(source: "graph TD; A-->", theme: theme, width: 400)
        XCTAssertNil(broken)
        XCTAssertEqual(renderer.renders, 3)
        let again = await renderer.image(source: "graph TD; A-->B", theme: theme, width: 400)
        XCTAssertTrue(again === image)
        XCTAssertTrue(renderer.cached(source: "graph TD; A-->B", theme: theme, width: 400) === image)
        XCTAssertEqual(renderer.renders, 3)
        let huge = "sequenceDiagram\n" + (0..<1500).map { "  A->>B: m\($0)" }.joined(separator: "\n")
        let refused = await renderer.image(source: huge, theme: theme, width: 400)
        XCTAssertNil(refused)
    }

    private static func pixels(_ image: CGImage) -> [UInt32] {
        var data = [UInt32](repeating: 0, count: image.width * image.height)
        let context = CGContext(data: &data, width: image.width, height: image.height, bitsPerComponent: 8, bytesPerRow: image.width * 4, space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        context?.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
        return data
    }
}
