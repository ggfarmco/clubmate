import Darwin
import Foundation
import XCTest

import ClubmateKit

final class BuildIdentityTests: XCTestCase {
    func testPathsFollowFlavor() {
        let release = ClubmatePaths.resolve(env: ["HOME": "/h"], flavor: "release")
        XCTAssertEqual(release.state.path(percentEncoded: false), "/h/Library/Application Support/clubmate")
        let dev = ClubmatePaths.resolve(env: ["HOME": "/h"], flavor: "dev")
        XCTAssertEqual(dev.state.path(percentEncoded: false), "/h/Library/Application Support/clubmate-dev")
        XCTAssertEqual(dev.runtime.path(percentEncoded: false), "/tmp/clubmate-dev-\(getuid())")
    }

    func testBuildInfoDisplay() {
        XCTAssertEqual(BuildInfo.display(version: "1.2.3", commit: "abcdef0123"), "1.2.3 (abcdef0)")
    }
}
