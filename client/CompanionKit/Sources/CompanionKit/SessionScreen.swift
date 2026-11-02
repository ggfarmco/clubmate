import Foundation

import ClubmateProtocol

public enum SessionState: String, Sendable, CaseIterable {
    case waitingForYou
    case askingYou
    case askingParent
    case running
    case stopped
    case review
    case done
    case closed

    public var label: String {
        switch self {
        case .waitingForYou: "Waiting for you"
        case .askingYou: "Asking you"
        case .askingParent: "Asking parent"
        case .running: "Running"
        case .stopped: "Stopped"
        case .review: "Review"
        case .done: "Done"
        case .closed: "Closed"
        }
    }

    public var needsYou: Bool {
        self == .waitingForYou || self == .askingYou
    }
}

extension AgentSession {
    public var state: SessionState {
        if isClosed { return .closed }
        if needsYou { return status == .waiting || !pending.isEmpty ? .waitingForYou : .askingYou }
        if asksParent { return .askingParent }
        if status == .running { return .running }
        if status == .error { return .stopped }
        if review { return .review }
        return .done
    }

    public var stopped: SessionFailure {
        failure ?? SessionFailure(word: SessionState.stopped.label, badge: SessionState.stopped.label + " · error", line: "")
    }

    public func failureButtons(supported: (String) -> Bool) -> [FailureAction] {
        guard status == .error else { return [] }
        return stopped.actions.filter { $0.command.map(supported) ?? false }
    }

    public func failureOnMac(_ mac: String) -> [String] {
        guard status == .error else { return [] }
        return stopped.actions.filter { $0.command == nil }.map { "\($0.label) on \(mac)" }
    }

    public var headerState: String {
        state == .stopped ? stopped.badge : state.label
    }

    public var headerDetail: String {
        parentSession == nil ? worktreeName : "subagent"
    }

    public var composerPlaceholder: String {
        switch state {
        case .askingYou: "Answer \(agentName)…"
        case _ where isTurnActive: "Queue a message"
        default: "Message \(agentName)"
        }
    }

    public func composerCaption(stopInHeader: Bool = false) -> String? {
        guard isTurnActive else { return nil }
        return stopInHeader ? "Sends after the current turn" : "Sends after the current turn · Stop is in •••"
    }

    public var sendLabel: String {
        isTurnActive ? "Queue" : "Send"
    }
}

public struct ToolGroup: Sendable, Equatable {
    public let calls: [ToolCall]

    public var title: String {
        calls.count == 1 ? calls[0].displayTitle : "\(calls.count) actions"
    }

    public var detail: String {
        calls.map { $0.displayTitle.prefix(1).lowercased() + $0.displayTitle.dropFirst() }.joined(separator: " · ")
    }

    public var changes: (added: Int, removed: Int) {
        calls.map(\.changes).reduce((0, 0)) { ($0.0 + $1.added, $0.1 + $1.removed) }
    }

    public var failed: Bool {
        calls.contains { $0.status == .failed }
    }

    public var running: Bool {
        calls.contains { $0.status.isOpen }
    }

    public var symbol: String {
        (calls.last { $0.kind.touchesFiles } ?? calls.last)?.kind.symbol ?? ToolKind.other.symbol
    }
}

public enum TranscriptRow: Sendable, Equatable, Identifiable {
    case entry(TranscriptEntry)
    case agent(id: String, blocks: [MarkdownBlock], docLinks: [String: String], docCards: [DocCard], images: [ImageRef])
    case tools(id: String, ToolGroup)
    case thought(id: String, text: String, title: String)
    case answered(PermissionRequest)
    case asked(Elicitation)

    public var id: String {
        switch self {
        case .entry(let entry): entry.id
        case .agent(let id, _, _, _, _), .tools(let id, _), .thought(let id, _, _): id
        case .answered(let request): "permission/" + request.id
        case .asked(let question): "elicitation/" + question.id
        }
    }
}

public enum SessionDock: Sendable, Equatable {
    case permission(PermissionRequest, more: Int)
    case elicitation(Elicitation)
    case question(String)
}

public struct SessionScreen: Sendable, Equatable {
    public let rows: [TranscriptRow]
    public let dock: SessionDock?

    public init(entries: [TranscriptEntry], session: AgentSession?) {
        let pending = Set(session?.pending ?? [])
        var asked: (index: Int, question: String, drop: Bool)?
        if session?.state == .askingYou, let index = entries.lastIndex(where: { if case .agent = $0.content { true } else { false } }),
           case .agent(let text) = entries[index].content {
            asked = Self.question(in: Markdown.blocks(text)).map { (index, $0.text, $0.whole) }
        }
        var rows: [TranscriptRow] = []
        var waiting: [PermissionRequest] = []
        var questions: [Elicitation] = []
        for (index, entry) in entries.enumerated() {
            switch entry.content {
            case .agent(let text):
                var blocks = Markdown.blocks(text)
                if index + 1 < entries.count, case .plan = entries[index + 1].content {
                    blocks = Self.foldPlan(blocks)
                }
                if asked?.index == index, asked?.drop == true, case .paragraph = blocks.last {
                    blocks.removeLast()
                }
                if !blocks.isEmpty || !entry.images.isEmpty { rows.append(.agent(id: entry.id, blocks: blocks, docLinks: entry.docLinks, docCards: entry.docCards, images: entry.images)) }
            case .thought(let text):
                let next = index + 1 < entries.count ? entries[index + 1].at : nil
                rows.append(.thought(id: entry.id, text: text, title: Self.thoughtTitle(from: entry.at, to: next)))
            case .tool(let call):
                if call.kind != .agent, case .tools(let id, let group) = rows.last, !group.calls.contains(where: { $0.kind == .agent }) {
                    rows[rows.count - 1] = .tools(id: id, ToolGroup(calls: group.calls + [call]))
                } else {
                    rows.append(.tools(id: entry.id, ToolGroup(calls: [call])))
                }
            case .permission(let request):
                if pending.contains(request.id) {
                    waiting.append(request)
                } else {
                    rows.append(.answered(request))
                }
            case .elicitation(let question):
                if pending.contains(question.id) {
                    questions.append(question)
                } else {
                    rows.append(.asked(question))
                }
            case .other:
                break
            default:
                rows.append(.entry(entry))
            }
        }
        self.rows = rows
        if let first = waiting.first {
            dock = .permission(first, more: waiting.count - 1)
        } else if let question = questions.first {
            dock = .elicitation(question)
        } else if let asked {
            dock = .question(asked.question)
        } else if session?.state == .askingYou {
            dock = .question("")
        } else {
            dock = nil
        }
    }

    static func thoughtTitle(from start: Date?, to end: Date?) -> String {
        guard let start, let end else { return "Thought" }
        return ShortAge.thought(Int(end.timeIntervalSince(start).rounded()))
    }

    static func foldPlan(_ blocks: [MarkdownBlock]) -> [MarkdownBlock] {
        func isList(_ block: MarkdownBlock) -> Bool {
            if case .list = block { true } else { false }
        }
        if blocks.contains(where: isList), blocks.allSatisfy({ block in
            if case .paragraph(let text) = block { return text.hasSuffix(":") && block == blocks.first }
            return isList(block)
        }) {
            return []
        }
        var out: [MarkdownBlock] = []
        var inPlan = false
        for block in blocks {
            switch block {
            case .heading(_, let text):
                inPlan = text.lowercased().contains("plan")
                if inPlan { continue }
            case .list where inPlan:
                if case .paragraph(let text) = out.last, text.hasSuffix(":") {
                    out[out.count - 1] = .paragraph(String(text.dropLast()) + ".")
                }
                continue
            default:
                break
            }
            out.append(block)
        }
        return out
    }

    static func question(in blocks: [MarkdownBlock]) -> (text: String, whole: Bool)? {
        for block in blocks.reversed() {
            let texts: [String]
            switch block {
            case .paragraph(let text), .quote(let text): texts = [text]
            case .list(let items): texts = items.map(\.text)
            case .heading(_, let text): texts = [text]
            case .code, .mermaid, .table: continue
            }
            for text in texts.reversed() {
                let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
                guard let sentence = sentences(trimmed).last(where: { $0.hasSuffix("?") }) else { continue }
                if case .paragraph = block, block == blocks.last { return (sentence, sentence == trimmed) }
                return (sentence, false)
            }
        }
        return nil
    }

    private static func sentences(_ text: String) -> [String] {
        var out: [String] = []
        var current = ""
        var previous: Character?
        for character in text {
            if let previous, ".!?\n".contains(previous), character.isWhitespace {
                out.append(current.trimmingCharacters(in: .whitespacesAndNewlines))
                current = ""
            }
            current.append(character)
            previous = character
        }
        out.append(current.trimmingCharacters(in: .whitespacesAndNewlines))
        return out.filter { !$0.isEmpty }
    }
}

public struct AlwaysConfirm: Sendable, Equatable, Identifiable {
    public let optionID: String
    public let label: String
    public let rule: String?
    public let scope: String?
    public let forSession: Bool
    public let guessed: Bool

    public var id: String { optionID }
}

extension PermissionRequest {
    public func option(_ kind: PermissionKind) -> PermissionOption? {
        options.first { $0.kind == kind }
    }

    public var fallbackButtons: [PermissionCardContent.Option] {
        let labels: [(PermissionKind, String)] = [(.allowOnce, "Allow once"), (.allowAlways, "Always allow…"), (.rejectOnce, "Reject")]
        return labels.compactMap { kind, label in option(kind).map { PermissionCardContent.Option(id: $0.optionId, label: label, kind: kind) } }
    }

    public var fallbackMenu: [PermissionCardContent.Option] {
        option(.rejectAlways).map { [PermissionCardContent.Option(id: $0.optionId, label: "Reject and don't ask again", kind: .rejectAlways, menu: true)] } ?? []
    }

    public static let keyCount = 3

    public func keyed(_ digit: Int) -> PermissionCardContent.Option? {
        if let card { return card.options.first { $0.shortcut == "opt+\(digit)" } }
        let kinds: [PermissionKind] = [.allowOnce, .allowAlways, .rejectOnce]
        guard digit >= 1, digit <= kinds.count else { return nil }
        return fallbackButtons.first { $0.kind == kinds[digit - 1] }
    }

    public var subject: String {
        (toolCall.command ?? toolCall.displayTitle).split(omittingEmptySubsequences: false, whereSeparator: \.isNewline).joined(separator: "\n")
    }

    public static let dockCommandLines = 3
    public static let dockDiffLines = 6

    public var hiddenInDock: String? {
        let extraLines = subject.split(separator: "\n", omittingEmptySubsequences: false).count - Self.dockCommandLines
        let extraFiles = toolCall.diffs.count - 1
        let extraDiff = (toolCall.diffs.first?.lines.count ?? 0) - Self.dockDiffLines
        var parts: [String] = []
        if extraLines > 0 { parts.append(extraLines == 1 ? "1 more line" : "\(extraLines) more lines") }
        if extraDiff > 0 { parts.append(extraDiff == 1 ? "1 more changed line" : "\(extraDiff) more changed lines") }
        if extraFiles > 0 { parts.append(extraFiles == 1 ? "1 more file" : "\(extraFiles) more files") }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    public var question: String {
        switch toolCall.kind {
        case .execute: "Run a command?"
        case .edit, .move: "Edit a file?"
        case .delete: "Delete a file?"
        case .fetch: "Fetch a page?"
        case .read, .search: "Read files?"
        default: "Allow \(toolCall.displayTitle)?"
        }
    }

    public var alwaysTitle: String {
        toolCall.kind == .execute ? "Always allow this command?" : "Always allow this action?"
    }

    public func alwaysConfirm(_ id: String) -> AlwaysConfirm? {
        let raw = options.first { $0.optionId == id }
        if let card {
            guard let option = card.options.first(where: { $0.id == id }), option.kind == .allowAlways else { return nil }
            let label = raw?.name ?? option.label
            return AlwaysConfirm(optionID: id, label: label, rule: option.rule, scope: option.scope, forSession: Self.forSession(label), guessed: false)
        }
        guard let raw, raw.kind == .allowAlways else { return nil }
        return AlwaysConfirm(optionID: id, label: raw.name, rule: rule(from: raw.name), scope: nil, forSession: Self.forSession(raw.name), guessed: true)
    }

    private static func forSession(_ label: String) -> Bool {
        label.lowercased().contains("during this session")
    }

    private func rule(from name: String) -> String {
        if Self.forSession(name) {
            let rest = name.hasPrefix("Yes, ") ? String(name.dropFirst(5)) : name
            return rest.prefix(1).uppercased() + rest.dropFirst()
        }
        if let range = name.range(of: "don't ask again for ") {
            let target = String(name[range.upperBound...])
            switch toolCall.kind {
            case .execute where target.hasSuffix(" commands"):
                let prefixes = Self.listed(String(target.dropLast(" commands".count)))
                if prefixes != ["similar"] { return prefixes.map { "Bash(\($0):*)" }.joined(separator: "\n") }
            case .execute:
                break
            case .fetch:
                return "WebFetch(domain:\(target))"
            default:
                return target
            }
        }
        if toolCall.kind == .execute, let word = toolCall.command?.split(whereSeparator: \.isWhitespace).first {
            return "Bash(\(word):*)"
        }
        let generic = ["always allow", "allow always", "always", "allow"]
        var rest = name.lowercased()
        for word in generic where rest.hasPrefix(word) {
            rest = String(rest.dropFirst(word.count))
            break
        }
        return rest.trimmingCharacters(in: .whitespacesAndNewlines.union(.punctuationCharacters)).isEmpty ? subject : name
    }

    private static func listed(_ text: String) -> [String] {
        let joined = text.replacingOccurrences(of: ", and ", with: ", ")
        return (joined.contains(", ") ? joined.components(separatedBy: ", ") : joined.components(separatedBy: " and ")).filter { !$0.isEmpty }
    }

    public var verdict: String {
        switch outcome?.outcome {
        case nil: return "No longer pending"
        case .cancelled: return "Cancelled"
        default:
            switch options.first(where: { $0.optionId == outcome?.optionId })?.kind {
            case .allowOnce: return "Allowed once"
            case .allowAlways: return "Always allowed"
            case .rejectOnce: return "Rejected"
            case .rejectAlways: return "Rejected for good"
            default: return answer ?? "Answered"
            }
        }
    }

    public var summary: String {
        verdict + " · " + subject
    }

    public var allowed: Bool {
        options.first { $0.optionId == outcome?.optionId }?.kind.allows ?? false
    }
}
