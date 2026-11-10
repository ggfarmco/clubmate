import XCTest

import ClubmateKit

@MainActor
final class CoalescerTests: XCTestCase {
    func testCallsDuringARunShareOneRunThatStartsAfterIt() async throws {
        let coalescer = Coalescer<String, Int>()
        var runs = 0
        var gate: CheckedContinuation<Void, Never>?
        let work: @MainActor () async -> Int = {
            runs += 1
            let run = runs
            if run == 1 { await withCheckedContinuation { gate = $0 } }
            return run
        }
        let first = Task { await coalescer.run("w", work) }
        try await waitUntil { gate != nil }
        let second = Task { await coalescer.run("w", work) }
        let third = Task { await coalescer.run("w", work) }
        let other = await coalescer.run("other", { 7 })
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(runs, 1)
        gate?.resume()
        let results = await [first.value, second.value, third.value]
        XCTAssertEqual(results, [1, 2, 2])
        XCTAssertEqual(other, 7)
        XCTAssertEqual(runs, 2)
    }
}
