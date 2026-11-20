import Foundation

public enum CompanionLayout: Sendable, Equatable {
    case compact
    case regular
    case wide

    public static let regularWidth = 600.0
    public static let wideWidth = 1000.0

    public init(width: Double, compactClass: Bool = false) {
        self = compactClass || width < Self.regularWidth ? .compact : width > Self.wideWidth ? .wide : .regular
    }
}
