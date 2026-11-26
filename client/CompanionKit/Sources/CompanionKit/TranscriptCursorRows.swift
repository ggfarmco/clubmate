import Foundation

import ClubmateProtocol

public enum TranscriptCursorRows {
    public static let dockID = "dock"

    public static func blocks(_ rows: [TranscriptRow], open: (String, Bool) -> Bool, listed: Int?, docked: Bool = false) -> [TranscriptBlock] {
        var blocks: [TranscriptBlock] = []
        for row in rows {
            switch row {
            case .entry(let entry):
                if let block = block(entry, open: open) { blocks.append(block) }
            case .agent(let id, _, _, _, _, _, _, _):
                blocks.append(TranscriptBlock(id: id, kind: .answer))
            case .handoff(let id, _, _):
                blocks.append(TranscriptBlock(id: id, kind: .answer, folds: true, expanded: open(id, false)))
            case .tools(let id, let group):
                guard group.calls.count > 1 else {
                    blocks.append(TranscriptBlock(id: id, kind: .action, folds: true, expanded: open(id, false)))
                    continue
                }
                let shown = open(id, false)
                let folds = listed.map { group.calls.count > $0 } ?? true
                blocks.append(TranscriptBlock(id: id, kind: .actions, folds: folds, expanded: folds && shown))
                let calls: [ToolCall] = switch listed {
                case nil: shown ? group.calls : []
                case let limit?: shown ? group.calls : Array(group.calls.prefix(limit))
                }
                blocks += calls.map { TranscriptBlock(id: $0.id, kind: .action, folds: true, expanded: open($0.id, false), parent: id) }
            case .held(let id, _):
                blocks.append(TranscriptBlock(id: id, kind: .action))
            case .thought(let id, _, _, let isOpen):
                blocks.append(TranscriptBlock(id: id, kind: .thought, folds: true, expanded: open(id, isOpen)))
            case .answered, .asked, .openClosed:
                blocks.append(TranscriptBlock(id: row.id, kind: .card))
            }
        }
        if docked { blocks.append(TranscriptBlock(id: dockID, kind: .card)) }
        return blocks
    }

    public static func ids(_ rows: [TranscriptRow]) -> Set<String> {
        var ids: Set<String> = []
        for row in rows {
            ids.insert(row.id)
            if case .tools(_, let group) = row { ids.formUnion(group.calls.map(\.id)) }
        }
        return ids
    }

    public static func row(of id: String, in rows: [TranscriptRow]) -> String? {
        for row in rows {
            if row.id == id { return id }
            if case .tools(let group, let tools) = row, tools.calls.contains(where: { $0.id == id }) { return group }
        }
        return nil
    }

    public static func markdown(_ id: String, in rows: [TranscriptRow]) -> String? {
        for row in rows {
            switch row {
            case .entry(let entry) where entry.id == id:
                return markdown(entry.content)
            case .agent(let rowID, let text, _, _, _, _, _, _) where rowID == id:
                return text
            case .handoff(let rowID, _, let agent) where rowID == id:
                return markdown(rowID, in: [agent])
            case .thought(let rowID, let text, _, _) where rowID == id:
                return text
            case .held(let rowID, let call) where rowID == id:
                return tool(call)
            case .tools(let rowID, let group):
                if rowID == id {
                    return group.calls.count == 1 ? tool(group.calls[0]) : ([group.title, ""] + group.calls.map { "- " + $0.title }).joined(separator: "\n")
                }
                if let call = group.calls.first(where: { $0.id == id }) { return tool(call) }
            case .answered(let request) where row.id == id:
                return request.subject
            case .asked(let question) where row.id == id:
                return question.request.message
            default:
                continue
            }
        }
        return nil
    }

    private static func block(_ entry: TranscriptEntry, open: (String, Bool) -> Bool) -> TranscriptBlock? {
        let id = entry.id
        switch entry.content {
        case .user(let text, _, _, _):
            let folds = LongMessage(text) != nil
            return TranscriptBlock(id: id, kind: .prompt, folds: folds, expanded: folds && open(id, false))
        case .voice:
            return TranscriptBlock(id: id, kind: .prompt, folds: true, expanded: open(id, false))
        case .gateMessage:
            return TranscriptBlock(id: id, kind: .prompt)
        case .agent:
            return TranscriptBlock(id: id, kind: .answer)
        case .thought:
            return TranscriptBlock(id: id, kind: .thought, folds: true, expanded: open(id, entry.open))
        case .tool:
            return TranscriptBlock(id: id, kind: .action, folds: true, expanded: open(id, false))
        case .plan(let items):
            let folds = PlanFold(items.map(\.status)).folds
            return TranscriptBlock(id: id, kind: .plan, folds: folds, expanded: folds && open(id, false))
        case .permission, .elicitation, .openItems:
            return TranscriptBlock(id: id, kind: .card)
        case .notice, .compaction, .other:
            return nil
        }
    }

    private static func markdown(_ content: TranscriptEntry.Content) -> String? {
        switch content {
        case .user(let text, _, _, _), .agent(let text), .thought(let text): text
        case .voice(let prompt): prompt.utterance
        case .tool(let call): tool(call)
        case .plan(let items): items.map { "- [\($0.status == .completed ? "x" : " ")] \($0.content)" }.joined(separator: "\n")
        case .permission(let request): request.subject
        case .elicitation(let question): question.request.message
        default: nil
        }
    }

    private static func tool(_ call: ToolCall) -> String {
        let title = "**" + call.title + "**"
        let diffs = call.diffs.map { diff in
            (["```diff", "--- " + diff.path, "+++ " + diff.path] + diff.lines.map(\.text) + ["```"]).joined(separator: "\n")
        }
        if !diffs.isEmpty { return ([title] + diffs).joined(separator: "\n\n") }
        guard !call.output.isEmpty else { return title }
        return [title, "", "```", call.output, "```"].joined(separator: "\n")
    }
}

public enum TranscriptPress: Equatable, Sendable {
    case step(TranscriptStep)
    case page(Int)
    case type
    case leader
    case help
    case answer(String)

    public static func resolve(_ characters: String, arrow: TranscriptArrow? = nil, shift: Bool) -> TranscriptPress? {
        if let arrow {
            switch arrow {
            case .down: return .step(.move(1))
            case .up: return .step(.move(-1))
            case .right: return .step(.unfold)
            case .left: return .step(.fold)
            case .enter: return .step(.toggle)
            case .space: return .page(shift ? -1 : 1)
            }
        }
        switch characters {
        case "j": return .step(.move(1))
        case "k": return .step(.move(-1))
        case "l": return .step(.unfold)
        case "h": return .step(.fold)
        case "G": return .step(.last)
        case "g": return shift ? .step(.last) : .leader
        case "i", "a": return .type
        case "?": return .help
        case "1", "2", "3", "y", "n": return .answer(characters)
        default: return nil
        }
    }
}

public enum TranscriptArrow: Sendable {
    case up
    case down
    case left
    case right
    case enter
    case space
}

public enum TranscriptLeader: Equatable, Sendable {
    case first
    case jump(Int)
    case last
    case needs(Int)
    case tree
    case composer

    public static func resolve(_ characters: String) -> TranscriptLeader? {
        if let n = Int(characters), (1...SessionJumps.limit).contains(n) { return .jump(n) }
        switch characters {
        case "g": return .first
        case "l": return .last
        case "n": return .needs(1)
        case "N": return .needs(-1)
        case "t": return .tree
        case "i": return .composer
        default: return nil
        }
    }

    public static let whichKey = "g → g first · 1–9 session · l last · n needs you · t list · i reply"
}

extension PermissionRequest {
    public func bareDigit(_ characters: String) -> Int? {
        if let digit = Int(characters) { return keyed(digit) == nil ? nil : digit }
        let kind: PermissionKind? = switch characters {
        case "y": .allowOnce
        case "n": .rejectOnce
        default: nil
        }
        guard let kind else { return nil }
        return (1...Self.keyCount).first { keyed($0)?.kind == kind }
    }
}

public struct PaneKeysRow: Sendable, Equatable {
    public let keys: String
    public let action: String
}

public enum PaneKeysTable {
    public static let transcript: [PaneKeysRow] = [
        PaneKeysRow(keys: "j k ↓ ↑", action: "Next, previous block"),
        PaneKeysRow(keys: "l → h ←", action: "Unfold, fold"),
        PaneKeysRow(keys: "↩", action: "Fold or unfold the block"),
        PaneKeysRow(keys: "gg G", action: "First block (shows earlier rows), newest block"),
        PaneKeysRow(keys: "Space ⇧Space", action: "Page down, up"),
        PaneKeysRow(keys: "⌘C", action: "Copy the block as Markdown"),
        PaneKeysRow(keys: "1 2 3 y n", action: "Answer the pending card"),
        PaneKeysRow(keys: "g1…g9 gl gn gN", action: "Session N, last session, next or previous that needs you"),
        PaneKeysRow(keys: "gt gi", action: "Session list, composer"),
        PaneKeysRow(keys: "i a", action: "Composer, the draft kept"),
        PaneKeysRow(keys: "Esc ⌃[", action: "Step out to the session list"),
        PaneKeysRow(keys: "? ⌘/", action: "These keys"),
    ]
}
