import Foundation

@MainActor
public final class Coalescer<Key: Hashable, Value: Sendable> {
    private var running: [Key: Task<Value, Never>] = [:]
    private var queued: [Key: Task<Value, Never>] = [:]

    public init() {}

    public func run(_ key: Key, _ work: @escaping @MainActor () async -> Value) async -> Value {
        if let next = queued[key] { return await next.value }
        let previous = running[key]
        let task = Task { [weak self] in
            if let previous {
                _ = await previous.value
                self?.queued[key] = nil
            }
            return await work()
        }
        running[key] = task
        if previous != nil { queued[key] = task }
        let value = await task.value
        if running[key] == task { running[key] = nil }
        return value
    }
}
