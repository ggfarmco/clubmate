import Foundation
import Observation

public enum SidebarSide: String, Sendable, CaseIterable {
    case left
    case right
}

public enum CodeSection {
    public static let id = "editor-nvim"
    public static let chatSlot = "editor-nvim.sidebar"
    public static let stackedBelow: Double = 1100

    public static func stacksChat(width: Double) -> Bool {
        width > 0 && width < stackedBelow
    }
}

public enum FontFamily {
    public static let terminal = ""
    public static let system = ".system"
    public static let systemMonospaced = ".systemMonospaced"
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
    public var interfaceFontFamily: String {
        didSet { save(interfaceFontFamily.isEmpty ? nil : interfaceFontFamily, Key.interfaceFontFamily) }
    }
    public var ghosttyImportOffered: Bool {
        didSet { defaults.set(ghosttyImportOffered, forKey: Key.ghosttyImportOffered) }
    }
    public private(set) var paneFontSizes: [String: Double] {
        didSet { save(paneFontSizes.isEmpty ? nil : paneFontSizes, Key.paneFontSizes) }
    }
    public private(set) var paneFontFamilies: [String: String] {
        didSet { save(paneFontFamilies.isEmpty ? nil : paneFontFamilies, Key.paneFontFamilies) }
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
        interfaceFontFamily = defaults.string(forKey: Key.interfaceFontFamily) ?? FontFamily.terminal
        ghosttyImportOffered = defaults.bool(forKey: Key.ghosttyImportOffered)
        let sizes = defaults.dictionary(forKey: Key.paneFontSizes) as? [String: Double] ?? [:]
        paneFontSizes = Dictionary(sizes.map { (PaneFont.key(slot: $0.key, composer: false), $0.value) }, uniquingKeysWith: max)
        paneFontFamilies = defaults.dictionary(forKey: Key.paneFontFamilies) as? [String: String] ?? [:]
        recentsClearedAt = defaults.object(forKey: Key.recentsClearedAt) as? Date
        sectionOrder = defaults.stringArray(forKey: Key.sectionOrder)
    }

    public static let interfaceFontRange: ClosedRange<Double> = 10...20
    public static let terminalFontRange: ClosedRange<Double> = 8...32

    public static func fontSteps(current: Double, delta: Double, range: ClosedRange<Double>) -> Double {
        min(max(current + delta, range.lowerBound), range.upperBound)
    }

    public func takeLegacyTerminalFontSize() -> Double? {
        defer { defaults.removeObject(forKey: Key.terminalFontSize) }
        return defaults.object(forKey: Key.terminalFontSize) as? Double
    }

    public func paneFontSize(_ pane: String) -> Double {
        paneFontSizes[pane] ?? interfaceFontSize
    }

    public func stepPaneFont(_ pane: String, by delta: Double) {
        paneFontSizes[pane] = Self.fontSteps(current: paneFontSize(pane), delta: delta, range: Self.interfaceFontRange)
    }

    public func setPaneFontSize(_ pane: String, _ size: Double) {
        paneFontSizes[pane] = min(max(size, Self.interfaceFontRange.lowerBound), Self.interfaceFontRange.upperBound)
    }

    public func resetPaneFont(_ pane: String) {
        paneFontSizes[pane] = nil
    }

    public func paneFontFamily(_ pane: String) -> String {
        paneFontFamilies[pane] ?? interfaceFontFamily
    }

    public func setPaneFontFamily(_ pane: String, _ family: String?) {
        paneFontFamilies[pane] = family
    }

    public var chatFontSize: Double {
        get { paneFontSize(PaneFont.chat) }
        set { setPaneFontSize(PaneFont.chat, newValue) }
    }

    public var paneFontOverrideCount: Int {
        Set(paneFontSizes.keys).union(paneFontFamilies.keys).count
    }

    public func resetPaneFonts() {
        paneFontSizes = [:]
        paneFontFamilies = [:]
    }

    private func save(_ value: Any?, _ key: String) {
        if let value { defaults.set(value, forKey: key) } else { defaults.removeObject(forKey: key) }
    }

    private enum Key {
        static let sidebarSide = "sidebarSide"
        static let interfaceFontSize = "interfaceFontSize"
        static let interfaceFontFamily = "interfaceFontFamily"
        static let terminalFontSize = "terminalFontSize"
        static let ghosttyImportOffered = "ghosttyImportOffered"
        static let paneFontSizes = "paneFontSizes"
        static let paneFontFamilies = "paneFontFamilies"
        static let recentsClearedAt = "recentsClearedAt"
        static let sectionOrder = "sectionOrder"
    }
}
