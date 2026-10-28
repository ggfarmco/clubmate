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
}
