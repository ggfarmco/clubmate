import Foundation

public enum MacColour: String, Sendable, CaseIterable, Identifiable {
    case sky
    case blue
    case indigo
    case purple
    case magenta
    case graphite

    public init(token: String?) {
        self = token.flatMap(MacColour.init(rawValue:)) ?? .graphite
    }

    public var id: String { rawValue }

    public var label: String {
        rawValue.prefix(1).uppercased() + rawValue.dropFirst()
    }

    public var dark: UInt32 {
        switch self {
        case .sky: 0x5AC8FA
        case .blue: 0x3D8BFF
        case .indigo: 0x8582FF
        case .purple: 0xBF7AF0
        case .magenta: 0xF06FC8
        case .graphite: 0xA1A1A6
        }
    }

    public var light: UInt32 {
        switch self {
        case .sky: 0x0A78A8
        case .blue: 0x1F62D0
        case .indigo: 0x5450D6
        case .purple: 0x8540BF
        case .magenta: 0xB02E8C
        case .graphite: 0x636369
        }
    }
}

public struct MacTag: Codable, Sendable, Equatable, Hashable {
    public let name: String
    public let colour: String?

    public init(name: String, colour: String?) {
        self.name = name
        self.colour = colour
    }

    public var macColour: MacColour { MacColour(token: colour) }

    public var glyph: String {
        name.localizedCaseInsensitiveContains("MacBook") ? "laptopcomputer" : "desktopcomputer"
    }
}

extension Computer {
    public var tag: MacTag { MacTag(name: name, colour: colour?.isEmpty == false ? colour : nil) }
}
