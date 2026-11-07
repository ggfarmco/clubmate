import Foundation
import Observation

import ClubmateProtocol

public struct SDUIConnectRepo: Sendable, Equatable, Identifiable {
    public let key: String
    public let name: String
    public let path: String?
    public let remote: String?
    public let state: String?
    public let warn: Bool
    public let clone: Bool
    public let reason: String?
    public let landsBy: String?
    public let source: String?
    public let brings: String?
    public let trust: String?
    public let folded: Bool

    public var id: String { key }

    public init(key: String, name: String, path: String? = nil, clone: Bool = false, folded: Bool = false) {
        self.key = key
        self.name = name
        self.path = path
        self.clone = clone
        self.folded = folded
        (remote, state, reason, landsBy, source, brings, trust) = (nil, nil, nil, nil, nil, nil, nil)
        warn = false
    }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let key = fields["key"]?.stringValue, let name = fields["name"]?.stringValue else { return nil }
        let text = { (field: String) in fields[field]?.stringValue.flatMap { $0.isEmpty ? nil : $0 } }
        self.key = key
        self.name = name
        path = text("path")
        remote = text("remote")
        state = text("state")
        warn = fields["warn"]?.boolValue ?? false
        clone = fields["clone"]?.boolValue ?? false
        reason = text("reason")
        landsBy = text("landsBy")
        source = text("source")
        brings = text("brings")
        trust = text("trust")
        folded = fields["folded"]?.boolValue ?? false
    }
}

public struct SDUIConnectEffect: Sendable, Equatable, Identifiable {
    public let text: String
    public let note: String?

    public var id: String { text }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let text = fields["text"]?.stringValue else { return nil }
        self.text = text
        note = fields["note"]?.stringValue
    }
}

extension SDUINode {
    public func connectRepos() -> [SDUIConnectRepo] {
        props["repos"]?.arrayValue?.compactMap(SDUIConnectRepo.init) ?? []
    }

    public func connectEffects() -> [SDUIConnectEffect] {
        props["effects"]?.arrayValue?.compactMap(SDUIConnectEffect.init) ?? []
    }
}

@MainActor
@Observable
public final class ConnectCardModel {
    public var cursor = 0
    public private(set) var unticked: Set<String> = []
    public private(set) var others: [String: String] = [:]
    public var unfolded = false

    public init() {}

    public func visible(_ repos: [SDUIConnectRepo]) -> [SDUIConnectRepo] {
        unfolded ? repos : repos.filter { !$0.folded }
    }

    public func ticked(_ repos: [SDUIConnectRepo]) -> Int {
        repos.filter { !unticked.contains($0.key) }.count
    }

    public func toggle(_ repos: [SDUIConnectRepo]) {
        let at = min(cursor, repos.count - 1)
        guard at >= 0 else { return }
        if unticked.remove(repos[at].key) == nil { unticked.insert(repos[at].key) }
    }

    public func move(_ delta: Int, in repos: [SDUIConnectRepo]) {
        guard !repos.isEmpty else { return }
        cursor = max(0, min(repos.count - 1, cursor + delta))
        if repos[cursor].folded { unfolded = true }
    }

    public func choose(other path: String, for key: String) {
        others[key] = path
        unticked.remove(key)
    }

    public func answers(_ repos: [SDUIConnectRepo]) -> [JSONValue] {
        repos.map { repo in
            if unticked.contains(repo.key) { return Self.answer(repo.key, "skip") }
            guard let other = others[repo.key] else { return Self.answer(repo.key, "connect") }
            return Self.answer(repo.key, "other", other: other)
        }
    }

    public func declined(_ repos: [SDUIConnectRepo]) -> [JSONValue] {
        repos.map { Self.answer($0.key, "skip") }
    }

    private static func answer(_ key: String, _ action: String, other: String? = nil) -> JSONValue {
        var fields: [String: JSONValue] = ["path": .string(key), "action": .string(action)]
        if let other { fields["otherPath"] = .string(other) }
        return .object(fields)
    }
}

public enum CardInline {
    public enum Style: Sendable, Equatable {
        case plain
        case strong
        case code
    }

    public static func spans(_ text: String) -> [(text: String, style: Style)] {
        var out: [(text: String, style: Style)] = []
        var rest = Substring(text)
        var plain = ""
        while let at = rest.firstIndex(where: { $0 == "`" || $0 == "*" }) {
            let tail = rest[at...]
            let (mark, style): (String, Style) = tail.hasPrefix("`") ? ("`", .code) : ("**", .strong)
            guard tail.hasPrefix(mark), let end = tail.dropFirst(mark.count).range(of: mark), tail.dropFirst(mark.count).startIndex < end.lowerBound else {
                plain += rest[..<rest.index(after: at)]
                rest = rest[rest.index(after: at)...]
                continue
            }
            plain += rest[..<at]
            if !plain.isEmpty { out.append((plain, .plain)) }
            plain = ""
            out.append((String(tail.dropFirst(mark.count)[..<end.lowerBound]), style))
            rest = tail[end.upperBound...]
        }
        plain += rest
        if !plain.isEmpty { out.append((plain, .plain)) }
        return out
    }
}
