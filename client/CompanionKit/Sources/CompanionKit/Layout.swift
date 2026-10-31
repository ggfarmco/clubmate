import Foundation

public enum CompanionLayout: Sendable, Equatable {
    case compact
    case regular
    case wide

    public enum Columns: Sendable, Equatable {
        case stack
        case two
        case three
    }

    public enum Column: Sendable, Equatable {
        case sidebar
        case content
        case detail
    }

    public static let regularWidth = 600.0
    public static let wideWidth = 1000.0

    public init(width: Double, compactClass: Bool = false) {
        self = compactClass || width < Self.regularWidth ? .compact : width > Self.wideWidth ? .wide : .regular
    }

    public func columns(chosen: Bool) -> Columns {
        switch self {
        case .compact: .stack
        case .regular: chosen ? .two : .three
        case .wide: .three
        }
    }

    public static func stackColumn(chosen: Bool, detail: Bool) -> Column {
        guard chosen else { return .sidebar }
        return detail ? .detail : .content
    }
}

public enum PermissionKeys {
    public static let count = 3

    public static func option(_ digit: Int, kinds: [PermissionKind]) -> Int? {
        guard digit >= 1, digit <= kinds.count, label(digit - 1, kind: kinds[digit - 1]) != nil else { return nil }
        return digit - 1
    }

    public static func label(_ index: Int, kind: PermissionKind) -> String? {
        guard index < count, kind != .allowAlways, kind != .rejectAlways else { return nil }
        return "⌥\(index + 1)"
    }
}
