import Foundation
import Observation

public enum StartupWait: Sendable {
    case core
    case workspace
}

@MainActor
@Observable
public final class StartupWatch {
    public enum Stage: Sendable {
        case quiet
        case slow
        case long
    }

    public static let slowAfter: Duration = .seconds(5)
    public static let longAfter: Duration = .seconds(15)

    public private(set) var stage = Stage.quiet
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void

    public init(sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.sleep = sleep
    }

    public func run() async {
        stage = .quiet
        do {
            try await sleep(Self.slowAfter)
            stage = .slow
            try await sleep(Self.longAfter - Self.slowAfter)
            stage = .long
        } catch {}
    }

    public func label(_ wait: StartupWait) -> String? {
        switch stage {
        case .quiet: nil
        case .slow: wait == .core ? "Starting core…" : "Connecting to workspace…"
        case .long: "Still starting core…"
        }
    }

    public var showsLog: Bool {
        stage == .long
    }
}
