import Foundation
import Observation

public enum SidebarSide: String, Sendable, CaseIterable {
    case left
    case right
}

public enum CodeSection {
    public static let id = "editor-nvim"
    public static let chatSlot = "editor-nvim.sidebar"
}

@MainActor
@Observable
public final class AppSettings {
    public var sidebarSide: SidebarSide {
        didSet { defaults.set(sidebarSide.rawValue, forKey: Key.sidebarSide) }
    }
    public var interfaceFontSize: Double {
        didSet { defaults.set(interfaceFontSize, forKey: Key.interfaceFontSize) }
    }
    public var terminalFontSize: Double? {
        didSet { save(terminalFontSize, Key.terminalFontSize) }
    }
    public var recentsClearedAt: Date? {
        didSet { save(recentsClearedAt, Key.recentsClearedAt) }
    }
    public var sectionOrder: [String]? {
        didSet { save(sectionOrder, Key.sectionOrder) }
    }

    @ObservationIgnored private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        sidebarSide = defaults.string(forKey: Key.sidebarSide).flatMap(SidebarSide.init(rawValue:)) ?? .left
        interfaceFontSize = defaults.object(forKey: Key.interfaceFontSize) as? Double ?? 12
        terminalFontSize = defaults.object(forKey: Key.terminalFontSize) as? Double
        recentsClearedAt = defaults.object(forKey: Key.recentsClearedAt) as? Date
        sectionOrder = defaults.stringArray(forKey: Key.sectionOrder)
    }

    public static let interfaceFontRange: ClosedRange<Double> = 10...20
    public static let terminalFontRange: ClosedRange<Double> = 8...32

    public static func fontSteps(current: Double, delta: Double, range: ClosedRange<Double>) -> Double {
        min(max(current + delta, range.lowerBound), range.upperBound)
    }

    private func save(_ value: Any?, _ key: String) {
        if let value { defaults.set(value, forKey: key) } else { defaults.removeObject(forKey: key) }
    }

    private enum Key {
        static let sidebarSide = "sidebarSide"
        static let interfaceFontSize = "interfaceFontSize"
        static let terminalFontSize = "terminalFontSize"
        static let recentsClearedAt = "recentsClearedAt"
        static let sectionOrder = "sectionOrder"
    }
}
