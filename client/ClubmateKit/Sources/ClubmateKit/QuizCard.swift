import Foundation
import Observation

import ClubmateProtocol

@MainActor
@Observable
public final class QuizCardModel {
    public var cursor: Int?
    public private(set) var anchor: Int?
    public var bar = false
    public var full = false

    public static let peekLines = 6
    public static let peekShare = 0.45

    public init() {}

    public func at(_ excerpt: SDUIQuizExcerpt) -> Int? {
        let lines = excerpt.lines.compactMap(\.n)
        if let cursor, lines.contains(cursor) { return cursor }
        return [excerpt.cursor, excerpt.picked?.lowerBound].compactMap { $0 }.first { lines.contains($0) } ?? lines.first
    }

    public func move(_ delta: Int, extend: Bool, in excerpt: SDUIQuizExcerpt) {
        let lines = excerpt.lines.compactMap(\.n)
        guard let current = at(excerpt), let index = lines.firstIndex(of: current) else { return }
        if extend, anchor == nil { anchor = current }
        if !extend { anchor = nil }
        cursor = lines[min(max(index + delta, 0), lines.count - 1)]
    }

    public func point(at line: Int) {
        cursor = line
        anchor = nil
    }

    public func drag(from first: Int, to last: Int) {
        anchor = first
        cursor = last
    }

    public func span(_ excerpt: SDUIQuizExcerpt) -> ClosedRange<Int>? {
        guard let anchor, let current = at(excerpt), anchor != current else { return nil }
        return min(anchor, current)...max(anchor, current)
    }

    public func pick(_ excerpt: SDUIQuizExcerpt) -> ClosedRange<Int>? {
        guard let current = at(excerpt) else { return nil }
        return span(excerpt) ?? current...current
    }

    public func picked() {
        anchor = nil
    }

    public func peeks(_ excerpt: SDUIQuizExcerpt, rowHeight: CGFloat, chrome: CGFloat, pane: CGFloat) -> Bool {
        guard !full else { return false }
        let lines = excerpt.lines.count
        return lines > Self.peekLines && CGFloat(excerpt.rows.count) * rowHeight + chrome > pane * Self.peekShare
    }

    public func window(_ excerpt: SDUIQuizExcerpt) -> [SDUIQuizRow] {
        let lines = excerpt.lines
        guard lines.count > Self.peekLines, let current = at(excerpt), let index = lines.firstIndex(where: { $0.n == current }) else { return lines }
        let first = min(max(index - Self.peekLines / 2, 0), lines.count - Self.peekLines)
        return Array(lines[first..<(first + Self.peekLines)])
    }

    public static func range(_ range: ClosedRange<Int>) -> String {
        range.lowerBound == range.upperBound ? String(range.lowerBound) : "\(range.lowerBound)–\(range.upperBound)"
    }
}
