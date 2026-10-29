import Foundation
import XCTest

import ClubmateKit

final class DiagnosticsTests: XCTestCase {
    func testRedactedSettingsHidesSecretKeys() throws {
        let data = Diagnostics.redactedSettings([
            "sidebarSide": "left",
            "GitHubToken": "abc",
            "apiKey": "k",
            "Password": "p",
            "clientSecret": "s",
            "fontSize": 13,
        ])
        let fields = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(fields["sidebarSide"] as? String, "left")
        XCTAssertEqual(fields["fontSize"] as? Int, 13)
        for key in ["GitHubToken", "apiKey", "Password", "clientSecret"] {
            XCTAssertEqual(fields[key] as? String, "<redacted>", key)
        }
    }

    func testArchiveName() throws {
        let date = Date(timeIntervalSince1970: 1_793_280_605)
        XCTAssertEqual(Diagnostics.archiveName(date, timeZone: try XCTUnwrap(TimeZone(identifier: "UTC"))), "clubmate-diagnostics-20261029-133005.zip")
    }
}
