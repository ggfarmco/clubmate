public enum ListKeyAction: Equatable, Sendable {
    case move(Int)
    case open
}

public enum ListKeys {
    public static func action(keyCode: UInt16, control: Bool, command: Bool, option: Bool) -> ListKeyAction? {
        guard control, !command, !option else { return nil }
        switch keyCode {
        case 38, 45: return .move(1)
        case 40, 35: return .move(-1)
        case 37: return .open
        default: return nil
        }
    }

    public static func gridArrow(keyCode: UInt16, control: Bool, command: Bool, option: Bool) -> SwitcherGrid.Arrow? {
        guard !control, !command, !option else { return nil }
        switch keyCode {
        case 4: return .left
        case 38: return .down
        case 40: return .up
        case 37: return .right
        default: return nil
        }
    }
}
