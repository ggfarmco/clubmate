import UIKit
import XCTest

@MainActor
final class IPadUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"
    private static let task = "!! Retry the charge step on 502"

    override func setUpWithError() throws {
        continueAfterFailure = false
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "the columns and keys are iPad only")
        XCUIDevice.shared.orientation = .landscapeLeft
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
    }

    func testTabsAtTheBottomOfTheListSwitchTabs() {
        let app = launch(open: "tab:agents")
        let row = sessionRow(app)
        let backlog = tab(app, "Backlog")
        XCTAssertTrue(backlog.waitForExistence(timeout: 10))
        XCTAssertFalse(tab(app, "Search").exists)
        XCTAssertGreaterThan(backlog.frame.minY, app.windows.firstMatch.frame.height * 0.85)
        XCTAssertLessThan(backlog.frame.maxX, 360)
        backlog.tap()
        XCTAssertTrue(shown(app.staticTexts[Self.task]))
        XCTAssertTrue(backlog.isSelected)
        XCTAssertTrue(hidden(row))
        tab(app, "Agents").tap()
        XCTAssertTrue(shown(row))
    }

    func testNoTabBarNoWorkspaceTabNoListTitle() {
        let app = launch(open: "tab:agents")
        _ = sessionRow(app)
        XCTAssertTrue(hidden(app.tabBars.firstMatch))
        XCTAssertFalse(app.buttons["Mac Studio"].exists)
        XCTAssertFalse(app.navigationBars.staticTexts["Agents"].exists)
        XCTAssertLessThan(app.buttons["workspace.capsule"].frame.minY, 40)
    }

    func testSidebarButtonAndKeyToggleTheList() {
        let app = launch(open: "agents/agt_0000000000000004")
        let row = sessionRow(app)
        XCTAssertTrue(shown(row))
        toggle(app).tap()
        XCTAssertTrue(listHidden(app))
        XCTAssertLessThan(toggle(app).frame.minX, 60)
        toggle(app).tap()
        XCTAssertTrue(shown(row))
        XCTAssertTrue(press(app, "s", [.control, .command], until: listHidden(app)))
        XCTAssertTrue(press(app, "s", [.control, .command], until: shown(row)))
    }

    func testCapsuleSwitchesTheWorkspaceWithTheListHidden() {
        let app = launch(open: "tab:agents")
        XCTAssertTrue(shown(sessionRow(app)))
        XCTAssertTrue(wait { self.capsules(app).count == 1 })
        toggle(app).tap()
        XCTAssertTrue(listHidden(app))
        XCTAssertTrue(wait(timeout: 10) { self.capsules(app).count == 1 }, "no capsule in the detail column with the list hidden")
        let capsule = capsules(app)[0]
        XCTAssertLessThan(capsule.frame.minX, toggle(app).frame.maxX + 80)
        capsule.tap()
        let item = app.buttons.matching(NSPredicate(format: "identifier != 'workspace.capsule' AND (label == 'clubmate' OR label BEGINSWITH 'clubmate, ')")).element(boundBy: 0)
        XCTAssertTrue(item.waitForExistence(timeout: 10))
        item.tap()
        XCTAssertTrue(wait(timeout: 10) { self.capsules(app).first?.label.contains("clubmate") == true })
        toggle(app).tap()
        let listCell = app.cells.containing(NSPredicate(format: "identifier BEGINSWITH 'row.'")).firstMatch
        XCTAssertTrue(listCell.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(timeout: 10) { self.capsules(app).count == 1 && self.capsules(app)[0].frame.maxX <= listCell.frame.maxX })
    }

    func testPortraitListSlidesOverAndClosesOnPick() {
        XCUIDevice.shared.orientation = .portrait
        let app = launch(open: "agents/agt_0000000000000004")
        XCTAssertTrue(app.navigationBars["Add stock reservation expiry"].waitForExistence(timeout: 15))
        let row = app.staticTexts["row.agt_0000000000000006"]
        XCTAssertTrue(hidden(row))
        toggle(app).tap()
        XCTAssertTrue(shown(row))
        row.tap()
        XCTAssertTrue(hidden(row))
    }

    func testColumnStateKeptPerTab() {
        let app = launch(open: "agents/agt_0000000000000004")
        let row = sessionRow(app)
        XCTAssertTrue(shown(row))
        XCTAssertTrue(press(app, "s", [.control, .command], until: listHidden(app)))
        app.typeKey("2", modifierFlags: .command)
        XCTAssertTrue(shown(app.staticTexts[Self.task]))
        app.typeKey("1", modifierFlags: .command)
        XCTAssertTrue(app.navigationBars["Add stock reservation expiry"].waitForExistence(timeout: 10))
        XCTAssertTrue(listHidden(app))
    }

    func testReadingViewRestoresColumns() {
        let app = launch(open: "tab:wiki", ["-wiki", "YES"])
        let page = app.webViews.staticTexts["Shop wiki part 1 explains one step of the shop in detail."]
        XCTAssertTrue(page.waitForExistence(timeout: 15))
        let tree = app.buttons["Checkout flow"].firstMatch
        XCTAssertTrue(press(app, "f", [.control, .command], until: hidden(tree)))
        XCTAssertTrue(hidden(app.navigationBars["Shop wiki"]))
        XCTAssertTrue(press(app, "f", [.control, .command], until: shown(tree)))
        XCTAssertTrue(press(app, "s", [.control, .command], until: hidden(tree)))
        XCTAssertTrue(press(app, "f", [.control, .command], until: hidden(app.navigationBars["Shop wiki"])))
        XCTAssertTrue(press(app, "f", [.control, .command], until: shown(app.navigationBars["Shop wiki"])))
        XCTAssertTrue(hidden(tree))
        XCTAssertTrue(page.exists)
    }

    func testEveryListColumnLooksLikeAgents() {
        XCUIDevice.shared.orientation = .portrait
        let app = launch(open: "tab:agents", ["-wiki", "YES"])
        let agents = column(app, app.staticTexts["row.agt_0000000000000006"])
        XCTAssertEqual(agents.list.minX, 0)
        XCTAssertEqual(agents.list.width, 360)
        for (name, text) in [("Backlog", app.staticTexts[Self.task]), ("More", app.staticTexts["Processes"]), ("Wiki", app.buttons["Checkout flow"].firstMatch)] {
            tab(app, name).tap()
            if name == "Wiki" {
                XCTAssertTrue(app.webViews.firstMatch.waitForExistence(timeout: 15))
                toggle(app).tap()
            }
            XCTAssertEqual(column(app, text), agents, name)
        }
    }

    func testTabKeys() {
        let app = launch(open: "backlog/7")
        XCTAssertTrue(app.navigationBars["#7"].waitForExistence(timeout: 15))
        app.typeKey("i", modifierFlags: [.command, .shift])
        XCTAssertTrue(app.navigationBars["Inbox"].waitForExistence(timeout: 10))
        app.typeKey("[", modifierFlags: .command)
        XCTAssertTrue(app.navigationBars["#7"].waitForExistence(timeout: 10))
        app.typeKey("n", modifierFlags: .command)
        XCTAssertTrue(app.navigationBars["New Task"].waitForExistence(timeout: 10))
        app.typeKey("1", modifierFlags: .command)
        XCTAssertTrue(app.staticTexts["No Session Selected"].waitForExistence(timeout: 10))
    }

    func testShiftCommandFFocusesTheTabSearch() {
        let app = launch(open: "tab:backlog")
        let field = app.searchFields["Search, or a #number"]
        XCTAssertTrue(field.waitForExistence(timeout: 15))
        app.typeKey("f", modifierFlags: [.command, .shift])
        XCTAssertTrue(wait(field, "hasKeyboardFocus == true"))
        app.typeText("split")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Split checkout.go")).firstMatch.waitForExistence(timeout: 10))
    }

    func testEmptyAgentsDetailOpensTheOldestWaiting() {
        let app = launch(open: "tab:agents")
        let open = app.buttons["Open the oldest waiting"]
        XCTAssertTrue(open.waitForExistence(timeout: 15))
        open.tap()
        XCTAssertTrue(app.navigationBars["Explain the checkout flow"].waitForExistence(timeout: 10))
        XCTAssertFalse(open.exists)
    }

    func testWikiBodyDrawnAtRest() {
        for _ in 0..<3 {
            let app = launch(open: "tab:wiki", ["-wiki", "YES"])
            let text = app.webViews.staticTexts["Shop wiki part 1 explains one step of the shop in detail."]
            XCTAssertTrue(text.waitForExistence(timeout: 15))
            XCTAssertTrue(wait { self.inked(text.frame, in: XCUIScreen.main.screenshot()) }, "no text drawn in \(text.frame)")
            app.terminate()
        }
    }

    func testWikiTitleAndBodyShareTheColumn() {
        let app = openCheckoutFlow()
        let title = app.staticTexts["page.title"]
        let text = app.webViews.staticTexts["Checkout flow part 1 explains one step of the shop in detail."]
        XCTAssertTrue(title.waitForExistence(timeout: 10))
        XCTAssertEqual(title.frame.minX, text.frame.minX, accuracy: 2)
        let bar = app.navigationBars["Checkout flow"]
        XCTAssertLessThanOrEqual(title.frame.minY - bar.frame.maxY, 12)
        XCTAssertLessThanOrEqual(text.frame.width, 781)
        let barTitle = app.staticTexts["page.barTitle"]
        XCTAssertFalse(barTitle.exists && barTitle.label == "Checkout flow")
    }

    func testWikiMarginColumnShowsWhenTheListHides() {
        let app = openCheckoutFlow()
        let title = app.staticTexts["page.title"]
        let text = app.webViews.staticTexts["Checkout flow part 1 explains one step of the shop in detail."]
        let outline = app.webViews.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "On this page")).firstMatch
        XCTAssertTrue(outline.waitForExistence(timeout: 10))
        XCTAssertLessThan(outline.frame.minX, text.frame.minX + 30)
        toggle(app).tap()
        XCTAssertTrue(listHidden(app))
        XCTAssertTrue(wait { outline.exists && outline.frame.minX > text.frame.maxX + 30 }, "\(outline.frame) vs \(text.frame)")
        XCTAssertEqual(title.frame.minX, text.frame.minX, accuracy: 2)
    }

    func testWikiChatPanelOpensOnPicker() {
        let app = openCheckoutFlow()
        let row = app.buttons["Checkout flow"].firstMatch
        let panel = app.otherElements["chat.panel"]
        app.buttons["wiki.chat"].tap()
        XCTAssertTrue(panel.waitForExistence(timeout: 10))
        let first = app.staticTexts["Wiki: document review flow"]
        let second = app.staticTexts["Fix flaky land gate test"]
        XCTAssertTrue(shown(second))
        XCTAssertFalse(app.staticTexts["I can add a paragraph under Send about a closed opener."].exists)
        XCTAssertEqual(panel.frame.width, 400, accuracy: 2)
        XCTAssertGreaterThanOrEqual(second.frame.minY - first.frame.minY, 56)
        XCTAssertTrue(listHidden(app))
        XCTAssertTrue(app.staticTexts["chat.subtitle"].exists)
        XCTAssertFalse(app.buttons["Hide chat"].exists)
        let add = app.buttons["New session"]
        XCTAssertGreaterThanOrEqual(add.frame.width, panel.frame.width - 40 - 2)
        XCTAssertGreaterThanOrEqual(add.frame.height, 44)
        app.buttons["chat.close"].tap()
        XCTAssertTrue(wait { !panel.exists })
        XCTAssertTrue(shown(row))
        XCTAssertTrue(press(app, "b", .command, until: panel.waitForExistence(timeout: 4)))
        XCTAssertTrue(listHidden(app))
    }

    func testWikiChatContextToggleStaysAfterTheLastChip() {
        let app = openCheckoutFlow()
        app.buttons["wiki.chat"].tap()
        let session = app.staticTexts["Rate-limit notifications"].firstMatch
        XCTAssertTrue(session.waitForExistence(timeout: 10))
        session.tap()
        let chip = app.descendants(matching: .any)["composer.chip.page"]
        XCTAssertTrue(chip.waitForExistence(timeout: 10))
        app.buttons["Leave out Checkout flow page"].tap()
        XCTAssertTrue(wait { !chip.exists })
        XCTAssertTrue(app.buttons["composer.context"].exists)
    }

    func testScrolledWikiPageKeepsOneBarThenReads() {
        let app = openCheckoutFlow()
        let bar = app.navigationBars["Checkout flow"]
        let start = app.webViews.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.6))
        start.press(forDuration: 0.1, thenDragTo: start.withOffset(CGVector(dx: 0, dy: -110)), withVelocity: .slow, thenHoldForDuration: 0.3)
        let barTitle = app.staticTexts["page.barTitle"]
        XCTAssertTrue(wait { barTitle.exists && barTitle.label == "Checkout flow" })
        XCTAssertLessThanOrEqual(bar.frame.height, 56)
        app.webViews.firstMatch.swipeUp()
        app.webViews.firstMatch.swipeUp()
        XCTAssertTrue(wait { !(bar.exists && bar.isHittable) })
    }

    func testTabSwitchKeepsTheScrolledPageAndHidesTheOtherTabs() {
        let app = openCheckoutFlow()
        let start = app.webViews.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.6))
        start.press(forDuration: 0.1, thenDragTo: start.withOffset(CGVector(dx: 0, dy: -110)), withVelocity: .slow, thenHoldForDuration: 0.3)
        let part = app.webViews.staticTexts["Checkout flow part 3 explains one step of the shop in detail."]
        XCTAssertTrue(part.waitForExistence(timeout: 10))
        var top = part.frame.minY
        XCTAssertTrue(wait { let now = part.frame.minY; defer { top = now }; return now == top })
        tab(app, "Agents").tap()
        XCTAssertTrue(shown(sessionRow(app)))
        XCTAssertTrue(wait { !app.navigationBars["Checkout flow"].exists && !app.buttons["Shop wiki"].exists && app.webViews.count == 0 })
        tab(app, "Wiki").tap()
        XCTAssertTrue(part.waitForExistence(timeout: 10))
        XCTAssertEqual(part.frame.minY, top, accuracy: 1)
    }

    func testAFallbackTabKeepsTheTabsAndTheListToggle() {
        let app = launch(open: "tab:wiki")
        let agents = tab(app, "Agents")
        XCTAssertTrue(wait(timeout: 15) { agents.exists && agents.isSelected && !self.tab(app, "Wiki").exists })
        XCTAssertTrue(shown(sessionRow(app)))
        toggle(app).tap()
        XCTAssertTrue(listHidden(app))
    }

    func testOptionKeyDoesNotAnswerASessionInAHiddenTab() {
        let app = launch(open: "agents/agt_0000000000000001")
        let reject = app.buttons["Reject"]
        XCTAssertTrue(reject.waitForExistence(timeout: 15))
        tab(app, "Backlog").tap()
        XCTAssertTrue(shown(app.staticTexts[Self.task]))
        app.typeKey("1", modifierFlags: .option)
        tab(app, "Agents").tap()
        XCTAssertTrue(shown(reject))
        let answered = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: reject)
        answered.isInverted = true
        XCTAssertEqual(XCTWaiter().wait(for: [answered], timeout: 3), .completed)
    }

    func testOptionKeyAnswersTheSessionOpenedFromTheInboxOverAnotherTab() {
        let app = launch(open: "tab:backlog")
        XCTAssertTrue(shown(app.staticTexts[Self.task]))
        app.buttons["inbox.tray"].tap()
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "go test")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.tap()
        let reject = app.buttons["Reject"]
        XCTAssertTrue(reject.waitForExistence(timeout: 10))
        tab(app, "Agents").tap()
        app.staticTexts["row.agt_0000000000000006"].tap()
        XCTAssertTrue(app.textViews["composer"].waitForExistence(timeout: 10))
        tab(app, "Backlog").tap()
        XCTAssertTrue(shown(reject))
        XCTAssertTrue(press(app, "1", .option, until: wait(timeout: 3) { !reject.exists }))
    }

    func testOptionKeyLeavesACoveredSessionAndAnswersItAfterBack() {
        let app = launch(open: "agents/agt_0000000000000001")
        let reject = app.buttons["Reject"]
        XCTAssertTrue(reject.waitForExistence(timeout: 15))
        app.buttons["inbox.tray"].tap()
        let inbox = app.navigationBars["Inbox"]
        XCTAssertTrue(inbox.waitForExistence(timeout: 10))
        app.typeKey("1", modifierFlags: .option)
        inbox.buttons.firstMatch.tap()
        XCTAssertTrue(shown(reject))
        let answered = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: reject)
        answered.isInverted = true
        XCTAssertEqual(XCTWaiter().wait(for: [answered], timeout: 3), .completed)
        XCTAssertTrue(press(app, "1", .option, until: wait(timeout: 3) { !reject.exists }))
    }

    func testSessionHeaderKeepsItsTitleOnTheLeftAndItsStatusLine() {
        let app = launch(open: "agents/agt_0000000000000004", ["-longTranscript", "YES"])
        XCTAssertTrue(app.textViews["composer"].waitForExistence(timeout: 15))
        let sessions = ["agt_0000000000000004", "agt_0000000000000002", "agt_0000000000000001"]
        for round in 0..<3 {
            for id in sessions {
                app.staticTexts["row." + id].tap()
                XCTAssertTrue(wait(timeout: 10) { self.headerShown(app) }, "\(id) round \(round): no title on the left with its status line")
            }
            app.swipeDown()
            XCTAssertTrue(wait(timeout: 10) { self.headerShown(app) }, "round \(round): header gone after a scroll")
        }
        toggle(app).tap()
        XCTAssertTrue(listHidden(app))
        XCTAssertTrue(wait(timeout: 10) { self.headerShown(app) && self.capsules(app).count == 1 }, "header or capsule gone with the list hidden")
    }

    func testStepOutLadderAndPaneKeys() {
        let app = launch(open: "agents/agt_0000000000000004", ["-longTranscript", "YES"])
        let composer = app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 15))
        composer.tap()
        composer.typeText("draft")
        let focus = app.descendants(matching: .any)["transcript.focus"]
        app.typeKey("[", modifierFlags: .control)
        XCTAssertTrue(focus.waitForExistence(timeout: 5))
        app.typeKey("i", modifierFlags: [])
        XCTAssertTrue(wait(timeout: 5) { !focus.exists })
        app.typeKey("s", modifierFlags: [])
        XCTAssertTrue(wait(timeout: 5) { composer.value as? String == "drafts" })
        app.typeKey("[", modifierFlags: .control)
        XCTAssertTrue(focus.waitForExistence(timeout: 5))
        app.typeKey("[", modifierFlags: .control)
        XCTAssertTrue(app.descendants(matching: .any)["list.focus"].waitForExistence(timeout: 5))
        XCTAssertFalse(focus.exists)
        app.typeKey("[", modifierFlags: .control)
        app.typeKey("!", modifierFlags: [])
        XCTAssertTrue(wait(timeout: 5) { composer.value as? String == "drafts!" })
        app.typeKey(.upArrow, modifierFlags: [.option, .command])
        XCTAssertTrue(focus.waitForExistence(timeout: 5))
        let last = app.staticTexts["Last reply of the long session"]
        XCTAssertTrue(wait(timeout: 5) { self.onScreen(last) })
        app.typeKey(" ", modifierFlags: .shift)
        XCTAssertTrue(wait(timeout: 5) { !self.onScreen(last) })
        app.typeKey(" ", modifierFlags: [])
        XCTAssertTrue(wait(timeout: 5) { self.onScreen(last) })
        app.typeKey(.downArrow, modifierFlags: [.option, .command])
        XCTAssertTrue(wait(timeout: 5) { !focus.exists })
    }

    func testStepOutClosesTheReadingViewFirstAndHintsThatCommandPeriodStops() {
        let app = launch(open: "agents/agt_0000000000000004", ["-agents.stopHintShown", "0", "-stopHintSeconds", "300"])
        let composer = app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 15))
        composer.tap()
        let bar = app.navigationBars["Add stock reservation expiry"]
        app.typeKey("f", modifierFlags: [.control, .command])
        XCTAssertTrue(wait(timeout: 5) { !bar.exists })
        app.typeKey("[", modifierFlags: .control)
        XCTAssertTrue(bar.waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["stopHint"].exists)
        composer.tap()
        app.typeKey("[", modifierFlags: .control)
        XCTAssertTrue(app.staticTexts["stopHint"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.descendants(matching: .any)["list.focus"].exists)
        app.typeKey(".", modifierFlags: .command)
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Stop '")).firstMatch.waitForExistence(timeout: 5))
    }

    func testTranscriptBlocksStepAndGFollowsTheNewest() {
        let app = launch(open: "agents/agt_0000000000000004", ["-longTranscript", "YES"])
        let composer = app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 15))
        composer.tap()
        app.typeKey(.upArrow, modifierFlags: [.option, .command])
        let bar = {
            let found = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'transcript.bar.'")).firstMatch
            return found.exists ? found.identifier : nil
        }
        XCTAssertTrue(wait { bar() != nil })
        let newest = bar()
        app.typeKey("k", modifierFlags: [])
        XCTAssertTrue(wait { bar() != nil && bar() != newest })
        app.typeKey("g", modifierFlags: .shift)
        XCTAssertTrue(wait { bar() == newest })
    }

    func testBareKeyAnswersTheCardFromTheTranscript() {
        let app = launch(open: "agents/agt_0000000000000001")
        let reject = app.buttons["Reject"]
        XCTAssertTrue(reject.waitForExistence(timeout: 15))
        let composer = app.textViews["composer"]
        composer.tap()
        XCTAssertTrue(wait { (composer.value(forKey: "hasKeyboardFocus") as? Bool) == true })
        app.typeKey(.upArrow, modifierFlags: [.option, .command])
        XCTAssertTrue(app.descendants(matching: .any)["transcript.focus"].waitForExistence(timeout: 5))
        XCTAssertTrue(press(app, "n", [], until: wait(timeout: 3) { !reject.exists }))
    }

    func testStepOutDoesNothingUnderASheet() {
        let app = launch(open: "agents/agt_0000000000000001")
        XCTAssertTrue(app.buttons["Reject"].waitForExistence(timeout: 15))
        let confirm = app.staticTexts["Always allow this command?"]
        XCTAssertTrue(press(app, "2", .option, until: confirm.waitForExistence(timeout: 1.5)))
        app.typeKey("[", modifierFlags: .control)
        app.typeKey(.upArrow, modifierFlags: [.option, .command])
        XCTAssertFalse(app.descendants(matching: .any)["transcript.focus"].waitForExistence(timeout: 2))
        XCTAssertTrue(confirm.exists)
    }

    func testAFallbackTabTakesAPushAndItsBack() {
        let app = launch(open: "tab:wiki")
        let agents = tab(app, "Agents")
        XCTAssertTrue(wait(timeout: 15) { agents.exists && agents.isSelected && !self.tab(app, "Wiki").exists })
        app.buttons["inbox.tray"].tap()
        let inbox = app.navigationBars["Inbox"]
        XCTAssertTrue(inbox.waitForExistence(timeout: 10))
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "go test")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.tap()
        let reject = app.buttons["Reject"]
        XCTAssertTrue(reject.waitForExistence(timeout: 10))
        app.typeKey("[", modifierFlags: .command)
        XCTAssertTrue(wait { inbox.exists && !reject.exists })
    }

    func testLongCodeLineScrollsSideways() {
        let app = openCheckoutFlow()
        let code = app.webViews.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "booking.roots2")).firstMatch
        XCTAssertTrue(code.waitForExistence(timeout: 10))
        let before = code.frame.minX
        code.swipeLeft()
        XCTAssertTrue(wait { code.frame.minX < before - 40 }, "\(code.frame.minX) vs \(before)")
    }

    private func openCheckoutFlow() -> XCUIApplication {
        let app = launch(open: "tab:wiki", ["-wiki", "YES"])
        let row = app.buttons["Checkout flow"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15))
        row.tap()
        XCTAssertTrue(app.navigationBars["Checkout flow"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.webViews.staticTexts["Checkout flow part 1 explains one step of the shop in detail."].waitForExistence(timeout: 10))
        return app
    }

    private func inked(_ frame: CGRect, in shot: XCUIScreenshot) -> Bool {
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let upright = UIGraphicsImageRenderer(size: shot.image.size, format: format).image { _ in shot.image.draw(at: .zero) }
        guard upright.size.width > upright.size.height, let image = upright.cgImage, frame.width > 0, frame.height > 0,
              let crop = image.cropping(to: frame.integral) else { return false }
        let width = crop.width, height = crop.height
        var pixels = [UInt8](repeating: 0, count: width * height * 4)
        let context = CGContext(data: &pixels, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        context?.draw(crop, in: CGRect(x: 0, y: 0, width: width, height: height))
        let lumas = stride(from: 0, to: pixels.count, by: 4).map { Int(pixels[$0]) + Int(pixels[$0 + 1]) + Int(pixels[$0 + 2]) }
        guard let low = lumas.min(), let high = lumas.max() else { return false }
        return high - low > 300
    }

    private func wait(timeout: TimeInterval = 5, _ condition: @escaping () -> Bool) -> Bool {
        XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in condition() }, object: nil)], timeout: timeout) == .completed
    }

    private struct Column: Equatable {
        let list: CGRect
        let capsule: CGRect
        let card: [UInt8]
        let ground: [UInt8]
    }

    private func column(_ app: XCUIApplication, _ text: XCUIElement) -> Column {
        XCTAssertTrue(shown(text))
        let frame = app.cells.containing(NSPredicate(format: "identifier == %@ OR label == %@", text.identifier, text.label)).firstMatch.frame
        let list = app.collectionViews.allElementsBoundByIndex.first { $0.frame.contains(frame) }?.frame ?? .zero
        let shot = upright(app.screenshot())
        return Column(list: list, capsule: app.buttons["workspace.capsule"].frame,
                      card: color(at: CGPoint(x: list.maxX - 22, y: text.frame.midY), in: shot),
                      ground: color(at: CGPoint(x: 6, y: text.frame.midY), in: shot))
    }

    private func tab(_ app: XCUIApplication, _ title: String) -> XCUIElement {
        app.toolbars.buttons[title]
    }

    private func headerShown(_ app: XCUIApplication) -> Bool {
        let head = app.navigationBars.descendants(matching: .any)["page.header"]
        guard head.exists, head.label.contains(" · ") else { return false }
        let frame = head.frame
        let actions = app.navigationBars.buttons.allElementsBoundByIndex.filter { $0.frame.minX > frame.minX }
        return frame.width > 150 && frame.minX < app.windows.firstMatch.frame.midX && actions.allSatisfy { $0.frame.minX >= frame.maxX }
    }

    private func toggle(_ app: XCUIApplication) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier CONTAINS[c] 'sidebar' OR label CONTAINS[c] 'sidebar'")).element(boundBy: 0)
    }

    private func capsules(_ app: XCUIApplication) -> [XCUIElement] {
        app.buttons.matching(identifier: "workspace.capsule").allElementsBoundByIndex.filter { onScreen($0) && $0.isHittable }
    }

    private func sessionRow(_ app: XCUIApplication) -> XCUIElement {
        let row = app.staticTexts["row.agt_0000000000000006"]
        XCTAssertTrue(row.waitForExistence(timeout: 15))
        return row
    }

    private func press(_ app: XCUIApplication, _ key: String, _ modifiers: XCUIElement.KeyModifierFlags, until done: @autoclosure () -> Bool) -> Bool {
        for _ in 0..<6 {
            app.typeKey(key, modifierFlags: modifiers)
            if done() { return true }
        }
        return false
    }

    private func shown(_ element: XCUIElement) -> Bool {
        wait(timeout: 10) { self.onScreen(element) && element.isHittable }
    }

    private func hidden(_ element: XCUIElement) -> Bool {
        wait(timeout: 10) { !self.onScreen(element) || !element.isHittable }
    }

    private func onScreen(_ element: XCUIElement) -> Bool {
        guard element.exists else { return false }
        let frame = element.frame
        return !frame.isEmpty && XCUIApplication().windows.firstMatch.frame.contains(CGPoint(x: frame.midX, y: frame.midY))
    }

    private func listHidden(_ app: XCUIApplication) -> Bool {
        app.buttons["Show Sidebar"].waitForExistence(timeout: 10)
    }

    private func wait(_ element: XCUIElement, _ format: String) -> Bool {
        XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: format), object: element)], timeout: 10) == .completed
    }

    private func upright(_ shot: XCUIScreenshot) -> UIImage {
        let format = UIGraphicsImageRendererFormat()
        format.scale = shot.image.scale
        return UIGraphicsImageRenderer(size: shot.image.size, format: format).image { _ in shot.image.draw(at: .zero) }
    }

    private func color(at point: CGPoint, in image: UIImage) -> [UInt8] {
        guard let cgImage = image.cgImage else { return [] }
        var pixel = [UInt8](repeating: 0, count: 4)
        let context = CGContext(data: &pixel, width: 1, height: 1, bitsPerComponent: 8, bytesPerRow: 4, space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        context?.draw(cgImage, in: CGRect(x: -point.x * image.scale, y: point.y * image.scale - CGFloat(cgImage.height) + 1, width: CGFloat(cgImage.width), height: CGFloat(cgImage.height)))
        return Array(pixel.prefix(3))
    }

    private func launch(open target: String, _ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", target] + extra
        app.launch()
        let window = app.windows.firstMatch
        let landscape = XCUIDevice.shared.orientation.isLandscape
        XCTAssertTrue(wait(timeout: 10) { window.exists && (window.frame.width > window.frame.height) == landscape }, "the app did not turn to the device orientation")
        return app
    }
}
