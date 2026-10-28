import Foundation
import XCTest

import ClubmateKit

@MainActor
final class OpenProjectsTests: XCTestCase {
    func testOpenProjectsSurviveQuit() throws {
        let name = "OpenProjectsTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }
        let open = OpenProjects(defaults: defaults)
        open.opened("a")
        open.opened("b")
        open.opened("c")
        open.closed("c")
        open.terminating()
        open.closed("a")
        open.closed("b")
        XCTAssertEqual(OpenProjects(defaults: defaults).ids, ["a", "b"])
    }
}
