import Foundation

public enum PopupKey: Equatable, Sendable {
    case close
    case switchFocus
    case zoomIn
    case zoomOut
    case zoomReset
    case find
    case findNext
    case findPrevious
    case closeFind
}

public enum PopupKeys {
    public static let escape: UInt16 = 53
    public static let returnKey: UInt16 = 36

    public static func key(keyCode: UInt16, characters: String?, command: Bool, control: Bool, option: Bool, shift: Bool, document: Bool, lightbox: Bool, finding: Bool = false) -> PopupKey? {
        if keyCode == escape, !command, !control, !option, !shift {
            return lightbox ? nil : finding ? .closeFind : .close
        }
        if keyCode == returnKey, shift, finding, !command, !control, !option {
            return .findPrevious
        }
        guard command, !control, !option, let characters = characters?.lowercased() else { return nil }
        switch (characters, shift) {
        case ("w", false): return .close
        case ("j", false): return .switchFocus
        case ("=", false), ("+", _): return document ? .zoomIn : nil
        case ("-", false): return document ? .zoomOut : nil
        case ("0", false): return document ? .zoomReset : nil
        case ("f", false): return document ? .find : nil
        case ("g", false): return document && finding ? .findNext : nil
        case ("g", true): return document && finding ? .findPrevious : nil
        default: return nil
        }
    }
}
