import Foundation
import Synchronization

import ClubmateProtocol

public enum MarkdownCache {
    static let limit = 2048

    private struct InlineKey: Hashable {
        let text: String
        let docLinks: [String: String]
        let imageLinks: [String: String]
        let taskLinks: [String: String]
    }

    private struct Parsed {
        var blocks: [String: [MarkdownBlock]] = [:]
        var inline: [InlineKey: AttributedString] = [:]
    }

    private static let parsed = Mutex(Parsed())

    public static func blocks(_ text: String) -> [MarkdownBlock] {
        if let hit = parsed.withLock({ $0.blocks[text] }) { return hit }
        let blocks = Markdown.blocks(text)
        parsed.withLock { cache in
            if cache.blocks.count >= limit { cache.blocks.removeAll(keepingCapacity: true) }
            cache.blocks[text] = blocks
        }
        return blocks
    }

    public static func inline(_ text: String, docLinks: [String: String] = [:], imageLinks: [String: String] = [:], taskLinks: [String: String] = [:]) -> AttributedString {
        let key = InlineKey(text: text, docLinks: docLinks, imageLinks: imageLinks, taskLinks: taskLinks)
        if let hit = parsed.withLock({ $0.inline[key] }) { return hit }
        let content = Markdown.inline(text, docLinks: docLinks, imageLinks: imageLinks, taskLinks: taskLinks)
        parsed.withLock { cache in
            if cache.inline.count >= limit { cache.inline.removeAll(keepingCapacity: true) }
            cache.inline[key] = content
        }
        return content
    }
}
