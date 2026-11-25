import Foundation

import ClubmateProtocol

public struct TranscriptFolds: Equatable, Sendable {
    public var tools: ToolExpansionState
    public var messages: Set<String>
    public var thoughts: [String: Bool]

    public init(tools: ToolExpansionState = ToolExpansionState(), messages: Set<String> = [], thoughts: [String: Bool] = [:]) {
        self.tools = tools
        self.messages = messages
        self.thoughts = thoughts
    }

    public func thought(_ id: String, open: Bool) -> Bool {
        thoughts[id] ?? open
    }

    public static func pruned(_ thoughts: [String: Bool], in transcript: SDUINode) -> [String: Bool] {
        guard let id = transcript.id, let slash = id.lastIndex(of: "/") else { return thoughts }
        let scope = id[...slash]
        let present = Set(transcript.children.compactMap(\.id))
        return thoughts.filter { !$0.key.hasPrefix(scope) || present.contains($0.key) }
    }
}

public enum TranscriptBlocks {
    public static let cardTypes: Set<String> = ["permissionCard", "request.land", "request.connectRepo", "repos", "offer", "card"]
    public static let openFile = "openLocation"

    public static func docked(in tree: SDUINode?) -> SDUINode? {
        guard let tree else { return nil }
        if ["permissionCard", "request.land", "request.connectRepo"].contains(tree.type) { return tree }
        if tree.type == "form", tree.id?.contains("/elicitation/") == true { return tree }
        if tree.type == "transcript" { return nil }
        return tree.children.lazy.compactMap(docked).first
    }

    public static func blocks(_ transcript: SDUINode, folds: TranscriptFolds, docked: SDUINode? = nil) -> [TranscriptBlock] {
        var blocks: [TranscriptBlock] = []
        for item in transcript.transcriptItems() {
            switch item {
            case let .node(child):
                if let block = block(child, folds: folds) { blocks.append(block) }
            case let .action(child):
                blocks.append(action(child, folds: folds, parent: nil))
            case let .actions(id, cards):
                let expanded = folds.tools.isExpanded(group: cards.map(\.id))
                blocks.append(TranscriptBlock(id: id, kind: .actions, folds: true, expanded: expanded))
                if expanded { blocks += cards.map { action($0, folds: folds, parent: id) } }
            }
        }
        if let docked, let id = docked.id { blocks.append(TranscriptBlock(id: id, kind: .card)) }
        return blocks
    }

    public static func file(_ node: SDUINode) -> SDUICardAction? {
        node.cardActions().first { $0.action == openFile }
    }

    public static func markdown(_ id: String, in transcript: SDUINode, docked: SDUINode? = nil) -> String? {
        if let docked, docked.id == id { return card(docked) }
        for item in transcript.transcriptItems() {
            switch item {
            case let .node(child) where child.id == id:
                return markdown(child.node)
            case let .action(child) where child.id == id:
                return tool(child.node)
            case let .actions(group, cards):
                if group == id {
                    return ([ToolActionsSummary(cards.map(\.node)).text, ""] + cards.map { "- " + ($0.node.string("title") ?? "") }).joined(separator: "\n")
                }
                if let card = cards.first(where: { $0.id == id }) { return tool(card.node) }
            default:
                continue
            }
        }
        return nil
    }

    private static func markdown(_ node: SDUINode) -> String {
        switch node.type {
        case "message": return node.string("text") ?? ""
        case "plan": return node.planEntries().map { "- [\($0.status == .completed ? "x" : " ")] \($0.content)" }.joined(separator: "\n")
        default: return card(node)
        }
    }

    private static func tool(_ node: SDUINode) -> String {
        let title = "**" + (node.string("title") ?? "") + "**"
        guard let body = node.toolBody() else { return title }
        let fence = body.format == .diff ? "```diff" : "```"
        return [title, "", fence, body.text.hasSuffix("\n") ? String(body.text.dropLast()) : body.text, "```"].joined(separator: "\n")
    }

    private static func card(_ node: SDUINode) -> String {
        var parts = ["title", "text", "command"].compactMap { node.string($0) }.filter { !$0.isEmpty }
        if node.string("title") == node.string("text"), parts.count > 1 { parts.remove(at: 1) }
        if !parts.isEmpty { return parts.joined(separator: "\n\n") }
        return node.children.map(card).filter { !$0.isEmpty }.joined(separator: "\n\n")
    }

    private static func block(_ child: SDUIChild, folds: TranscriptFolds) -> TranscriptBlock? {
        let node = child.node
        switch node.type {
        case "message":
            switch node.messageRole {
            case .user:
                let long = node.string("heard") != nil || LongMessage(node.string("text") ?? "") != nil
                return TranscriptBlock(id: child.id, kind: .prompt, folds: long, expanded: long && folds.messages.contains(child.id))
            case .thought:
                return TranscriptBlock(id: child.id, kind: .thought, folds: true, expanded: folds.thought(child.id, open: node.bool("open") == true))
            case .agent:
                return TranscriptBlock(id: child.id, kind: .answer)
            }
        case "plan":
            let fold = PlanFold(node.planEntries().map(\.status)).folds
            return TranscriptBlock(id: child.id, kind: .plan, folds: fold, expanded: fold && folds.messages.contains(child.id))
        case "button" where child.id.hasSuffix("/earlier"):
            return TranscriptBlock(id: child.id, kind: .earlier)
        case let type where cardTypes.contains(type):
            return TranscriptBlock(id: child.id, kind: .card)
        default:
            return nil
        }
    }

    private static func action(_ child: SDUIChild, folds: TranscriptFolds, parent: String?) -> TranscriptBlock {
        let node = child.node
        let folding = node.toolBody() != nil || node.bool("truncated") == true || !node.cardActions().isEmpty
        return TranscriptBlock(id: child.id, kind: .action, folds: folding, expanded: folding && folds.tools.isExpanded(card: child.id), parent: parent, file: file(node) != nil)
    }
}
