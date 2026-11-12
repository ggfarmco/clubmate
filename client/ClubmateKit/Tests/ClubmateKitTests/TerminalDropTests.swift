import XCTest

import ClubmateKit

final class TerminalDropTests: XCTestCase {
    func testQuote() {
        let cases: [(String, String)] = [
            ("/tmp/notes.txt", "/tmp/notes.txt"),
            ("/tmp/Заметки-1.md", "'/tmp/Заметки-1.md'"),
            ("/tmp/x\u{0D4E};rm", "'/tmp/x\u{0D4E};rm'"),
            ("/tmp/my file.txt", "'/tmp/my file.txt'"),
            ("/tmp/$HOME;rm *", "'/tmp/$HOME;rm *'"),
            ("/tmp/it's", #"'/tmp/it'"'"'s'"#),
            ("", "''"),
        ]
        for (word, quoted) in cases {
            XCTAssertEqual(TerminalDrop.quote(word), quoted, word)
        }
    }

    func testTextQuotesFilesAndKeepsText() {
        XCTAssertEqual(TerminalDrop.text([.file("/a b"), .file("/c"), .text("it's $x")]), "'/a b' /c it's $x")
        XCTAssertNil(TerminalDrop.text([]))
    }
}
