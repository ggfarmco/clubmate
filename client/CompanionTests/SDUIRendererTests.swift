import XCTest

import CompanionKit

@testable import Companion

@MainActor
final class SDUIRendererTests: XCTestCase {
    private var sent: [(String, String, JSONValue?)] = []
    private var opened: [String] = []

    private func context(_ slot: String) -> SDUIContext {
        SDUIContext(slot: slot, send: { [weak self] in self?.sent.append(($0, $1, $2)) }, open: { [weak self] in self?.opened.append($0) })
    }

    func testGalleryRendersEveryKnownType() throws {
        let url = try XCTUnwrap(Bundle.main.url(forResource: "GalleryFixture", withExtension: "json"))
        let tree = try JSONDecoder().decode(SDUINode.self, from: Data(contentsOf: url))
        var types: Set<String> = []
        func walk(_ node: SDUINode) {
            types.insert(node.type)
            node.children.forEach(walk)
        }
        walk(tree)
        let placeholderOnPhone: Set<String> = ["sheet", "choice", "keys", "impact", "crumb", "repos", "request.connectRepo", "request.land", "request.quiz", "radio"]
        XCTAssertEqual(types.subtracting(MacOnlyCaption.types).subtracting(placeholderOnPhone).filter { SDUIKind(rawValue: $0) == nil }, [])
    }

    func testChatPickerBarIsLeftToThePanelChrome() {
        let text = SDUINode(type: "text", props: ["text": .string("Chat")])
        let hide = SDUINode(type: "button", id: "chat/hide", props: ["label": .string("Hide chat"), "clientAction": .string("hideChat")])
        let back = SDUINode(type: "button", id: "chat/back", props: ["label": .string("All sessions"), "action": .string("chatBack")])
        let bar: ([SDUINode]) -> SDUINode = { SDUINode(type: "stack", props: ["direction": .string("horizontal"), "inset": .string("bar")], children: $0) }

        XCTAssertTrue(StackNode.chromeBar(bar([text, SDUINode(type: "spacer"), hide])))
        XCTAssertFalse(StackNode.chromeBar(bar([back, text, hide])))
    }

    func testChatPanelHeaderTakesItsSubtitleFromThePickerBar() {
        let bar = SDUINode(type: "stack", props: ["direction": .string("horizontal"), "inset": .string("bar")], children: [
            SDUINode(type: "text", props: ["text": .string("Chat"), "style": .string("strong")]),
            SDUINode(type: "text", props: ["text": .string("· pick a session"), "style": .string("muted")]),
            SDUINode(type: "spacer"),
        ])
        let root = SDUINode(type: "keys", children: [SDUINode(type: "stack", props: ["direction": .string("vertical")], children: [bar])])

        XCTAssertEqual(ChatPanel.subtitle(in: root), "· pick a session")
        XCTAssertNil(ChatPanel.subtitle(in: SDUINode(type: "stack", props: ["direction": .string("vertical")])))
    }

    func testPickerRowsDrawCompanionStatusGlyphs() {
        XCTAssertEqual(SessionGlyph(statusIcon: "play.circle", needsYou: false), .running)
        XCTAssertEqual(SessionGlyph(statusIcon: "exclamationmark.circle", needsYou: true), .permission)
        XCTAssertEqual(SessionGlyph(statusIcon: "exclamationmark.circle", needsYou: false), .childWaiting)
        XCTAssertNil(SessionGlyph(statusIcon: "doc.text", needsYou: false))
    }

    func testContextChipTapNamesTheChip() {
        XCTAssertEqual(ContextChip(id: "ctx/page", kind: "page", label: "Roadmap page", removable: true).openValue, .string("ctx/page"))
        XCTAssertEqual(ContextChip(id: nil, kind: "page", label: "Roadmap page", removable: false).openValue, .null)
    }

    func testUnknownTypePlaceholder() {
        let tree = SDUINode(type: "stack", props: ["direction": .string("vertical")], children: [
            SDUINode(type: "text", props: ["text": .string("before")]),
            SDUINode(type: "futureWidget"),
            SDUINode(type: "button", id: "go", props: ["label": .string("Go")]),
        ])
        XCTAssertEqual(tree.children.map { SDUIKind(rawValue: $0.type) }, [.text, nil, .button])
        XCTAssertEqual(SDUIPlaceholder.title(for: "futureWidget"), "futureWidget isn't supported on the phone")
    }

    func testGhostOnlyFlowDrawsNothing() {
        let card: (Bool) -> JSONValue = { .object(["id": .string("when"), "label": .string("When"), "ghost": .bool($0), "outs": .array([])]) }
        XCTAssertTrue(MacOnlyCaption.ghostOnly(SDUINode(type: "flow", id: "f", props: ["cards": .array([card(true)])])))
        XCTAssertFalse(MacOnlyCaption.ghostOnly(SDUINode(type: "flow", id: "f", props: ["cards": .array([card(true), card(false)])])))
    }

    func testSidebarRowTapSendsSelectAndOpensMain() async {
        let list = SDUINode(type: "list", id: "jobs", props: ["items": .array([.object(["id": .string("j1"), "label": .string("Nightly")])])])
        await context("cron.sidebar").select(list, "j1")
        XCTAssertEqual(sent.map(\.0), ["jobs"])
        XCTAssertEqual(sent.map(\.1), ["select"])
        XCTAssertEqual(sent.map(\.2), [.string("j1")])
        XCTAssertEqual(opened, ["j1"])
    }

    func testToggleSendsItsBool() async {
        let toggle = SDUINode(type: "toggle", id: "paused", props: ["value": .bool(false), "action": .string("pause")])
        await context("cron.main").changed(toggle, .toggle(true), in: nil)
        XCTAssertEqual(sent.map(\.1), ["pause"])
        XCTAssertEqual(sent.map(\.2), [.bool(true)])
        XCTAssertEqual(opened, [])
    }

    func testAutosaveFormSendsItsFieldValues() async {
        let toggle = SDUINode(type: "toggle", id: "autostart", props: ["value": .bool(true)])
        let form = SDUINode(type: "form", id: "settings", props: ["action": .string("settings.save"), "autosave": .bool(true)], children: [
            SDUINode(type: "textField", id: "name", props: ["value": .string("clubmate")]),
            SDUINode(type: "stack", props: ["direction": .string("horizontal")], children: [toggle]),
        ])
        let state = SDUIFormState()
        state.update(form, context: context("settings.main"))
        state.values = ["name": .string("clubmate"), "gone": .string("stale")]
        await context("settings.main").changed(toggle, .toggle(false), in: state)
        XCTAssertEqual(sent.map(\.0), ["settings"])
        XCTAssertEqual(sent.map(\.1), ["settings.save"])
        XCTAssertEqual(sent.map(\.2), [.object(["name": .string("clubmate"), "autostart": .bool(false)])])
    }
}
