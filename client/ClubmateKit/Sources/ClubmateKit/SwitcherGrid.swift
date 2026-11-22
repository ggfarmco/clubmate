public enum SwitcherGrid {
    public enum Arrow: Sendable {
        case left, right, up, down
    }

    public static func move(highlight: Int, count: Int, columns: Int, key: Arrow) -> Int {
        guard columns > 0 else { return highlight }
        let row = highlight / columns
        let column = highlight % columns
        let (targetRow, targetColumn) = switch key {
        case .left: (row, column - 1)
        case .right: (row, column + 1)
        case .up: (row - 1, column)
        case .down: (row + 1, column)
        }
        let target = targetRow * columns + targetColumn
        guard targetRow >= 0, (0..<columns).contains(targetColumn), target <= count else { return highlight }
        return target
    }

    public static func pickerMove(cursor: Int, count: Int, key: Arrow) -> Int {
        let target = switch key {
        case .left: cursor == 1 ? 0 : cursor
        case .right: cursor == 0 ? 1 : cursor
        case .up: cursor == 2 ? 0 : cursor > 2 ? cursor - 1 : cursor
        case .down: cursor < 2 ? 2 : cursor + 1
        }
        return (0..<count).contains(target) ? target : cursor
    }
}
