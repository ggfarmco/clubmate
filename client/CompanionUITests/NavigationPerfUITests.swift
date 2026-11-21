import OSLog
import XCTest

@MainActor
final class NavigationPerfUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"
    private static let parent = (id: "agt_0000000000000004", seed: 99)
    private static let child = (id: "agt_0000000000000901", seed: 1)
    private static let long = [(id: "agt_0000000000000900", seed: 0), (id: "agt_0000000000000902", seed: 2), (id: "agt_0000000000000903", seed: 3), (id: "agt_0000000000000904", seed: 4), (id: "agt_0000000000000906", seed: 6), (id: "agt_0000000000000907", seed: 7), (id: "agt_0000000000000908", seed: 8)]
    private static let iterations = Int(ProcessInfo.processInfo.environment["PERF_ITERATIONS"] ?? "") ?? 6
    private static let log = Logger(subsystem: "com.clubmate.companion.perf", category: "step")

    private var latency: String {
        ProcessInfo.processInfo.environment["PERF_LATENCY"] ?? "120"
    }

    private var jitter: String {
        ProcessInfo.processInfo.environment["PERF_JITTER"] ?? "0"
    }

    override func setUpWithError() throws {
        continueAfterFailure = false
        try XCTSkipUnless(ProcessInfo.processInfo.environment["PERF_NAV"] != nil, "run with TEST_RUNNER_PERF_NAV=1")
    }

    func testPushListToTranscript() {
        let app = launch(open: "tab:agents")
        let (first, second) = (Self.long[0], Self.long[1])
        XCTAssertTrue(row(first.id, in: app).waitForExistence(timeout: 20))
        open(first, in: app)
        back(to: first.id, in: app)
        var turn = 0
        measure(metrics: metrics(app), options: manual) {
            let target = turn % 2 == 0 ? second : first
            turn += 1
            startMeasuring()
            open(target, in: app)
            stopMeasuring()
            back(to: target.id, in: app)
        }
    }

    func testPushColdTranscript() {
        let app = launch(open: "tab:agents")
        XCTAssertTrue(row(Self.long[0].id, in: app).waitForExistence(timeout: 20))
        var turn = 0
        measure(metrics: metrics(app), options: manual) {
            let target = Self.long[turn % Self.long.count]
            turn += 1
            startMeasuring()
            open(target, in: app)
            stopMeasuring()
            back(to: target.id, in: app)
        }
    }

    func testPopTranscriptToList() {
        let app = launch(open: "tab:agents")
        let target = Self.long[0]
        XCTAssertTrue(row(target.id, in: app).waitForExistence(timeout: 20))
        measure(metrics: metrics(app), options: manual) {
            open(target, in: app)
            startMeasuring()
            back(to: target.id, in: app)
            stopMeasuring()
        }
    }

    func testSwipeBackSlow() {
        backMoves("swipeslow") { app in
            self.edgeDrag(app, to: 0.85, velocity: XCUIGestureVelocity(rawValue: 220))
        }
    }

    func testSwipeBackFast() {
        backMoves("swipefast") { app in
            self.edgeDrag(app, to: 0.7, velocity: XCUIGestureVelocity(rawValue: 1500))
        }
    }

    func testBackButton() {
        backMoves("backbutton") { app in
            app.navigationBars.buttons.element(boundBy: 0).tap()
        }
    }

    func testSessionToSession() {
        let app = launch(open: "agents/" + Self.child.id)
        XCTAssertTrue(last(Self.child.seed, in: app).waitForExistence(timeout: 20))
        measure(metrics: metrics(app), options: manual) {
            let card = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Started by'")).firstMatch
            XCTAssertTrue(card.waitForExistence(timeout: 10))
            for _ in 0..<20 {
                if card.isHittable { break }
                app.swipeDown(velocity: .fast)
            }
            startMeasuring()
            Self.log.info("step switch begin \(Self.parent.id, privacy: .public)")
            card.tap()
            XCTAssertTrue(last(Self.parent.seed, in: app).waitForExistence(timeout: 20))
            Self.log.info("step switch end \(Self.parent.id, privacy: .public)")
            stopMeasuring()
            Self.log.info("step pop begin \(Self.parent.id, privacy: .public)")
            app.navigationBars.buttons.element(boundBy: 0).tap()
            XCTAssertTrue(last(Self.child.seed, in: app).waitForExistence(timeout: 20))
            Self.log.info("step pop end \(Self.parent.id, privacy: .public)")
        }
    }

    func testTabSwitch() {
        let app = launch(open: "tab:agents", extra: ["-wiki", "YES"])
        let tabs = app.tabBars.firstMatch
        XCTAssertTrue(row(Self.long[0].id, in: app).waitForExistence(timeout: 20))
        let stops: [(tab: String, shown: XCUIElement)] = [
            ("Backlog", app.navigationBars["Backlog"]),
            ("Wiki", app.buttons["Checkout flow"].firstMatch),
            ("Agents", row(Self.long[0].id, in: app)),
        ]
        for stop in stops {
            tabs.buttons[stop.tab].tap()
            XCTAssertTrue(stop.shown.waitForExistence(timeout: 20))
        }
        measure(metrics: metrics(app), options: manual) {
            startMeasuring()
            for stop in stops {
                Self.log.info("step tab begin \(stop.tab, privacy: .public)")
                tabs.buttons[stop.tab].tap()
                XCTAssertTrue(stop.shown.waitForExistence(timeout: 20))
                Self.log.info("step tab end \(stop.tab, privacy: .public)")
            }
            stopMeasuring()
        }
    }

    func testWikiPageToPage() {
        let app = launch(open: "tab:wiki", extra: ["-wiki", "YES"])
        let pages = [(title: "Checkout flow", text: "Checkout flow part 1"), (title: "Shop wiki", text: "Start with the")]
        XCTAssertTrue(app.buttons[pages[0].title].firstMatch.waitForExistence(timeout: 20))
        var turn = 0
        measure(metrics: metrics(app), options: manual) {
            let page = pages[turn % pages.count]
            turn += 1
            startMeasuring()
            Self.log.info("step wiki begin \(page.title, privacy: .public)")
            app.buttons[page.title].firstMatch.tap()
            XCTAssertTrue(app.webViews.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", page.text)).firstMatch.waitForExistence(timeout: 20))
            Self.log.info("step wiki end \(page.title, privacy: .public)")
            stopMeasuring()
            Self.log.info("step wikipop begin \(page.title, privacy: .public)")
            app.navigationBars[page.title].buttons.firstMatch.tap()
            XCTAssertTrue(app.buttons[pages[turn % pages.count].title].firstMatch.waitForExistence(timeout: 20))
            Self.log.info("step wikipop end \(page.title, privacy: .public)")
        }
    }

    func testSpinnerShots() {
        let app = launch(open: "tab:agents", latency: "1500")
        let target = Self.long[0]
        XCTAssertTrue(row(target.id, in: app).waitForExistence(timeout: 30))
        row(target.id, in: app).tap()
        shot(app, "cold-push-300ms", after: 0.3)
        XCTAssertTrue(last(target.seed, in: app).waitForExistence(timeout: 30))
        back(to: target.id, in: app)
        row(target.id, in: app).tap()
        shot(app, "warm-push-300ms", after: 0.3)
    }

    func testWikiShots() {
        let app = launch(open: "tab:wiki", latency: "3000", extra: ["-wiki", "YES"])
        let checkout = app.buttons["Checkout flow"].firstMatch
        XCTAssertTrue(checkout.waitForExistence(timeout: 30))
        checkout.tap()
        shot(app, "wiki-cold-300ms", after: 0.3)
        XCTAssertTrue(app.webViews.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Checkout flow part 1")).firstMatch.waitForExistence(timeout: 30))
        shot(app, "wiki-cold-loaded", after: 0)
        app.navigationBars["Checkout flow"].buttons.firstMatch.tap()
        app.buttons["Shop wiki"].firstMatch.tap()
        XCTAssertTrue(app.webViews.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Start with the")).firstMatch.waitForExistence(timeout: 30))
        app.navigationBars["Shop wiki"].buttons.firstMatch.tap()
        checkout.tap()
        shot(app, "wiki-revisit-300ms", after: 0.3)
        XCTAssertTrue(app.staticTexts["Updating…"].exists)
        shot(app, "wiki-revisit-1s", after: 0.7)
        XCTAssertTrue(app.staticTexts["Updating…"].waitForNonExistence(timeout: 10))
        shot(app, "wiki-revisit-fresh", after: 0)
    }

    private func backMoves(_ kind: String, move: (XCUIApplication) -> Void) {
        let app = launch(open: "tab:agents", extra: ["-frameMonitor", "YES"])
        let target = Self.long[0]
        XCTAssertTrue(row(target.id, in: app).waitForExistence(timeout: 20))
        for _ in 0..<Self.iterations {
            open(target, in: app)
            Self.log.info("step \(kind, privacy: .public) begin \(target.id, privacy: .public)")
            move(app)
            Self.log.info("step \(kind, privacy: .public) end \(target.id, privacy: .public)")
            XCTAssertTrue(row(target.id, in: app).waitForExistence(timeout: 20))
        }
    }

    private func edgeDrag(_ app: XCUIApplication, to x: CGFloat, velocity: XCUIGestureVelocity) {
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0.5)).withOffset(CGVector(dx: 1, dy: 0))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: x, dy: 0.5))
        start.press(forDuration: 0.05, thenDragTo: end, withVelocity: velocity, thenHoldForDuration: 0)
    }

    private var manual: XCTMeasureOptions {
        let options = XCTMeasureOptions()
        options.invocationOptions = [.manuallyStart, .manuallyStop]
        options.iterationCount = Self.iterations
        return options
    }

    private func metrics(_ app: XCUIApplication) -> [any XCTMetric] {
        [XCTClockMetric(), XCTOSSignpostMetric.navigationTransitionMetric, XCTHitchMetric(application: app), XCTCPUMetric(application: app)]
    }

    private func launch(open target: String, latency: String? = nil, extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-latency", latency ?? self.latency, "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", target, "-perfFixture", "YES", "-latencyJitter", jitter] + extra + (ProcessInfo.processInfo.environment["PERF_ARGS"]?.split(separator: " ").map(String.init) ?? [])
        if ProcessInfo.processInfo.environment["PERF_ATTACH"] != nil {
            app.activate()
        } else {
            app.launch()
        }
        return app
    }

    private func row(_ id: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any)["row." + id]
    }

    private func last(_ seed: Int, in app: XCUIApplication) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "of \(seed). Tests pass")).firstMatch
    }

    private func open(_ target: (id: String, seed: Int), in app: XCUIApplication) {
        let element = row(target.id, in: app)
        if !element.isHittable { app.swipeUp() }
        Self.log.info("step push begin \(target.id, privacy: .public)")
        element.tap()
        XCTAssertTrue(last(target.seed, in: app).waitForExistence(timeout: 20))
        Self.log.info("step push end \(target.id, privacy: .public)")
    }

    private func back(to id: String, in app: XCUIApplication) {
        Self.log.info("step pop begin \(id, privacy: .public)")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(row(id, in: app).waitForExistence(timeout: 20))
        Self.log.info("step pop end \(id, privacy: .public)")
    }

    private func shot(_ app: XCUIApplication, _ name: String, after delay: TimeInterval) {
        Thread.sleep(forTimeInterval: delay)
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
