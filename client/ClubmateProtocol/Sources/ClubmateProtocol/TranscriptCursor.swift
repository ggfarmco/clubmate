import Foundation

public enum TranscriptBlockKind: String, Equatable, Sendable {
    case prompt
    case thought
    case plan
    case actions
    case action
    case answer
    case card
    case earlier
}

public struct TranscriptBlock: Equatable, Sendable {
    public let id: String
    public let kind: TranscriptBlockKind
    public let folds: Bool
    public let expanded: Bool
    public let parent: String?
    public let file: Bool

    public init(id: String, kind: TranscriptBlockKind, folds: Bool = false, expanded: Bool = false, parent: String? = nil, file: Bool = false) {
        self.id = id
        self.kind = kind
        self.folds = folds
        self.expanded = expanded
        self.parent = parent
        self.file = file
    }
}

public enum TranscriptStep: Equatable, Sendable {
    case move(Int)
    case first
    case last
    case unfold
    case fold
    case toggle
}

public enum TranscriptCommand: Equatable, Sendable {
    case cursor(String)
    case expand(String)
    case collapse(String, cursor: String)
    case press(String)
    case top(String, earlier: String?)
    case bottom(String)
}

public enum TranscriptCursor {
    public static func landing(_ blocks: [TranscriptBlock]) -> String? {
        blocks.last { $0.kind != .earlier }?.id ?? blocks.last?.id
    }

    public static func move(_ id: String?, by delta: Int, in blocks: [TranscriptBlock]) -> String? {
        guard let id, let index = blocks.firstIndex(where: { $0.id == id }) else { return landing(blocks) }
        return blocks[min(max(index + delta, 0), blocks.count - 1)].id
    }

    public static func resolve(_ step: TranscriptStep, cursor: String?, in blocks: [TranscriptBlock]) -> TranscriptCommand? {
        guard !blocks.isEmpty else { return nil }
        switch step {
        case .move(let delta):
            return move(cursor, by: delta, in: blocks).map(TranscriptCommand.cursor)
        case .first:
            let earlier = blocks.first { $0.kind == .earlier }?.id
            let first = blocks.first { $0.kind != .earlier }?.id ?? blocks[0].id
            return .top(first, earlier: earlier)
        case .last:
            return landing(blocks).map(TranscriptCommand.bottom)
        default:
            break
        }
        guard let index = blocks.firstIndex(where: { $0.id == cursor }) else { return landing(blocks).map(TranscriptCommand.cursor) }
        let block = blocks[index]
        let child = blocks.indices.contains(index + 1) && blocks[index + 1].parent == block.id ? blocks[index + 1].id : nil
        switch step {
        case .unfold:
            if block.folds, !block.expanded { return .expand(block.id) }
            return child.map(TranscriptCommand.cursor)
        case .fold:
            if block.folds, block.expanded { return .collapse(block.id, cursor: block.id) }
            guard let parent = block.parent else { return nil }
            return .collapse(parent, cursor: parent)
        case .toggle:
            if block.kind == .card || block.kind == .earlier { return .press(block.id) }
            guard block.folds else { return nil }
            return block.expanded ? .collapse(block.id, cursor: block.id) : .expand(block.id)
        default:
            return nil
        }
    }

    public static func settle(_ cursor: String?, from old: [TranscriptBlock], to new: [TranscriptBlock]) -> String? {
        guard let cursor, !new.isEmpty, !new.contains(where: { $0.id == cursor }) else { return cursor }
        guard let index = old.firstIndex(where: { $0.id == cursor }) else { return landing(new) }
        if let parent = old[index].parent, new.contains(where: { $0.id == parent }) { return parent }
        let kept = Set(new.map(\.id))
        if let before = old[..<index].last(where: { kept.contains($0.id) }) { return before.id }
        return landing(new)
    }

    public static func newer(from old: [TranscriptBlock], to new: [TranscriptBlock]) -> Int {
        let seen = Set(old.map(\.id))
        guard let first = new.firstIndex(where: { $0.kind != .earlier && seen.contains($0.id) }) else { return 0 }
        return new[first...].filter { $0.parent == nil && !seen.contains($0.id) }.count
    }

    public static func follow(_ cursor: String?, from old: [TranscriptBlock], to new: [TranscriptBlock]) -> TranscriptFollow {
        guard let cursor, old.contains(where: { $0.id == cursor }), cursor != landing(old) else {
            return TranscriptFollow(cursor: landing(new), follows: true, newer: 0)
        }
        return TranscriptFollow(cursor: settle(cursor, from: old, to: new), follows: false, newer: newer(from: old, to: new))
    }
}

public struct TranscriptFollow: Equatable, Sendable {
    public let cursor: String?
    public let follows: Bool
    public let newer: Int

    public init(cursor: String?, follows: Bool, newer: Int) {
        self.cursor = cursor
        self.follows = follows
        self.newer = newer
    }
}

public enum TranscriptNote {
    public static func newer(_ count: Int) -> String {
        "↓ \(count) new · G"
    }

    public static let noFile = "No file on this block"
}
