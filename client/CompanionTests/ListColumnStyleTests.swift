import Foundation
import XCTest

final class ListColumnStyleTests: XCTestCase {
    func testPluginsAndSDUIUseTheSharedListStyle() throws {
        let companion = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent().appending(path: "Companion")
        var found: [String] = []
        for folder in ["Plugins", "SDUI"] {
            let files = FileManager.default.enumerator(at: companion.appending(path: folder), includingPropertiesForKeys: nil)
            while let file = files?.nextObject() as? URL {
                guard file.pathExtension == "swift" else { continue }
                for (index, line) in try String(contentsOf: file, encoding: .utf8).split(separator: "\n", omittingEmptySubsequences: false).enumerated()
                where [".listStyle(", ".scrollContentBackground(", ".listRowBackground("].contains(where: line.contains) {
                    found.append("\(folder)/\(file.lastPathComponent):\(index + 1)")
                }
            }
        }
        XCTAssertEqual(found, [], "use columnList(), columnRow(), bareRow() or bareEditor() from App/ListColumn.swift")
    }
}
