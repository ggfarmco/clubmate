import Foundation

public struct ReadingMode: Equatable, Sendable {
    public struct Assistive: OptionSet, Sendable, Hashable {
        public let rawValue: Int

        public init(rawValue: Int) {
            self.rawValue = rawValue
        }

        public static let voiceOver = Assistive(rawValue: 1 << 0)
        public static let switchControl = Assistive(rawValue: 1 << 1)
        public static let reduceMotion = Assistive(rawValue: 1 << 2)
    }

    public enum Input: Sendable {
        case scrolled(offset: Double, delta: Double)
        case tappedText
        case escape
        case accessibility(Assistive)
    }

    public static let headerFold = 52.0
    public static let enterBelowHeader = 120.0
    public static let enterRun = 40.0
    public static let leaveRun = 24.0

    public private(set) var active = false
    private var assistive: Assistive = []
    private var run = 0.0

    public init() {}

    public mutating func handle(_ input: Input) {
        switch input {
        case .scrolled(let offset, let delta):
            run = delta == 0 || (delta > 0) == (run > 0) ? run + delta : delta
            if offset < Self.headerFold || run <= -Self.leaveRun {
                active = false
            } else if assistive.isEmpty, offset >= Self.headerFold + Self.enterBelowHeader, run >= Self.enterRun {
                active = true
            }
        case .tappedText, .escape:
            active = false
            run = 0
        case .accessibility(let assistive):
            self.assistive = assistive
            if !assistive.isEmpty { active = false }
        }
    }
}
