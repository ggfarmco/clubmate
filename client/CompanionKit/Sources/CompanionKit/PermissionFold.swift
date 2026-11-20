import Foundation

public struct PermissionFold: Equatable, Sendable {
    public enum Input: Sendable {
        case liveEnd(below: Double)
        case composerFocused(Bool)
        case tapped
        case newRequest
    }

    public static let foldBelow = 120.0
    public static let openWithin = 24.0

    public private(set) var folded = false
    public private(set) var pulses = 0
    private var composerFocused = false
    private var held = false

    public init() {}

    public mutating func handle(_ input: Input) {
        switch input {
        case .liveEnd(let below):
            if below <= Self.openWithin {
                folded = false
                held = false
            } else if below > Self.foldBelow, !composerFocused, !held {
                folded = true
            }
        case .composerFocused(let focused):
            composerFocused = focused
            if focused { folded = false }
        case .tapped:
            folded = false
            held = true
        case .newRequest:
            if folded { pulses += 1 }
        }
    }
}
