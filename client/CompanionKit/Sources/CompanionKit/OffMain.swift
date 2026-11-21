import Foundation

@MainActor
public final class OffMain {
    public static let commands = OffMain()

    private var last: [String: (id: Int, wait: @Sendable () async -> Void)] = [:]
    private var next = 0

    public init() {}

    public func run<T: Sendable, E: Error>(_ key: String, _ work: @escaping @Sendable () throws(E) -> T) async throws(E) -> T {
        next += 1
        let id = next
        let previous = last[key]?.wait
        let task = Task.detached(priority: Task.currentPriority) { () -> Result<T, E> in
            let result = Result<T, E> { () throws(E) -> T in try work() }
            await previous?()
            return result
        }
        last[key] = (id, { _ = await task.value })
        let result = await task.value
        if last[key]?.id == id { last[key] = nil }
        return try result.get()
    }
}
