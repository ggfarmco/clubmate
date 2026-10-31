import Foundation
import XCTest

import ClubmateKit

final class LineBufferTests: XCTestCase {
    func testSplitsLinesAcrossChunkBoundaries() {
        let big = Data(repeating: 0x61, count: 700 * 1024)
        let stream = big + Data("\n\nshort\nx".utf8) + Data("y\n".utf8)
        for size in [1, 7, 64 * 1024, stream.count] {
            XCTAssertEqual(Self.split(stream, chunk: size), [big, Data("short".utf8), Data("xy".utf8)], "chunk \(size)")
        }
    }

    func testKeepsIncompleteTail() {
        XCTAssertEqual(Self.split(Data("one\ntwo".utf8), chunk: 3), [Data("one".utf8)])
    }

    func testLargeLinesThroughput() {
        let line = Data(repeating: 0x7B, count: 750 * 1024) + Data([0x0A])
        let stream = Data((0..<20).flatMap { _ in line })
        measure {
            XCTAssertEqual(Self.split(stream, chunk: 64 * 1024).count, 20)
        }
    }

    private static func split(_ stream: Data, chunk: Int) -> [Data] {
        var buffer = LineBuffer()
        var lines: [Data] = []
        stream.withUnsafeBytes { raw in
            var offset = 0
            while offset < raw.count {
                let end = min(offset + chunk, raw.count)
                buffer.feed(UnsafeRawBufferPointer(rebasing: raw[offset..<end])) { lines.append($0) }
                offset = end
            }
        }
        return lines
    }
}
