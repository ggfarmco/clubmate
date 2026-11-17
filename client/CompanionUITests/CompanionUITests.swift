import XCTest

@MainActor
final class CompanionUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"
    private static let proLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.3&p=47622&id=mac-pro&n=MacBook%20Pro&e=4000000000"

    override func setUp() {
        continueAfterFailure = false
    }

    func testScanningTheFirstMacOpensItsSessions() {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-latency", "20", "-scan", Self.pairLink]
        app.launch()
        app.buttons["Scan pairing code"].tap()
        app.buttons["Scan QR Code"].tap()
        XCTAssertTrue(app.staticTexts["Explain the checkout flow"].waitForExistence(timeout: 10))
    }

    func testConfirmingALinkOpensItsSessions() {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-latency", "20", "-pair", Self.pairLink]
        app.launch()
        let pair = app.buttons["Pair"]
        XCTAssertTrue(pair.waitForExistence(timeout: 10))
        pair.tap()
        XCTAssertTrue(app.staticTexts["Explain the checkout flow"].waitForExistence(timeout: 10))
    }

    func testScanningAnotherMacOpensItsSessions() {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-latency", "20", "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", "sidebar", "-scan", Self.proLink]
        app.launch()
        let add = app.navigationBars.buttons["Pair a Mac"]
        XCTAssertTrue(add.waitForExistence(timeout: 10))
        sleep(2)
        add.tap()
        app.buttons["Scan QR Code"].tap()
        XCTAssertTrue(app.staticTexts["Move the state bucket to the new account"].waitForExistence(timeout: 10))
    }

    func testNewSessionSendsPhoto() {
        let app = launch(open: "agents")
        app.navigationBars.buttons["New Session"].tap()
        let composer = app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: composer, "hasKeyboardFocus == true"))
        app.buttons["Attach"].tap()
        app.buttons["Photos or Videos"].tap()
        let photo = app.images.matching(NSPredicate(format: "label BEGINSWITH 'Photo'")).firstMatch
        XCTAssertTrue(photo.waitForExistence(timeout: 10))
        photo.tap()
        app.buttons["Add"].tap()
        XCTAssertTrue(app.buttons["Remove"].waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText("Fix the layout")
        let send = app.buttons["Send"]
        XCTAssertTrue(wait(for: send, "isEnabled == true"))
        send.tap()
        XCTAssertTrue(app.staticTexts["1 image"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.navigationBars["Fix the layout"].waitForExistence(timeout: 5))
    }

    func testNewSessionPastesAnImageFromTheClipboard() {
        UIPasteboard.general.image = UIGraphicsImageRenderer(size: CGSize(width: 64, height: 64)).image { context in
            UIColor.systemRed.setFill()
            context.fill(CGRect(x: 0, y: 0, width: 64, height: 64))
        }
        let app = launch(open: "agents")
        app.navigationBars.buttons["New Session"].tap()
        let composer = app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: composer, "hasKeyboardFocus == true"))
        let suggestion = app.buttons["RecentPaste"]
        XCTAssertTrue(suggestion.waitForExistence(timeout: 5))
        suggestion.tap()
        XCTAssertTrue(app.buttons["Remove"].waitForExistence(timeout: 10))
        add(screenshot(app, "pasted"))
        composer.press(forDuration: 1)
        let paste = app.menuItems["Paste"]
        XCTAssertTrue(paste.waitForExistence(timeout: 5))
        paste.tap()
        XCTAssertTrue(wait(for: app.buttons.matching(identifier: "Remove").element(boundBy: 1), "exists == true"))
    }

    func testNewSessionSendsFile() {
        let app = launch(open: "agents")
        app.navigationBars.buttons["New Session"].tap()
        let composer = app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 5))
        app.buttons["Attach"].tap()
        add(screenshot(app, "attach-menu"))
        app.buttons["Files"].tap()
        let browse = app.buttons["Browse"]
        XCTAssertTrue(browse.waitForExistence(timeout: 10))
        browse.tap()
        let file = app.cells.matching(NSPredicate(format: "identifier BEGINSWITH 'crash-logs.zip'")).firstMatch
        XCTAssertTrue(file.waitForExistence(timeout: 10), "no crash-logs.zip in Files: run make companion-ui-test, which seeds it on its own simulator")
        file.tap()
        if app.buttons["Open"].waitForExistence(timeout: 2) { app.buttons["Open"].tap() }
        XCTAssertTrue(app.staticTexts["crash-logs.zip"].waitForExistence(timeout: 10))
        add(screenshot(app, "file-chip"))
        composer.tap()
        composer.typeText("Why does it crash")
        app.buttons["Send"].tap()
        XCTAssertTrue(app.navigationBars["Why does it crash"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["crash-logs.zip"].waitForExistence(timeout: 10))
        add(screenshot(app, "sent-file"))
    }

    func testASubagentThatNeedsYouIsNeverFolded() {
        let app = launch(open: "agents")
        XCTAssertTrue(app.staticTexts["Explain the checkout flow"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Show 1 subagents"].exists)
    }

    func testALandReviewerHasNoRowUnlessItAsksThenItNestsUnderItsLander() {
        let app = launch(open: "agents", ["-landReview", "YES"])
        let lander = app.staticTexts["Gate graph instead of a fixed guard"]
        let review = app.staticTexts["Round 1 review"]
        XCTAssertTrue(review.waitForExistence(timeout: 10))
        XCTAssertGreaterThan(review.frame.minY, lander.frame.maxY)
        XCTAssertGreaterThan(review.frame.minX, lander.frame.minX)
        XCTAssertEqual(app.staticTexts.matching(identifier: "Round 1 review").count, 1)
        XCTAssertFalse(app.staticTexts["Round 2 review"].exists)
        add(screenshot(app, "needs-you"))
        let closed = app.buttons["Closed · 1"]
        reveal(closed, in: app)
        XCTAssertTrue(closed.exists)
        XCTAssertFalse(app.staticTexts["Round 2 review"].exists)
        XCTAssertFalse(app.staticTexts["Land review"].exists)
        add(screenshot(app, "end-rows"))
    }

    func testOpenReviewOnTheGateScreenOpensTheReviewersSession() {
        let app = launch(open: "agents", ["-landReview", "YES"])
        XCTAssertTrue(app.staticTexts["Round 1 review"].waitForExistence(timeout: 10))
        let gates = app.staticTexts["Land gates"]
        reveal(gates, in: app)
        gates.tap()
        let gate = app.staticTexts["clubmate"]
        XCTAssertTrue(gate.waitForExistence(timeout: 10))
        gate.tap()
        let open = app.buttons["sdui.table.link.1794837000000000000/agt_00000000000000b4"]
        XCTAssertTrue(open.waitForExistence(timeout: 10))
        reveal(open, in: app)
        add(screenshot(app, "gate"))
        open.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Three confirmed bugs")).firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Round 1 review"].exists)
        add(screenshot(app, "reviewer"))
    }

    func testSubagentStopIsInTheCardMenuAndAsksFirst() {
        let app = launch(open: "agents/agt_0000000000000001")
        let card = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Explore: Check the cart tests")).firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Stop subagent"].exists)
        card.press(forDuration: 1)
        app.buttons["Stop Subagent…"].tap()
        let confirm = app.buttons["Stop Subagent"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 5))
        confirm.tap()
        let running = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Running ·")).firstMatch
        wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: running)], timeout: 5)
    }

    func testStartSheetPromptKeepsAGapBeforeEdit() {
        let app = launch(open: "backlog/9")
        let start = app.buttons["Start agent"]
        XCTAssertTrue(start.waitForExistence(timeout: 10))
        start.tap()
        let value = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'the title'")).firstMatch
        XCTAssertTrue(value.waitForExistence(timeout: 5))
        let edit = app.buttons.matching(identifier: "Edit").allElementsBoundByIndex.first { abs($0.frame.midY - value.frame.midY) < 20 }
        XCTAssertGreaterThanOrEqual((edit?.frame.minX ?? 0) - value.frame.maxX, 8)
    }

    func testAStaleEditShowsTheMacsVersionAndKeepsTheText() {
        let app = launch(open: "backlog/9", ["-staleEdit", "YES"])
        let title = refusedEdit(app) { app.buttons["Low"].tap() }
        XCTAssertTrue(app.staticTexts["Cart badge shows stale count after checkout (renamed on the Mac)"].exists)
        XCTAssertTrue((title.value as? String)?.contains(" fast") == true)
        add(screenshot(app, "stale-edit-editor"))
        app.buttons["Cancel"].tap()
        XCTAssertTrue(app.staticTexts["queuedText"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["queuedText"].firstMatch.label.contains(" fast"))
        XCTAssertEqual(app.staticTexts["queuedChange"].firstMatch.label, "Priority → Low")
        add(screenshot(app, "stale-edit"))
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "(renamed on the Mac)")).firstMatch.waitForExistence(timeout: 5))
        add(screenshot(app, "backlog-after-delta"))
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Macs"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Send again"].firstMatch.exists)
        add(screenshot(app, "macs-queued"))
        app.buttons["Dismiss"].firstMatch.tap()
        XCTAssertTrue(wait(for: app.staticTexts["Changed on the Mac"], "exists == false"))
    }

    func testSavingAgainInTheEditorLandsOnTheMacsNewVersion() {
        let app = launch(open: "backlog/9", ["-renameAfter", "10"])
        _ = refusedEdit(app, waiting: 10)
        app.buttons["Save"].tap()
        XCTAssertTrue(landed(app).waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Changed on the Mac"].exists)
    }

    func testSendAgainLandsARefusedEdit() {
        let app = launch(open: "backlog/9", ["-renameAfter", "10"])
        _ = refusedEdit(app, waiting: 10)
        app.buttons["Cancel"].tap()
        let again = app.buttons["Send again"].firstMatch
        XCTAssertTrue(again.waitForExistence(timeout: 5))
        add(screenshot(app, "send-again"))
        again.tap()
        XCTAssertTrue(landed(app).waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: app.staticTexts["Changed on the Mac"], "exists == false"))
    }

    private func landed(_ app: XCUIApplication) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS ' fast' AND NOT label CONTAINS 'renamed'")).firstMatch
    }

    private func refusedEdit(_ app: XCUIApplication, waiting: UInt32 = 0, also: () -> Void = {}) -> XCUIElement {
        let edit = app.buttons["Edit"]
        XCTAssertTrue(edit.waitForExistence(timeout: 10))
        edit.tap()
        let title = app.textFields["taskTitle"]
        XCTAssertTrue(title.waitForExistence(timeout: 5))
        title.tap()
        title.typeText(" fast")
        also()
        sleep(waiting)
        app.buttons["Save"].tap()
        XCTAssertTrue(app.staticTexts["Changed on the Mac"].waitForExistence(timeout: 10))
        XCTAssertTrue(title.exists)
        return title
    }

    func testFloatingMicStepsAsideWhileTypingATask() {
        let app = launch(open: "backlog/new")
        XCTAssertTrue(app.textFields["taskTitle"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Talk to Clubmate"].exists)
        app.navigationBars["New Task"].buttons.firstMatch.tap()
        XCTAssertTrue(app.buttons["Talk to Clubmate"].waitForExistence(timeout: 5))
    }

    func testNewTaskDraftSurvivesLeavingTheForm() {
        let app = launch(open: "backlog")
        let new = app.buttons["New Task"]
        XCTAssertTrue(new.waitForExistence(timeout: 10))
        new.tap()
        let title = app.textFields["taskTitle"]
        XCTAssertTrue(title.waitForExistence(timeout: 5))
        title.typeText("Kept while away")
        app.navigationBars["New Task"].buttons.firstMatch.tap()
        XCTAssertTrue(new.waitForExistence(timeout: 5))
        new.tap()
        XCTAssertTrue(title.waitForExistence(timeout: 5))
        XCTAssertEqual(title.value as? String, "Kept while away")
    }

    func testANumberPinsItsTaskAndReturnOpensIt() {
        let app = launch(open: "backlog", ["-search", "4"])
        XCTAssertTrue(app.staticTexts["Return opens #4"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Bump Go to 1.26")).firstMatch.exists)
        XCTAssertFalse(app.buttons["Start agent"].exists)
        let search = app.searchFields["Search, or a #number"]
        search.tap()
        search.typeText("\n")
        XCTAssertTrue(app.buttons["Start agent"].waitForExistence(timeout: 5))
    }

    func testTheFloatingMicStaysClearOfTheBackButtonOnATask() {
        let app = launch(open: "backlog/4")
        let mic = app.buttons["Talk to Clubmate"]
        XCTAssertTrue(mic.waitForExistence(timeout: 10))
        XCTAssertGreaterThan(mic.frame.minY, app.windows.firstMatch.frame.midY)
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(app.searchFields["Search, or a #number"].waitForExistence(timeout: 5))
    }

    func testBacklogOpensFromTheWorkspaceNotTheMac() {
        let app = launch(open: "sidebar")
        XCTAssertTrue(app.navigationBars["Macs"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Backlog"].exists)
        app.staticTexts["shop"].tap()
        let backlog = app.buttons["Backlog"]
        XCTAssertTrue(backlog.waitForExistence(timeout: 5))
        backlog.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Retry the charge step on 502")).firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.navigationBars["shop"].exists)
    }

    func testAPluginSectionOpensFromMore() {
        let app = launch(open: "more/processes")
        XCTAssertTrue(app.staticTexts["Terminal 1"].waitForExistence(timeout: 10))
    }

    func testBackToMacsNeverShowsChooseAMac() {
        let app = launch(open: "sidebar")
        let macs = app.navigationBars["Macs"]
        XCTAssertTrue(macs.waitForExistence(timeout: 10))
        for (row, title, swipe) in [("shop", "shop", false), ("shop", "shop", true), ("Inbox", "Inbox", false)] {
            reveal(app.staticTexts[row], in: app)
            app.staticTexts[row].tap()
            XCTAssertTrue(app.navigationBars[title].waitForExistence(timeout: 5))
            if swipe {
                let edge = app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5))
                edge.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)))
            } else {
                app.navigationBars[title].buttons.firstMatch.tap()
            }
            XCTAssertTrue(macs.waitForExistence(timeout: 5))
            XCTAssertFalse(app.staticTexts["Choose a Mac"].waitForExistence(timeout: 2))
            XCTAssertTrue(macs.exists)
        }
    }

    func testWorkspaceAndInboxTitlesAreLargeEveryTimeTheyOpen() {
        for macs in [[], ["-macs", "3"]] {
            let app = launch(open: "sidebar", macs)
            XCTAssertTrue(app.navigationBars["Macs"].waitForExistence(timeout: 10))
            for _ in 0..<3 {
                reveal(app.staticTexts["shop"].firstMatch, in: app)
                app.staticTexts["shop"].firstMatch.tap()
                XCTAssertTrue(poll { largeTitle(app, "shop") }, "\(macs) on entry")
                XCTAssertTrue(app.staticTexts["mac.line"].exists)
                let row = app.staticTexts["Explain the checkout flow"]
                XCTAssertTrue(row.waitForExistence(timeout: 5))
                row.tap()
                XCTAssertTrue(app.textViews["composer"].waitForExistence(timeout: 5))
                app.navigationBars.buttons.firstMatch.tap()
                XCTAssertTrue(poll { largeTitle(app, "shop") }, "\(macs) back from a session")
                app.buttons["Backlog"].firstMatch.tap()
                XCTAssertTrue(poll { largeTitle(app, "shop") }, "\(macs) on Backlog")
                app.buttons["Agents"].firstMatch.tap()
                XCTAssertTrue(row.waitForExistence(timeout: 5))
                app.navigationBars["shop"].buttons.firstMatch.tap()
                XCTAssertTrue(app.navigationBars["Macs"].waitForExistence(timeout: 5))
                reveal(app.staticTexts["Inbox"].firstMatch, in: app)
                app.staticTexts["Inbox"].firstMatch.tap()
                XCTAssertTrue(poll { largeTitle(app, "Inbox") }, "\(macs) Inbox")
                app.navigationBars["Inbox"].buttons.firstMatch.tap()
                XCTAssertTrue(app.navigationBars["Macs"].waitForExistence(timeout: 5))
            }
            app.terminate()
        }
    }

    private func largeTitle(_ app: XCUIApplication, _ title: String) -> Bool {
        let text = app.navigationBars[title].staticTexts[title].firstMatch
        return text.exists && text.frame.height > 30 && text.frame.minX < 40
    }

    func testSendQueuesAndStopAsksOnceFromTheMenu() {
        let app = launch(open: "agents/agt_0000000000000002")
        let composer = app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText("Add retries")
        app.buttons["Send"].tap()
        XCTAssertTrue(app.staticTexts["Add retries"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Queue"].waitForExistence(timeout: 5))
        app.navigationBars.buttons["More"].tap()
        app.buttons["Stop…"].tap()
        let stop = app.buttons["Stop"]
        XCTAssertTrue(stop.waitForExistence(timeout: 5))
        stop.tap()
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["Working…"].exists)
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Got it'")).firstMatch.waitForExistence(timeout: 5))
    }

    func testAlwaysAllowConfirmsInASheetThenCollapsesToARow() {
        let app = launch(open: "agents/agt_0000000000000001")
        let always = app.buttons["Always allow…"]
        XCTAssertTrue(always.waitForExistence(timeout: 10))
        always.tap()
        XCTAssertTrue(app.staticTexts["Always allow this command?"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Bash(go test:*)"].exists)
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "in demo without asking, on this Mac")).firstMatch.exists)
        add(screenshot(app, "always-allow"))
        app.buttons["Always allow"].tap()
        XCTAssertTrue(app.staticTexts["Always allowed · go test ./..."].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Allow once"].exists)
    }

    func testTurnOnAFKFromSessionSettings() {
        let app = launch(open: "agents/agt_0000000000000001")
        let more = app.navigationBars.buttons["More"]
        XCTAssertTrue(more.waitForExistence(timeout: 10))
        more.tap()
        app.buttons["Session Settings"].tap()
        let afk = app.switches["settings.afk"]
        XCTAssertTrue(afk.waitForExistence(timeout: 5))
        afk.switches.firstMatch.tap()
        XCTAssertTrue(wait(for: afk, "value == '1'"))
        app.buttons["Done"].tap()
        more.tap()
        app.buttons["Session Settings"].tap()
        XCTAssertTrue(app.switches["settings.afk"].waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: app.switches["settings.afk"], "value == '1'"))
    }

    func testOptionKeysAnswerThePermissionInButtonOrder() {
        let app = launch(open: "agents/agt_0000000000000001")
        XCTAssertTrue(app.buttons["Reject"].waitForExistence(timeout: 10))
        app.typeKey("2", modifierFlags: .option)
        XCTAssertTrue(app.staticTexts["Always allow this command?"].waitForExistence(timeout: 5))
        app.buttons["Cancel"].tap()
        XCTAssertTrue(wait(for: app.staticTexts["Always allow this command?"], "exists == false"))
        XCTAssertTrue(app.buttons["Reject"].exists)
        app.typeKey("1", modifierFlags: .option)
        XCTAssertTrue(wait(for: app.buttons["Reject"], "exists == false"))
    }

    func testOptionKeysAnswerARunStep() {
        let step = launch(open: "agents/gr_0000000000000004", ["-runs", "YES", "-workspace", "prj_00000000000000f2"])
        let reject = step.buttons["Reject"]
        XCTAssertTrue(reject.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: reject, "isEnabled == true"))
        sleep(1)
        step.typeKey("2", modifierFlags: .option)
        XCTAssertTrue(step.buttons["Always allow"].waitForExistence(timeout: 5))
        step.swipeDown(velocity: .fast)
        XCTAssertTrue(wait(for: step.buttons["Always allow"], "exists == false"))
        step.typeKey("1", modifierFlags: .option)
        XCTAssertTrue(wait(for: reject, "exists == false"))
    }

    func testRotatingKeepsTheOpenSession() {
        let app = launch(open: "agents/agt_0000000000000002")
        let title = app.navigationBars["Explain the checkout flow"]
        XCTAssertTrue(title.waitForExistence(timeout: 10))
        for orientation in [UIDeviceOrientation.landscapeLeft, .portrait] {
            XCUIDevice.shared.orientation = orientation
            XCTAssertTrue(title.waitForExistence(timeout: 5))
            XCTAssertTrue(app.textViews["composer"].exists)
        }
    }

    func testMessageDocLinkOpensTheDocSheet() {
        let app = launch(open: "agents/agt_0000000000000002")
        let link = app.links["the retries plan"]
        XCTAssertTrue(link.waitForExistence(timeout: 10))
        link.tap()
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 10))
    }

    func testCodeBlockCopyButtonCopies() {
        let app = launch(open: "agents/agt_0000000000000001")
        let copy = app.buttons["Copy"]
        XCTAssertTrue(copy.waitForExistence(timeout: 10))
        app.swipeDown()
        app.swipeDown()
        copy.tap()
        XCTAssertTrue(app.buttons["Copied"].waitForExistence(timeout: 2))
        add(screenshot(app, "code-copied"))
    }

    func testDocCardOpensTheDocSheet() {
        let app = launch(open: "agents/agt_0000000000000002")
        let card = app.buttons["Open Checkout retries"]
        XCTAssertTrue(card.waitForExistence(timeout: 10))
        card.tap()
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 10))
    }

    func testMentionedDocsPlateOpensIntoCards() {
        let app = launch(open: "agents/agt_0000000000000002")
        let plate = app.buttons["2 docs mentioned"]
        XCTAssertTrue(plate.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Open checkout-flow"].exists)
        plate.tap()
        XCTAssertTrue(app.buttons["Open checkout-flow"].waitForExistence(timeout: 5))
    }

    func testDocLinkPushesUpdatesLiveAndBackReturns() {
        let app = launch(open: "agents", ["-doc", "/Users/me/src/shop/docs/checkout-retries.md"])
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.navigationBars.buttons["Back"].exists)
        add(screenshot(app, "doc-top"))
        let link = app.webViews.links["payments overview"]
        XCTAssertTrue(link.waitForExistence(timeout: 5))
        XCTAssertTrue(app.webViews.staticTexts["Updated while you were reading."].waitForExistence(timeout: 10))
        link.tap()
        XCTAssertTrue(app.navigationBars["Payments overview"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.webViews.links["checkout retries"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.webViews.staticTexts["Updated while you were reading."].waitForExistence(timeout: 10))
        add(screenshot(app, "doc-linked-updated"))
        app.navigationBars.buttons["Back"].tap()
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.navigationBars.buttons["Back"].exists)
        for index in 1...3 {
            app.webViews.firstMatch.swipeUp()
            add(screenshot(app, "doc-scroll-\(index)"))
        }
        app.buttons["Done"].tap()
        XCTAssertTrue(wait(for: app.navigationBars["Checkout retries"], "exists == false"))
    }

    func testDocOpensFromTheBannerAndAToolRow() {
        let app = launch(open: "agents/agt_0000000000000001", ["-docOpened", "YES"])
        let banner = app.buttons["Opened checkout-retries.md"]
        XCTAssertTrue(banner.waitForExistence(timeout: 10))
        add(screenshot(app, "doc-banner"))
        app.buttons["Open"].tap()
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 10))
        app.buttons["Done"].tap()
        XCTAssertTrue(wait(for: banner, "exists == false"))
        let group = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '3 actions'")).firstMatch
        reveal(group, in: app)
        group.tap()
        let open = app.buttons["Open checkout-retries.md"]
        reveal(open, in: app)
        add(screenshot(app, "doc-tool-row"))
        open.tap()
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 10))
    }

    func testDocsOpenFromTheWorkspaceListAndTheSessionMenu() {
        let app = launch(open: "docs")
        let payments = app.buttons["payments.md"]
        XCTAssertTrue(payments.waitForExistence(timeout: 10))
        add(screenshot(app, "docs-list"))
        let search = app.searchFields["Search docs"]
        search.tap()
        search.typeText("decisions")
        XCTAssertTrue(wait(for: payments, "exists == false"))
        add(screenshot(app, "docs-search"))
        app.buttons["0007-reserve-stock-before-charging-the-card-and-release-on-timeout.md"].tap()
        XCTAssertTrue(app.navigationBars["0007-reserve-stock-before-charging-the-card-and-release-on-timeout"].waitForExistence(timeout: 10))

        let session = launch(open: "agents/agt_0000000000000001")
        session.buttons["More"].firstMatch.tap()
        let docs = session.buttons["Docs"]
        XCTAssertTrue(docs.waitForExistence(timeout: 5))
        docs.tap()
        let conversion = session.buttons.matching(NSPredicate(format: "label BEGINSWITH 'conversion.html'")).firstMatch
        XCTAssertTrue(conversion.waitForExistence(timeout: 5))
        XCTAssertFalse(session.buttons.matching(NSPredicate(format: "label BEGINSWITH 'payments.md'")).firstMatch.exists)
        add(screenshot(session, "session-docs-menu"))
        conversion.tap()
        XCTAssertTrue(session.navigationBars["Checkout conversion"].waitForExistence(timeout: 10))
    }

    func testPlanFoldsDoneStepsBehindShowDone() {
        let app = launch(open: "agents/agt_0000000000000001")
        let show = app.buttons["Show done"]
        XCTAssertTrue(show.waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Extract cartTotal"].exists)
        show.tap()
        XCTAssertTrue(app.staticTexts["Extract cartTotal"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Hide done"].exists)
    }

    func testOpeningASessionFromTheListLeavesOnlyTheComposerMic() {
        let app = launch(open: "agents")
        let row = app.staticTexts["Explain the checkout flow"]
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Talk to Clubmate"].waitForExistence(timeout: 5))
        for _ in 0..<3 {
            row.tap()
            XCTAssertTrue(app.textViews["composer"].waitForExistence(timeout: 5))
            XCTAssertTrue(app.buttons["Dictate"].waitForExistence(timeout: 5))
            XCTAssertTrue(wait(for: app.buttons["Talk to Clubmate"], "exists == false"))
            sleep(1)
            XCTAssertFalse(app.buttons["Talk to Clubmate"].exists)
            app.navigationBars.buttons.firstMatch.tap()
            XCTAssertTrue(row.waitForExistence(timeout: 5))
        }
    }

    func testSearchTakesTheWholeBottomBarWhenVoiceIsOff() {
        let app = launch(open: "agents", ["-voiceOff", "YES"])
        let search = app.searchFields["Search sessions"]
        XCTAssertTrue(search.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Talk to Clubmate"].exists)
        XCTAssertLessThan(search.frame.minX, 40)
    }

    func testHandOffAFailedSessionOpensTheNewOne() {
        let app = launch(open: "agents/agt_0000000000000005", ["-failureAction", "continue"])
        let handoff = app.buttons["Hand off to a new session"]
        XCTAssertTrue(handoff.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Claude exited with status 1."].exists)
        handoff.tap()
        XCTAssertTrue(app.textViews["composer"].waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: app.staticTexts["Claude exited with status 1."], "exists == false"))
        XCTAssertFalse(app.staticTexts["This session is closed"].exists)
    }

    func testReopenTakesAClosedSessionOutOfClosed() {
        let app = launch(open: "agents/agt_0000000000000003", ["-reopenAction", "resume"])
        let reopen = app.buttons["Reopen"]
        XCTAssertTrue(reopen.waitForExistence(timeout: 10))
        XCTAssertFalse(app.textViews["composer"].exists)
        reopen.tap()
        XCTAssertTrue(app.textViews["composer"].waitForExistence(timeout: 5))
        XCTAssertFalse(reopen.exists)
    }

    func testQuestionCardPagesItsQuestionsAndSendsEveryAnswer() {
        let app = launch(open: "agents/agt_0000000000000004", ["-questionFixture", "YES"])
        let card = app.otherElements["elicitation-card"]
        XCTAssertTrue(card.staticTexts["1 of 2"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["What should happen when a reservation expires?"].exists)
        let thirty = option("30 minutes", in: card)
        XCTAssertTrue(wait(for: thirty, "isEnabled == true"))
        thirty.tap()
        card.buttons["Next"].tap()
        XCTAssertTrue(card.staticTexts["2 of 2"].waitForExistence(timeout: 5))
        XCTAssertFalse(card.buttons["Skip"].exists)
        let other = option("Other…", in: card)
        XCTAssertTrue(wait(for: other, "isEnabled == true"))
        XCTAssertEqual(card.textFields.count, 0)
        other.tap()
        let note = card.textFields.firstMatch
        XCTAssertTrue(note.waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: note, "hasKeyboardFocus == true"))
        note.typeText("Log it")
        card.buttons["Send"].tap()
        let answer = app.staticTexts.matching(NSPredicate(format: "label CONTAINS '30 minutes · Log it'")).firstMatch
        XCTAssertTrue(answer.waitForExistence(timeout: 5))
    }

    func testPickingTheOnlyQuestionsOptionCarriesTheOtherNote() {
        let app = launch(open: "agents/agt_0000000000000004", ["-questionFixture", "YES", "-questionSingle", "YES"])
        let card = app.otherElements["elicitation-card"]
        let other = option("Other…", in: card)
        XCTAssertTrue(other.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: other, "isEnabled == true"))
        other.tap()
        let note = card.textFields.firstMatch
        XCTAssertTrue(note.waitForExistence(timeout: 5))
        note.typeText("cache")
        option("30 minutes", in: card).tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS '30 minutes · cache'")).firstMatch.waitForExistence(timeout: 5))
    }

    func testLongQuestionCardShowsItAllAndKeepsSendInView() {
        let app = launch(open: "agents/agt_0000000000000004", ["-questionFixture", "YES", "-questionSingle", "YES", "-questionLong", "YES"])
        let card = app.otherElements["elicitation-card"]
        let question = card.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'reserve.go'")).firstMatch
        XCTAssertTrue(question.waitForExistence(timeout: 10))
        XCTAssertGreaterThan(question.frame.height, 160)
        let composer = app.textViews["composer"]
        composer.tap()
        XCTAssertTrue(wait(for: composer, "hasKeyboardFocus == true"))
        typeOther("Thirty minutes for card payments, but hold bank transfers for a full day", in: card)
        XCTAssertTrue(card.buttons["Send"].isHittable)
        let region = card.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.4))
        for _ in 0..<6 where question.frame.minY < card.frame.minY {
            region.press(forDuration: 0.05, thenDragTo: region.withOffset(CGVector(dx: 0, dy: 150)))
        }
        XCTAssertGreaterThan(question.frame.minY, card.frame.minY)
        XCTAssertTrue(card.buttons["Send"].isHittable)
        card.buttons["Send"].tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'hold bank transfers'")).firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: composer, "isHittable == true"))
        XCTAssertFalse(composer.value(forKey: "hasKeyboardFocus") as? Bool ?? true)
    }

    func testLongPagedQuestionKeepsItsOtherFieldAndNextInView() {
        let app = launch(open: "agents/agt_0000000000000004", ["-questionFixture", "YES", "-questionLong", "YES"])
        let card = app.otherElements["elicitation-card"]
        XCTAssertTrue(card.staticTexts["1 of 2"].waitForExistence(timeout: 10))
        typeOther("Thirty minutes for cards, a day for bank transfers", in: card)
        XCTAssertTrue(card.buttons["Next"].isHittable)
        XCTAssertFalse(app.textViews["composer"].exists && app.textViews["composer"].isHittable)
    }

    func testLongOpenItemsOtherFieldKeepsSendAnswersInView() {
        let app = launch(open: "agents/agt_0000000000000024", ["-openFixture", "YES", "-openStress", "YES"])
        let card = app.otherElements["open-card"]
        XCTAssertTrue(card.staticTexts["7 open"].waitForExistence(timeout: 10))
        typeOther("Back off, but only on cellular, and log every retry", in: card)
        XCTAssertTrue(card.buttons["Send answers"].isHittable)
        XCTAssertFalse(app.textViews["composer"].exists && app.textViews["composer"].isHittable)
    }

    func testQuestionCardSendWaitsForAnAnswer() {
        let app = launch(open: "agents/agt_0000000000000004", ["-questionFixture", "YES"])
        let card = app.otherElements["elicitation-card"]
        XCTAssertTrue(card.staticTexts["1 of 2"].waitForExistence(timeout: 10))
        let next = card.buttons["Next"]
        XCTAssertTrue(wait(for: next, "isEnabled == true"))
        next.tap()
        XCTAssertTrue(card.staticTexts["2 of 2"].waitForExistence(timeout: 5))
        let tick = option("Count it", in: card)
        XCTAssertTrue(wait(for: tick, "isEnabled == true"))
        XCTAssertFalse(card.buttons["Send"].isEnabled)
        tick.tap()
        XCTAssertTrue(wait(for: card.buttons["Send"], "isEnabled == true"))
        card.buttons["Send"].tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Count it'")).firstMatch.waitForExistence(timeout: 5))
    }

    func testQuestionCardKeepsItsPageAndPicksAfterLeavingTheSession() {
        let app = launch(open: "agents/agt_0000000000000004", ["-questionFixture", "YES"])
        let card = app.otherElements["elicitation-card"]
        let thirty = option("30 minutes", in: card)
        XCTAssertTrue(thirty.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: thirty, "isEnabled == true"))
        thirty.tap()
        card.buttons["Next"].tap()
        XCTAssertTrue(card.staticTexts["2 of 2"].waitForExistence(timeout: 5))
        app.navigationBars.buttons.firstMatch.tap()
        let row = app.staticTexts["Add stock reservation expiry"]
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.tap()
        XCTAssertTrue(card.staticTexts["2 of 2"].waitForExistence(timeout: 5))
        let back = card.buttons["Back"]
        XCTAssertTrue(wait(for: back, "isEnabled == true"))
        back.tap()
        XCTAssertTrue(card.staticTexts["1 of 2"].waitForExistence(timeout: 5))
        XCTAssertTrue(thirty.isSelected)
        XCTAssertTrue(wait(for: card.buttons["Next"], "isEnabled == true"))
    }

    func testReopenedSessionStillRefreshesItsTranscript() {
        let app = launch(open: "agents/agt_0000000000000004", ["-questionFixture", "YES", "-questionSingle", "YES"])
        let card = app.otherElements["elicitation-card"]
        XCTAssertTrue(option("30 minutes", in: card).waitForExistence(timeout: 10))
        for _ in 0..<2 {
            app.navigationBars.buttons.firstMatch.tap()
            let row = app.staticTexts["Add stock reservation expiry"]
            XCTAssertTrue(row.waitForExistence(timeout: 5))
            row.tap()
        }
        let thirty = option("30 minutes", in: card)
        XCTAssertTrue(thirty.waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: thirty, "isEnabled == true"))
        thirty.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH '30 minutes'")).firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'No longer pending'")).firstMatch.exists)
    }

    func testALongTranscriptOpensAtItsLastReply() {
        let app = launch(open: "agents", ["-longTranscript", "YES"])
        let row = app.staticTexts["Add stock reservation expiry"]
        let last = app.staticTexts["Last reply of the long session"]
        let window = app.windows.firstMatch
        for round in 0..<3 {
            XCTAssertTrue(row.waitForExistence(timeout: 10))
            row.tap()
            let shown = poll { last.exists && window.frame.contains(last.frame) }
            add(screenshot(app, "long-\(round)"))
            XCTAssertTrue(shown, "round \(round) opened away from the last reply")
            app.navigationBars.buttons.firstMatch.tap()
        }
    }

    func testALongTranscriptDrawsItsLastRowsAndTheEarlierOnesOnATap() {
        let app = launch(open: "agents/agt_0000000000000004", ["-longTranscript", "YES"])
        let first = app.staticTexts["Turn 1: go on"]
        XCTAssertTrue(app.staticTexts["Last reply of the long session"].waitForExistence(timeout: 10))
        XCTAssertFalse(first.exists)
        let earlier = app.buttons["earlier-rows"]
        earlier.tap()
        let window = app.windows.firstMatch
        XCTAssertTrue(poll { first.exists && window.frame.contains(first.frame) })
        XCTAssertFalse(earlier.exists)
    }

    func testALongerTranscriptPagesAHundredRowsAtATime() {
        let app = launch(open: "agents/agt_0000000000000004", ["-longTranscript", "YES", "-longTranscriptTurns", "85"])
        XCTAssertTrue(app.staticTexts["Last reply of the long session"].waitForExistence(timeout: 10))
        let earlier = app.buttons["earlier-rows"]
        let window = app.windows.firstMatch
        for (label, first) in [("Show 100 earlier · 111 left", "Reply 6"), ("Show 11 earlier", "Turn 1: go on")] {
            XCTAssertEqual(earlier.label, label)
            earlier.tap()
            let landed = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", first)).firstMatch
            XCTAssertTrue(poll { landed.exists && window.frame.contains(landed.frame) }, "\(label) did not land on \(first)")
        }
        XCTAssertFalse(earlier.exists)
    }

    func testASlimToolCallFetchesItsOutputWhenExpanded() {
        for fails in [false, true] {
            let app = launch(open: "agents/agt_0000000000000001", fails ? ["-failCommands", "agents.tool-call"] : [])
            let group = app.buttons.matching(NSPredicate(format: "label CONTAINS '3 actions'")).firstMatch
            XCTAssertTrue(group.waitForExistence(timeout: 10))
            reveal(group, in: app)
            group.tap()
            let edit = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Edit cart.go'")).firstMatch
            XCTAssertTrue(edit.waitForExistence(timeout: 5))
            add(screenshot(app, "slim-collapsed"))
            edit.tap()
            let shown = fails ? app.staticTexts["Couldn't load the output"] : app.staticTexts["func cartTotal(items []Item) int {"]
            XCTAssertTrue(shown.waitForExistence(timeout: 5))
            add(screenshot(app, fails ? "slim-failed" : "slim-expanded"))
            app.terminate()
        }
    }

    func testEveryImageOfALongTranscriptLoadsWhenScrolledTo() {
        XCUIDevice.shared.appearance = .light
        let app = launch(open: "agents/agt_0000000000000004", ["-manyImages", "YES"])
        XCTAssertTrue(app.buttons["shot-60.png"].waitForExistence(timeout: 10))
        for shot in [60, 55, 1, 60] {
            let tile = app.buttons["shot-\(shot).png"]
            bringIntoView(tile, in: app)
            XCTAssertTrue(poll { self.showsImage(tile, in: app) }, "shot-\(shot) kept its spinner")
        }
    }

    func testOpenCardSendsItsPicksAndKeepsTheRestOpen() {
        let app = launch(open: "agents/agt_0000000000000021", ["-openFixture", "YES"])
        let card = app.otherElements["open-card"]
        XCTAssertTrue(card.staticTexts["5 open"].waitForExistence(timeout: 10))
        XCTAssertFalse(card.buttons["Send answers"].isEnabled)
        option("Add an SDUI prop", in: card).tap()
        option("3", in: card).tap()
        option("Close #185", in: card).tap()
        XCTAssertTrue(card.buttons.matching(NSPredicate(format: "label CONTAINS 'Add an SDUI prop' AND NOT label BEGINSWITH 'Add'")).firstMatch.waitForExistence(timeout: 5))
        card.buttons["Send answers"].tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Lists → Add an SDUI prop; do: Close #185'")).firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(card.staticTexts["3 open"].waitForExistence(timeout: 5))
    }

    func testNoToAllOnTheOpenCardClosesInOneTapWithUndo() {
        let app = launch(open: "agents/agt_0000000000000021", ["-openFixture", "YES"])
        let card = app.otherElements["open-card"]
        XCTAssertTrue(card.staticTexts["5 open"].waitForExistence(timeout: 10))
        let no = card.buttons["No to All"]
        XCTAssertTrue(wait(for: no, "isEnabled == true"))
        no.tap()
        let closing = app.otherElements["open-closing"]
        XCTAssertTrue(closing.staticTexts["Closed 5 items"].waitForExistence(timeout: 5))
        closing.buttons["Undo"].tap()
        XCTAssertTrue(card.staticTexts["5 open"].waitForExistence(timeout: 5))
        no.tap()
        XCTAssertTrue(closing.waitForExistence(timeout: 5))
        XCTAssertTrue(closing.waitForNonExistence(timeout: 10))
        XCTAssertFalse(card.exists)
    }

    func testSelectedBacklogAndMoreRowsAreTintedNotFilledOnIPad() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "selection shows only in the iPad columns")
        XCUIDevice.shared.appearance = .light
        for (target, title) in [("backlog/7", "Retry the charge step on 502"), ("more/cron", "Cron")] {
            let app = launch(open: target)
            let row = app.cells.containing(NSPredicate(format: "label CONTAINS %@", title)).firstMatch
            XCTAssertTrue(row.waitForExistence(timeout: 10))
            sleep(2)
            let point = CGPoint(x: row.frame.minX + 8, y: row.frame.midY)
            XCTAssertGreaterThan(brightness(at: point, in: app.screenshot()), 0.7, target)
        }
    }

    func testAnswerAllPassesEveryItemThenSendsToEachSession() {
        let app = launch(open: "agents", ["-openFixture", "YES"])
        let all = app.buttons["Answer all"]
        XCTAssertTrue(all.waitForExistence(timeout: 10))
        all.tap()
        let step = app.otherElements["open-step"]
        XCTAssertTrue(step.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["1 of 7"].exists)
        option("Add an SDUI prop", in: step).tap()
        XCTAssertTrue(app.staticTexts["1 answered"].waitForExistence(timeout: 5))
        for number in 2...6 {
            sleep(1)
            app.buttons[number == 3 ? "Backlog" : number == 2 ? "Next" : "Later"].tap()
            XCTAssertTrue(app.staticTexts["\(number) of 7"].waitForExistence(timeout: 5))
        }
        sleep(1)
        app.buttons["Next"].tap()
        XCTAssertTrue(app.staticTexts["7 of 7"].waitForExistence(timeout: 5))
        sleep(1)
        option("Do this", in: step).tap()
        app.buttons["Send answers"].tap()
        XCTAssertTrue(app.staticTexts["Open"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Rework the Send review sheet"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Send answers"].exists)
    }

    private func typeOther(_ text: String, in card: XCUIElement) {
        let other = option("Other…", in: card)
        XCTAssertTrue(other.waitForExistence(timeout: 5))
        for _ in 0..<4 where !other.isHittable {
            card.swipeUp()
        }
        XCTAssertTrue(wait(for: other, "isEnabled == true"))
        other.tap()
        let field = card.textFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: field, "hasKeyboardFocus == true"))
        field.typeText(text)
        XCTAssertTrue(wait(for: field, "isHittable == true"))
    }

    private func option(_ title: String, in card: XCUIElement) -> XCUIElement {
        card.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
    }

    func testConnectionCheckFromMacDetailsSaysWhatToFix() {
        let app = launch(open: "mac", ["-checkFixture", "fail"])
        let row = app.buttons["mac.connection"]
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.tap()
        let run = app.buttons["connection.run"]
        XCTAssertTrue(run.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Connected"].waitForExistence(timeout: 5))
        run.tap()
        let port = app.descendants(matching: .any)["connection.step.tcp:100.101.1.2"]
        XCTAssertTrue(wait(for: port, "label CONTAINS 'may be asleep'"))
        XCTAssertTrue(app.descendants(matching: .any)["connection.step.tailscale"].label.contains("Tailscale isn't on on this phone"))
        XCTAssertTrue(wait(for: app.descendants(matching: .any)["connection.step.hello"], "label CONTAINS 'no address answered'"))
    }

    func testKeepAwakeFromMacDetailsHoldsAndTurnsOff() {
        let app = launch(open: "mac")
        let state = app.staticTexts["mac.awake.state"]
        XCTAssertTrue(state.waitForExistence(timeout: 10))
        XCTAssertEqual(state.label, "The Mac sleeps as usual.")
        let keep = app.buttons["mac.awake.keep"]
        keep.tap()
        XCTAssertTrue(app.buttons["mac.awake.agents"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["mac.awake.on"].exists)
        XCTAssertFalse(app.buttons["mac.awake.off"].exists)
        app.buttons["4 Hours"].tap()
        XCTAssertTrue(wait(for: state, "label BEGINSWITH 'Awake until'"))
        XCTAssertTrue(app.staticTexts["mac.awake.value"].label.hasPrefix("Until "))
        keep.tap()
        let off = app.buttons["mac.awake.off"]
        XCTAssertTrue(off.waitForExistence(timeout: 5))
        off.tap()
        XCTAssertTrue(wait(for: state, "label == 'The Mac sleeps as usual.'"))
        XCTAssertEqual(app.staticTexts["mac.awake.value"].label, "Off")
    }

    func testKeepAwakeAgentHoldOpensItsSession() {
        let app = launch(open: "mac", ["-awake", "hours"])
        let hold = app.buttons["mac.awake.hold.ah_1"]
        XCTAssertTrue(hold.waitForExistence(timeout: 10))
        XCTAssertTrue(hold.label.contains("release build"), hold.label)
        hold.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Explore: Check the cart tests")).firstMatch.waitForExistence(timeout: 10))
    }

    func testUsageLineOpensClaudeUsageAndSessionInfoShowsTheShares() {
        let app = launch(open: "sidebar", ["-usage", "normal", "-macs", "3"])
        XCTAssertTrue(app.staticTexts["MacBook Pro"].waitForExistence(timeout: 20))
        app.staticTexts["shop"].firstMatch.tap()
        let line = app.buttons["usage.header"]
        XCTAssertTrue(line.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: line, "label CONTAINS 'alex@acme · 5h 42% · 7d 81%'"), line.label)
        add(screenshot(app, "usage-header-accounts"))
        line.tap()
        XCTAssertTrue(app.staticTexts["Your other Macs"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["alex@home.example"].exists)
        add(screenshot(app, "claude-usage"))
        app.buttons["Close"].tap()
        XCTAssertTrue(wait(for: app.staticTexts["Your other Macs"], "exists == false"))
        app.staticTexts["Refactor the cart total into its own function"].firstMatch.tap()
        let more = app.navigationBars.buttons["More"]
        XCTAssertTrue(more.waitForExistence(timeout: 10))
        add(screenshot(app, "session-subtitle"))
        more.tap()
        app.buttons["session.info.open"].tap()
        XCTAssertTrue(app.staticTexts["Share of 5-hour window"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["≈6%"].exists)
        XCTAssertTrue(app.staticTexts["alex@acme.example"].exists)
        add(screenshot(app, "session-info"))
    }

    func testReportQueuesWhileOfflineAndListsFiles() {
        let app = launch(open: "report", ["-offline", "4"])
        let text = app.descendants(matching: .any)["report.text"]
        XCTAssertTrue(text.waitForExistence(timeout: 10))
        XCTAssertTrue(app.switches["report.mac"].exists)
        let events = app.buttons["report.file.phone/events.log"]
        XCTAssertTrue(events.waitForExistence(timeout: 5))
        XCTAssertTrue(events.isSelected)
        let outbox = app.buttons["report.file.phone/outbox.json"]
        XCTAssertFalse(outbox.isSelected)
        XCTAssertTrue(outbox.label.contains("contains what you typed"), outbox.label)
        let offline = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label CONTAINS 'is offline'"), object: app.staticTexts["report.status"])
        XCTAssertEqual(XCTWaiter().wait(for: [offline], timeout: 30), .completed)
        let send = app.buttons["report.send"]
        XCTAssertFalse(send.isEnabled)
        text.tap()
        text.typeText("Can't connect")
        send.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Problem report'")).firstMatch.waitForExistence(timeout: 10))
        add(screenshot(app, "report-queued"))
        let undo = app.buttons["Undo"]
        undo.tap()
        XCTAssertTrue(wait(for: undo, "exists == false"))
    }

    func testASentReportNoteNamesItsIDAndOpensNoSession() {
        let app = launch(open: "report")
        let text = app.descendants(matching: .any)["report.text"]
        XCTAssertTrue(text.waitForExistence(timeout: 10))
        text.tap()
        text.typeText("Lost the connection")
        app.buttons["report.send"].tap()
        let sent = app.staticTexts["Problem report sent"].firstMatch
        XCTAssertTrue(sent.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["r-fixture"].exists)
        XCTAssertFalse(app.staticTexts["Queued"].exists)
        add(screenshot(app, "report-sent-macs"))
        app.staticTexts["shop"].tap()
        XCTAssertTrue(sent.waitForExistence(timeout: 10))
        sent.tap()
        let opened = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == true"), object: app.navigationBars["Session"])
        XCTAssertEqual(XCTWaiter().wait(for: [opened], timeout: 3), .timedOut)
        add(screenshot(app, "report-sent-agents"))
    }

    func testReportOpensOverTheConnectionSheetAndCancelKeepsIt() {
        let app = launch(open: "connection")
        let run = app.buttons["connection.run"]
        XCTAssertTrue(run.waitForExistence(timeout: 10))
        app.buttons["More"].firstMatch.tap()
        app.buttons["connection.report"].tap()
        let text = app.descendants(matching: .any)["report.text"]
        XCTAssertTrue(text.waitForExistence(timeout: 5))
        app.buttons["Cancel"].tap()
        XCTAssertTrue(wait(for: text, "exists == false"))
        XCTAssertTrue(run.exists)
    }

    func testATaskLinkInAnAgentMessageOpensThatTask() {
        let app = launch(open: "agents/agt_0000000000000002")
        let link = app.links["#7"]
        for _ in 0..<8 where !link.isHittable {
            app.swipeDown()
        }
        XCTAssertTrue(link.waitForExistence(timeout: 10))
        XCTAssertFalse(app.links["#3"].exists)
        link.tap()
        XCTAssertTrue(app.staticTexts["Retry the charge step on 502"].waitForExistence(timeout: 10))
    }

    private func launch(open target: String, _ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", target] + extra
        app.launch()
        return app
    }

    private func screenshot(_ app: XCUIApplication, _ name: String) -> XCTAttachment {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        return shot
    }

    private func reveal(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<5 where !(element.exists && element.isHittable) {
            app.swipeUp()
        }
    }

    private func brightness(at point: CGPoint, in shot: XCUIScreenshot) -> CGFloat {
        guard let image = shot.image.cgImage else { return 0 }
        let scale = CGFloat(image.width) / shot.image.size.width
        var pixel = [UInt8](repeating: 0, count: 4)
        let context = CGContext(data: &pixel, width: 1, height: 1, bitsPerComponent: 8, bytesPerRow: 4, space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        context?.draw(image, in: CGRect(x: -point.x * scale, y: point.y * scale + 1 - CGFloat(image.height), width: CGFloat(image.width), height: CGFloat(image.height)))
        return CGFloat(Int(pixel[0]) + Int(pixel[1]) + Int(pixel[2])) / (3 * 255)
    }

    private func band(_ app: XCUIApplication) -> CGRect {
        let window = app.windows.firstMatch.frame
        return CGRect(x: window.minX, y: window.minY + 200, width: window.width, height: window.height - 350)
    }

    private func bringIntoView(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<60 {
            let band = band(app)
            let frame = element.frame
            if band.contains(frame) { return }
            let far = abs(frame.midY - band.midY) > 2_000
            if frame.midY < band.midY {
                app.swipeDown(velocity: far ? .fast : .slow)
            } else {
                app.swipeUp(velocity: far ? .fast : .slow)
            }
        }
    }

    private func showsImage(_ tile: XCUIElement, in app: XCUIApplication) -> Bool {
        let frame = tile.frame
        return band(app).contains(frame) && brightness(at: CGPoint(x: frame.minX + 12, y: frame.minY + 12), in: app.screenshot()) < 0.4
    }

    private func poll(_ condition: () -> Bool) -> Bool {
        let deadline = Date.now.addingTimeInterval(5)
        while Date.now < deadline {
            if condition() { return true }
            RunLoop.current.run(until: .now.addingTimeInterval(0.2))
        }
        return condition()
    }

    private func wait(for element: XCUIElement, _ predicate: String) -> Bool {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: predicate), object: element)
        return XCTWaiter().wait(for: [expectation], timeout: 5) == .completed
    }
}
