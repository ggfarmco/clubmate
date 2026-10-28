import Foundation
import Observation

public enum SidebarSide: String, Sendable, CaseIterable {
    case left
    case right
}

public enum CodeSection {
    public static let id = "editor-nvim"
    public static let navigationCommand = "editor-nvim.navigation"

    public static func skippedKeysMessage(_ result: JSONValue) -> String? {
        guard case .object(let fields) = result, case .array(let keys)? = fields["skipped"] else { return nil }
        let names = keys.compactMap { key -> String? in
            guard case .string(let lhs) = key, lhs.hasPrefix("<C-"), lhs.hasSuffix(">") else { return nil }
            return "Ctrl+" + lhs.dropFirst(3).dropLast().uppercased()
        }
        guard !names.isEmpty else { return nil }
        return "\(names.joined(separator: ", ")) left alone: you already map \(names.count == 1 ? "it" : "them")"
    }
}

@MainActor
@Observable
public final class AppSettings {
    public var sidebarSide: SidebarSide {
        didSet { defaults.set(sidebarSide.rawValue, forKey: Key.sidebarSide) }
    }
    public var autoHideSidebar: Bool {
        didSet { defaults.set(autoHideSidebar, forKey: Key.autoHideSidebar) }
    }
    public var interfaceFontSize: Double {
        didSet { defaults.set(interfaceFontSize, forKey: Key.interfaceFontSize) }
    }
    public var terminalFontSize: Double? {
        didSet { save(terminalFontSize, Key.terminalFontSize) }
    }
    public var navigationKeys: Bool? {
        didSet { save(navigationKeys, Key.navigationKeys) }
    }

    @ObservationIgnored private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        sidebarSide = defaults.string(forKey: Key.sidebarSide).flatMap(SidebarSide.init(rawValue:)) ?? .left
        autoHideSidebar = defaults.bool(forKey: Key.autoHideSidebar)
        interfaceFontSize = defaults.object(forKey: Key.interfaceFontSize) as? Double ?? 12
        terminalFontSize = defaults.object(forKey: Key.terminalFontSize) as? Double
        navigationKeys = defaults.object(forKey: Key.navigationKeys) as? Bool
    }

    public static let interfaceFontRange: ClosedRange<Double> = 10...20
    public static let terminalFontRange: ClosedRange<Double> = 8...32

    public static func fontSteps(current: Double, delta: Double, range: ClosedRange<Double>) -> Double {
        min(max(current + delta, range.lowerBound), range.upperBound)
    }

    public func autoHides(_ section: String) -> Bool {
        autoHideSidebar && section == CodeSection.id
    }

    public func hidesSidebar(_ section: String, manual: Bool, autoHidden: Bool) -> Bool {
        manual || (autoHidden && autoHides(section))
    }

    private func save(_ value: Any?, _ key: String) {
        if let value { defaults.set(value, forKey: key) } else { defaults.removeObject(forKey: key) }
    }

    private enum Key {
        static let sidebarSide = "sidebarSide"
        static let autoHideSidebar = "autoHideSidebar"
        static let interfaceFontSize = "interfaceFontSize"
        static let terminalFontSize = "terminalFontSize"
        static let navigationKeys = "navigationKeys"
    }
}
