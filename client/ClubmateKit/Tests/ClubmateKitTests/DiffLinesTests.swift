import XCTest

import ClubmateKit

final class DiffLinesTests: XCTestCase {
    func testDiffLineKinds() {
        let text = "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n ctx\n\n"
        XCTAssertEqual(DiffLine.parse(text), [
            DiffLine(text: "--- a/x", kind: .context),
            DiffLine(text: "+++ b/x", kind: .context),
            DiffLine(text: "@@ -1 +1 @@", kind: .hunk),
            DiffLine(text: "-old", kind: .removed),
            DiffLine(text: "+new", kind: .added),
            DiffLine(text: " ctx", kind: .context),
            DiffLine(text: "", kind: .context),
        ])
    }
}
