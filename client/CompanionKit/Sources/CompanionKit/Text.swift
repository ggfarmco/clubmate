import Foundation

import ClubmateProtocol

public enum LineDiff {
    private static let maxCells = 250_000

    public static func lines(old: String, new: String) -> [DiffLine] {
        let a = split(old)
        let b = split(new)
        guard a.count * b.count <= maxCells else {
            return a.map { DiffLine(text: $0, kind: .removed) } + b.map { DiffLine(text: $0, kind: .added) }
        }
        var table = Array(repeating: Array(repeating: 0, count: b.count + 1), count: a.count + 1)
        for i in stride(from: a.count - 1, through: 0, by: -1) {
            for j in stride(from: b.count - 1, through: 0, by: -1) {
                table[i][j] = a[i] == b[j] ? table[i + 1][j + 1] + 1 : max(table[i + 1][j], table[i][j + 1])
            }
        }
        var out: [DiffLine] = []
        var i = 0
        var j = 0
        while i < a.count || j < b.count {
            if i < a.count, j < b.count, a[i] == b[j] {
                out.append(DiffLine(text: a[i], kind: .context))
                i += 1
                j += 1
            } else if i < a.count, j == b.count || table[i + 1][j] >= table[i][j + 1] {
                out.append(DiffLine(text: a[i], kind: .removed))
                i += 1
            } else {
                out.append(DiffLine(text: b[j], kind: .added))
                j += 1
            }
        }
        return out
    }

    private static func split(_ text: String) -> [String] {
        guard !text.isEmpty else { return [] }
        var lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        if lines.last == "" { lines.removeLast() }
        return lines
    }
}
