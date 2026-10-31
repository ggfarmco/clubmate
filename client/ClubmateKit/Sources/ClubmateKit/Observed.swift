import Foundation
import Observation

public enum Observed {
    @MainActor
    public static func first<Value: Sendable>(within timeout: Duration, _ read: @escaping @MainActor @Sendable () -> Value?) async -> Value? {
        let waiting = Task { @MainActor () -> Value? in
            for await value in Observations(read) {
                if let value { return value }
            }
            return nil
        }
        let deadline = Task {
            try await Task.sleep(for: timeout)
            waiting.cancel()
        }
        defer { deadline.cancel() }
        return await withTaskCancellationHandler {
            await waiting.value
        } onCancel: {
            waiting.cancel()
        }
    }
}
