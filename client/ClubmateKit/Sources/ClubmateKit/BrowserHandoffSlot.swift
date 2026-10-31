import Foundation

@MainActor
public final class BrowserHandoffSlot<Item: AnyObject> {
    public private(set) var current: Item?
    private var continuation: CheckedContinuation<Bool, Never>?

    public init() {}

    public func wait(for item: Item) async -> Bool {
        finish(false)
        current = item
        return await withCheckedContinuation { continuation = $0 }
    }

    @discardableResult
    public func finish(_ done: Bool) -> Item? {
        guard let item = current else { return nil }
        current = nil
        let waiting = continuation
        continuation = nil
        waiting?.resume(returning: done)
        return item
    }

    @discardableResult
    public func finish(_ done: Bool, ifShowing item: Item) -> Bool {
        guard current === item else { return false }
        finish(done)
        return true
    }
}
