import Foundation
import XCTest

import ClubmateKit

final class CommandLineToolTests: XCTestCase {
    func testStateFollowsLink() throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let tool = CommandLineTool(flavor: "release", resources: URL(filePath: "/Applications/Clubmate.app/Contents/Resources"), binDir: dir.path(percentEncoded: false))
        XCTAssertEqual(tool.link, dir.appending(path: "cm").path(percentEncoded: false))
        XCTAssertEqual(tool.state(), .missing)
        try FileManager.default.createSymbolicLink(atPath: tool.link, withDestinationPath: tool.target)
        XCTAssertEqual(tool.state(), .installed)
        try FileManager.default.removeItem(atPath: tool.link)
        try FileManager.default.createSymbolicLink(atPath: tool.link, withDestinationPath: "/opt/other/cm")
        XCTAssertEqual(tool.state(), .other("/opt/other/cm"))
    }

    func testInstallScriptQuotesPaths() {
        let tool = CommandLineTool(flavor: "dev", resources: URL(filePath: "/tmp/My \"Dev\" App.app/Contents/Resources"))
        XCTAssertEqual(tool.name, "cm-dev")
        XCTAssertEqual(tool.installScript, #"do shell script "/bin/mkdir -p " & quoted form of "/usr/local/bin" & " && /bin/ln -sfn " & quoted form of "/tmp/My \"Dev\" App.app/Contents/Resources/bin/clubmate" & " " & quoted form of "/usr/local/bin/cm-dev" with administrator privileges"#)
    }
}
