import Foundation
import Observation

public enum SidebarSide: String, Sendable, CaseIterable {
    case left
    case right
}

public enum CodeSection {
    public static let id = "editor-nvim"
    public static let navigationCommand = "editor-nvim.navigation"
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
