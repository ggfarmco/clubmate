import XCTest

import ClubmateKit

@MainActor
final class AgentsKeysTests: XCTestCase {
    private let both = AgentsLayout(composers: [.main, .aside], transcripts: [.main, .aside])

    func testEscapeStepsOutAndCtrlBracketIsTheSame() {
        XCTAssertEqual(AgentsChord.resolve(keyCode: 53, command: false, control: false, option: false, shift: false), .stepOut)
        XCTAssertEqual(AgentsChord.resolve(keyCode: 33, command: false, control: true, option: false, shift: false), .stepOut)
        XCTAssertEqual(AgentsChord.resolve(keyCode: 47, command: true, control: false, option: false, shift: false), .stop)
        XCTAssertNil(AgentsChord.resolve(keyCode: 53, command: false, control: false, option: false, shift: true))
    }

    func testOnlyTheAgentsSectionStepsOut() {
        XCTAssertTrue(AgentsKeys.steps(section: "agents"))
        XCTAssertFalse(AgentsKeys.steps(section: "agents.graphs"))
        XCTAssertFalse(AgentsKeys.steps(section: "agents.gates"))
    }

    func testTheLadderGoesComposerTranscriptTreeAndBackToTheLastComposer() {
        var layout = both
        layout.lastComposer = .aside
        XCTAssertEqual(AgentsKeys.move(.stepOut, from: .composer(.aside), in: layout), .focus(.transcript(.aside)))
        XCTAssertEqual(AgentsKeys.move(.stepOut, from: .transcript(.aside), in: layout), .focus(.transcript(.main)))
        XCTAssertEqual(AgentsKeys.move(.stepOut, from: .transcript(.main), in: layout), .focus(.tree))
        XCTAssertEqual(AgentsKeys.move(.stepOut, from: .tree, in: layout), .focus(.composer(.aside)))
        XCTAssertEqual(AgentsKeys.move(.stepOut, from: nil, in: layout), .focus(.tree))
    }

    func testTheLadderSkipsAnEmptyTranscriptAndSaysWhyItStays() {
        XCTAssertEqual(AgentsKeys.move(.stepOut, from: .composer(.main), in: AgentsLayout(composers: [.main])), .focus(.tree))
        XCTAssertEqual(AgentsKeys.move(.stepOut, from: .tree, in: AgentsLayout()), .note(AgentsKeys.noSession))
    }

    func testOptionCommandArrowsGoStraightToAPane() {
        XCTAssertEqual(AgentsKeys.move(.tree, from: .composer(.main), in: both), .focus(.tree))
        XCTAssertEqual(AgentsKeys.move(.right, from: .tree, in: both), .focus(.composer(.main)))
        XCTAssertEqual(AgentsKeys.move(.right, from: .transcript(.main), in: both), .focus(.composer(.aside)))
        XCTAssertEqual(AgentsKeys.move(.right, from: .composer(.aside), in: both), .stay)
        XCTAssertEqual(AgentsKeys.move(.transcript, from: .composer(.aside), in: both), .focus(.transcript(.aside)))
        XCTAssertEqual(AgentsKeys.move(.composer, from: .transcript(.main), in: both), .focus(.composer(.main)))
    }

    func testColumnKeysFollowTheSidebarSide() {
        XCTAssertEqual(AgentsKeys.column(-1, from: .transcript(.main), in: both), .focus(.tree))
        XCTAssertEqual(AgentsKeys.column(1, from: .transcript(.main), in: both), .focus(.transcript(.aside)))
        XCTAssertEqual(AgentsKeys.column(1, from: .transcript(.aside), in: both), .stay)
        var right = both
        right.side = .right
        XCTAssertEqual(AgentsKeys.column(1, from: .transcript(.aside), in: right), .focus(.tree))
        XCTAssertEqual(AgentsKeys.column(-1, from: .transcript(.main), in: right), .stay)
    }

    func testTranscriptKeys() {
        XCTAssertEqual(TranscriptKey.resolve(keyCode: 34, characters: "i", command: false, control: false, option: false, shift: false), .type)
        XCTAssertEqual(TranscriptKey.resolve(keyCode: 49, characters: " ", command: false, control: false, option: false, shift: true), .page(-1))
        XCTAssertEqual(TranscriptKey.resolve(keyCode: 4, characters: "h", command: false, control: true, option: false, shift: false), .column(-1))
        XCTAssertNil(TranscriptKey.resolve(keyCode: 0, characters: "a", command: true, control: false, option: false, shift: false))
        XCTAssertEqual(TranscriptKey.resolve(keyCode: 38, characters: "j", command: false, control: false, option: false, shift: false), .step(.move(1)))
        XCTAssertEqual(TranscriptKey.resolve(keyCode: 123, characters: nil, command: false, control: false, option: false, shift: false), .step(.fold))
        XCTAssertEqual(TranscriptKey.resolve(keyCode: 36, characters: "\r", command: false, control: false, option: false, shift: false), .step(.toggle))
        XCTAssertEqual(TranscriptKey.resolve(keyCode: 5, characters: "G", command: false, control: false, option: false, shift: true), .step(.last))
        XCTAssertEqual(TranscriptKey.resolve(keyCode: 5, characters: "g", command: false, control: false, option: false, shift: false), .leader)
        XCTAssertEqual(TranscriptKey.resolve(keyCode: 44, characters: "?", command: false, control: false, option: false, shift: true), .help)
    }

    func testTranscriptChordsReuseTheTreeChords() {
        XCTAssertEqual(TranscriptChord.resolve(5, shift: false), .first)
        XCTAssertEqual(TranscriptChord.resolve(3, shift: false), .file)
        XCTAssertEqual(TranscriptChord.resolve(19, shift: false), .go(.jump(2)))
        XCTAssertEqual(TranscriptChord.resolve(45, shift: true), .go(.needs(-1)))
        XCTAssertEqual(TranscriptChord.resolve(8, shift: false), .go(.focusField))
        XCTAssertTrue(TranscriptChord.whichKey(file: true).contains("f file"))
        XCTAssertFalse(TranscriptChord.whichKey(file: false).contains("f file"))
        XCTAssertEqual(AgentsChord.resolve(keyCode: 44, command: true, control: false, option: false, shift: false), .keys)
    }

    func testCommandPeriodStopsOnlyInTheColumnWithFocus() {
        XCTAssertTrue(AgentsKeys.stopsHere(button: "agents.main", focused: "agents.sidebar"))
        XCTAssertFalse(AgentsKeys.stopsHere(button: "agents.main", focused: "agents.aside"))
        XCTAssertTrue(AgentsKeys.stopsHere(button: "agents.aside", focused: "agents.aside"))
        XCTAssertTrue(AgentsKeys.stopsHere(button: "agents.graphs.main", focused: nil))
    }

    func testLayoutReadsComposersAndTranscriptsWithMessages() {
        let main = SDUINode(type: "stack", children: [SDUINode(type: "transcript", children: [SDUINode(type: "message")]), SDUINode(type: "composer")])
        let aside = SDUINode(type: "stack", children: [SDUINode(type: "transcript"), SDUINode(type: "composer")])
        let layout = AgentsLayout(main: main, aside: aside, lastComposer: "agents.main", side: .left)
        XCTAssertEqual(layout, AgentsLayout(composers: [.main, .aside], transcripts: [.main], lastComposer: .main))
    }

    func testStopHintShowsTwentyTimes() throws {
        let name = "AgentsKeysTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }

        let shown = (0..<21).map { _ in StopHint(defaults: defaults).show() }

        XCTAssertEqual(shown, Array(repeating: true, count: 20) + [false])
    }
}
