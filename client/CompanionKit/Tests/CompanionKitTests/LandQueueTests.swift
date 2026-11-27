import XCTest

import CompanionKit

private let queuesJSON = """
[{"id": "q1", "into": "feat/x", "title": "Landing into feat/x", "glyph": "arrow.down.to.line", "detail": "1 needs you", "detailTone": "warning", "grouped": true, "owner": "own", "needsYou": true,
  "entries": [{"id": "e1", "session": "a", "title": "A", "mode": "branch", "word": "asking", "glyph": "exclamationmark.circle", "tone": "warning", "line": "1st of 2 in line · asking", "why": "waits for your yes", "marks": [{"id": "carried", "label": "Review kept", "help": "Review kept: feat/x moved in other files only"}], "needsYou": true,
               "actions": [{"id": "up", "label": "Move up", "enabled": false, "to": 1}, {"id": "down", "label": "Move down", "enabled": true, "to": 2}], "fileGroups": []},
              {"id": "e2", "session": "b", "title": "B", "mode": "branch", "word": "ready", "glyph": "circle.inset.filled", "line": "2nd of 2 in line · ready", "needsYou": false, "actions": [], "fileGroups": []},
              {"id": "e4", "session": "own", "title": "Edits", "mode": "dirty", "word": "blocked", "glyph": "minus.circle", "line": "3rd of 3 in line · blocked", "needsYou": false, "actions": [], "fileGroups": []}]},
 {"id": "q2", "into": "main", "title": "Landing into main", "glyph": "arrow.down.to.line", "detail": "1 in line", "grouped": false, "needsYou": false,
  "entries": [{"id": "e3", "session": "c", "title": "C", "mode": "branch", "word": "landing", "glyph": "play.circle", "tone": "accent", "line": "1st of 1 in line · landing", "needsYou": false, "actions": [], "fileGroups": []}]}]
"""

final class LandQueueTests: XCTestCase {
    func testOnlyGroupedQueuesTakeSessionsOutOfTheList() throws {
        let queues = try JSONDecoder().decode([LandQueue].self, from: Data(queuesJSON.utf8))
        XCTAssertEqual(queues.inLine, ["a", "b"])
        let head = queues[0].entries[0]
        XCTAssertEqual(head.subtitle, "Review kept · waits for your yes")
        XCTAssertEqual(head.action("down")?.command, "agents.land-queue-move")
        XCTAssertEqual(head.action("up")?.enabled, false)
    }
}
