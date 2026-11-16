import Foundation
import XCTest

@testable import CompanionKit

@MainActor
final class ProblemReportTests: XCTestCase {
    private var dir: URL!

    override func setUp() {
        dir = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: dir)
    }

    private func queue() -> ActionQueue {
        ActionQueue(file: dir.appending(path: "queue.json"), reports: dir.appending(path: "reports"))
    }

    private func folder(_ id: String) -> URL {
        dir.appending(path: "reports").appending(path: id)
    }

    private let files = [
        ReportFile(name: "phone/events.log", data: Data("state online".utf8), checked: true),
        ReportFile(name: "phone/outbox.json", data: Data("[]".utf8), checked: false, note: "contains what you typed"),
    ]

    func testQueuedReportSurvivesARelaunchAndSendsItsFiles() async throws {
        let item = try queue().enqueueReport(computer: "m", text: "Can't connect", files: files, mac: true)
        XCTAssertTrue(FileManager.default.fileExists(atPath: folder(item.id).path))

        let relaunched = queue()
        let runner = ReportRunner()
        _ = await relaunched.flush(computer: "m", runner: runner, supports: { _ in false })

        XCTAssertEqual(runner.sent.count, 1)
        XCTAssertEqual(runner.sent.first?.text, "Can't connect")
        XCTAssertEqual(runner.sent.first?.mac, true)
        XCTAssertEqual(runner.sent.first?.requestId, item.id)
        XCTAssertEqual(runner.sent.first?.files.map(\.name), ["phone/events.log"])
        XCTAssertEqual(runner.sent.first?.files.map(\.data), [Data("state online".utf8)])
        XCTAssertTrue(relaunched.items.isEmpty)
        XCTAssertEqual(relaunched.notes.map(\.text), ["Problem report sent"])
        XCTAssertEqual(relaunched.notes.map(\.detail), ["r-1"])
        XCTAssertFalse(FileManager.default.fileExists(atPath: folder(item.id).path))
    }

    func testUnknownOutcomeOffersSendAgainUnderANewID() async throws {
        let queue = queue()
        let first = try queue.enqueueReport(computer: "m", text: "Lost", files: files, mac: false)
        let runner = ReportRunner(failures: [RemoteError.server(code: ServerReason.unknown, message: "outcome unknown")])
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in false })

        let failed = try XCTUnwrap(queue.items.first)
        XCTAssertEqual(failed.failure, QueuedAction.mayHaveGoneThrough)
        XCTAssertTrue(failed.canResend)

        try queue.resendReport(failed)
        let resent = try XCTUnwrap(queue.items.first)
        XCTAssertNotEqual(resent.id, first.id)
        XCTAssertFalse(FileManager.default.fileExists(atPath: folder(first.id).path))
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in false })

        XCTAssertEqual(runner.sent.map(\.requestId), [first.id, resent.id])
        XCTAssertEqual(runner.sent.last?.files.map(\.name), ["phone/events.log"])
        XCTAssertTrue(queue.items.isEmpty)
    }

    func testAReportWithNoFilesCanBeSentAgain() async throws {
        let queue = queue()
        let unticked = files.map { ReportFile(name: $0.name, data: $0.data, checked: false) }
        _ = try queue.enqueueReport(computer: "m", text: "Words only", files: unticked, mac: true)
        let runner = ReportRunner(failures: [RemoteError.server(code: ServerReason.unknown, message: "outcome unknown")])
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in false })

        try queue.resendReport(try XCTUnwrap(queue.items.first))
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in false })

        XCTAssertEqual(runner.sent.count, 2)
        XCTAssertEqual(runner.sent.last?.files.count, 0)
        XCTAssertTrue(queue.items.isEmpty)
    }

    func testALostUploadWaitsBeforeItsNextTry() async throws {
        let queue = queue()
        _ = try queue.enqueueReport(computer: "m", text: "Slow", files: files, mac: true)
        let runner = ReportRunner(failures: [RemoteError.unreachable("timed out")])
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in false })
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in false })

        XCTAssertEqual(runner.sent.count, 1)
        XCTAssertNotNil(queue.items.first?.nextTry)
        XCTAssertNil(queue.items.first?.failure)
    }

    func testAMacWithoutReportsFailsWithUpdateLine() async throws {
        let queue = queue()
        let item = try queue.enqueueReport(computer: "m", text: "Old Mac", files: files, mac: true)
        let runner = ReportRunner(reports: false)
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertTrue(runner.sent.isEmpty)
        XCTAssertEqual(queue.items.first?.failure, "Update Clubmate on Mac Studio to send reports")
        XCTAssertEqual(queue.items.first?.canResend, true)
        XCTAssertTrue(FileManager.default.fileExists(atPath: folder(item.id).path))
    }

    func testOutboxFileIsUncheckedAndInfoHasNoToken() throws {
        let computer = Computer(id: "mac1", name: "Mac Studio", hosts: ["100.64.0.1"], port: 47621, colour: "teal")
        let connection = ComputerConnection(computer: computer, token: "secret-token", transport: FakeTransport())
        connection.note(.command, "failed id=backlog.update err=server refused")
        let queue = queue()
        queue.enqueue(QueuedAction(computer: "mac1", workspace: "w", command: BacklogCommand.update, args: .object([:]), target: "task/7", title: "Edit #7", text: "typed words"))

        let files = ProblemReport.files(connection: connection, queue: queue, device: DeviceIdentity(id: "d", name: "Cali's iPhone", model: "iPhone18,1"), appVersion: "1.0+7", screenshot: Data([0xFF, 0xD8]))

        XCTAssertEqual(files.map(\.name), ["phone/info.json", "phone/events.log", "phone/connection.log", "phone/outbox.json", "phone/screenshot.jpg"])
        let outbox = try XCTUnwrap(files.first { $0.name == "phone/outbox.json" })
        XCTAssertFalse(outbox.checked)
        XCTAssertEqual(outbox.note, "contains what you typed")
        XCTAssertTrue(String(decoding: outbox.data, as: UTF8.self).contains("typed words"))
        XCTAssertEqual(files.filter(\.checked).map(\.name), ["phone/info.json", "phone/events.log", "phone/connection.log", "phone/screenshot.jpg"])
        let info = String(decoding: try XCTUnwrap(files.first).data, as: UTF8.self)
        XCTAssertTrue(info.contains("Mac Studio"), info)
        XCTAssertTrue(info.contains("iPhone18,1"), info)
        XCTAssertFalse(info.contains("secret-token"), info)
        XCTAssertTrue(String(decoding: files[1].data, as: UTF8.self).contains("failed id=backlog.update"))
    }

    func testCancelRemovesTheFolder() throws {
        let queue = queue()
        let item = try queue.enqueueReport(computer: "m", text: "Never mind", files: files, mac: false)

        queue.cancel(item)

        XCTAssertTrue(queue.items.isEmpty)
        XCTAssertFalse(FileManager.default.fileExists(atPath: folder(item.id).path))
    }
}

final class ReportRunner: CommandRunner {
    var failures: [any Error]
    let reports: Bool
    private(set) var sent: [(text: String, mac: Bool, files: [(name: String, data: Data)], requestId: String)] = []

    init(failures: [any Error] = [], reports: Bool = true) {
        self.failures = failures
        self.reports = reports
    }

    var dedupes: Bool { true }
    var macName: String { "Mac Studio" }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        .object([:])
    }

    func report(text: String, mac: Bool, files: [(name: String, data: Data)], requestId: String) async throws -> String {
        sent.append((text, mac, files, requestId))
        if !failures.isEmpty { throw failures.removeFirst() }
        return "r-\(sent.count)"
    }
}
