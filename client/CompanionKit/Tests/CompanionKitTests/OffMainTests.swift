import Foundation
import Synchronization
import XCTest

import CompanionKit

@MainActor
final class OffMainTests: XCTestCase {
    private var landed: [String] = []

    func testLaterResultLandsAfterSlowerEarlierOne() async throws {
        let lane = OffMain()
        let started = Mutex(false)
        let release = DispatchSemaphore(value: 0)
        let slow = Task {
            landed.append(await lane.run("list") {
                started.withLock { $0 = true }
                release.wait()
                return "earlier"
            })
        }
        try await waitUntil { started.withLock { $0 } }
        let done = Mutex(false)
        let fast = Task {
            landed.append(await lane.run("list") {
                done.withLock { $0 = true }
                return "later"
            })
        }
        try await waitUntil { done.withLock { $0 } }
        release.signal()
        await slow.value
        await fast.value
        XCTAssertEqual(landed, ["earlier", "later"])
    }

    func testASlowChainDoesNotHoldAnotherKey() async throws {
        let lane = OffMain()
        let started = Mutex(false)
        let release = DispatchSemaphore(value: 0)
        defer { release.signal() }
        let slow = Task {
            landed.append(await lane.run("transcript/a") {
                started.withLock { $0 = true }
                release.wait()
                return "transcript"
            })
        }
        try await waitUntil { started.withLock { $0 } }
        Task { landed.append(await lane.run("sessions") { "sessions" }) }
        try await waitUntil { landed == ["sessions"] }
        release.signal()
        await slow.value
        XCTAssertEqual(landed, ["sessions", "transcript"])
    }
}
