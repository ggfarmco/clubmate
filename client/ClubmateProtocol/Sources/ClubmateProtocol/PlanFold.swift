import Foundation

public struct PlanFold: Equatable, Sendable {
    public let done: Int
    public let total: Int

    public init(_ statuses: [PlanStatus]) {
        done = statuses.filter { $0 == .completed }.count
        total = statuses.count
    }

    public var folds: Bool {
        done > 0 && done < total
    }

    public func shows(_ status: PlanStatus, showingDone: Bool) -> Bool {
        !folds || showingDone || status != .completed
    }
}
