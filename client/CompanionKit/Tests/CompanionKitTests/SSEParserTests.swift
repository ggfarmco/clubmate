import Foundation
import XCTest

import CompanionKit

final class SSEParserTests: XCTestCase {
    func testParsesEventsSplitAcrossChunks() {
        var parser = SSEParser()
        let wire = ": ping\n\nid: 7\nevent: bus\ndata: {\"seq\":7,\r\ndata: \"topic\":\"x\"}\r\n\r\nid: 8\nevent: bus\ndata: two\n\n"
        let bytes = Array(wire.utf8)
        var events: [SSEEvent] = []
        for start in stride(from: 0, to: bytes.count, by: 5) {
            events += parser.feed(Data(bytes[start..<min(start + 5, bytes.count)]))
        }
        XCTAssertEqual(events, [
            SSEEvent(id: "7", event: "bus", data: "{\"seq\":7,\n\"topic\":\"x\"}"),
            SSEEvent(id: "8", event: "bus", data: "two"),
        ])
    }
}
