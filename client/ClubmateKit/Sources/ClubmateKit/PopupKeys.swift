import Foundation

public enum PopupKey: Equatable, Sendable {
    case close
    case zoomIn
    case zoomOut
    case zoomReset
}

public enum PopupKeys {
    public static let escape: UInt16 = 53

    public static func key(keyCode: UInt16, characters: String?, command: Bool, control: Bool, option: Bool, shift: Bool, document: Bool, lightbox: Bool) -> PopupKey? {
        if keyCode == escape, !command, !control, !option, !shift {
            return lightbox ? nil : .close
        }
        guard command, !control, !option, let characters = characters?.lowercased() else { return nil }
        switch (characters, shift) {
        case ("w", false): return .close
        case ("=", false), ("+", _): return document ? .zoomIn : nil
        case ("-", false): return document ? .zoomOut : nil
        case ("0", false): return document ? .zoomReset : nil
        default: return nil
        }
    }
}
