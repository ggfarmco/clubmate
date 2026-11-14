import Foundation
import Observation

public struct DocPage: Decodable, Equatable, Sendable {
    public let path: String
    public let title: String
    public let html: String
    public let rev: Int
    public let kind: String?
    public var anchor: String?

    public init(path: String, title: String, html: String, rev: Int, kind: String? = nil, anchor: String? = nil) {
        self.path = path
        self.title = title
        self.html = html
        self.rev = rev
        self.kind = kind
        self.anchor = anchor
    }

    public var isArtifact: Bool {
        kind == "artifact"
    }
}

@MainActor
@Observable
public final class DocsStore {
    public let workspace: String
    public private(set) var stack: [DocPage] = []
    public private(set) var error: String?

    @ObservationIgnored private weak var connection: ComputerConnection?
    @ObservationIgnored private var latest: [String: Int] = [:]

    public init(connection: ComputerConnection, workspace: String) {
        self.connection = connection
        self.workspace = workspace
    }

    public func open(_ link: String) async {
        let (path, anchor) = Self.split(link)
        if let last = stack.indices.last, stack[last].path == path {
            stack[last].anchor = anchor
            return
        }
        guard var page = await read(path) else { return }
        page.anchor = anchor
        stack.append(page)
    }

    public func back() {
        guard stack.count > 1 else { return }
        stack.removeLast()
        error = nil
        if let top = stack.last, let rev = latest[top.path], rev > top.rev {
            Task { await changed(path: top.path, rev: rev) }
        }
    }

    public func changed(path: String, rev: Int) async {
        latest[path] = max(latest[path] ?? 0, rev)
        guard let top = stack.last, top.path == path, rev > top.rev, var page = await read(path) else { return }
        guard let last = stack.indices.last, stack[last].path == page.path, page.rev > stack[last].rev else { return }
        page.anchor = stack[last].anchor
        stack[last] = page
    }

    public func handle(_ signal: ConnectionSignal) {
        guard case .event(let event) = signal, event.topic == Topic.docsChanged else { return }
        let target = event.payload["workspace"]?.stringValue ?? event.workspace
        guard target.isEmpty || target == workspace, let path = event.payload["path"]?.stringValue, let rev = event.payload["rev"]?.numberValue else { return }
        Task { await changed(path: path, rev: Int(rev)) }
    }

    private static func split(_ link: String) -> (String, String?) {
        guard let hash = link.lastIndex(of: "#"), [".md", ".markdown", ".mmd", ".html", ".htm"].contains(where: { link[..<hash].lowercased().hasSuffix($0) }) else { return (link, nil) }
        return (String(link[..<hash]), String(link[link.index(after: hash)...]))
    }

    private func read(_ path: String) async -> DocPage? {
        guard let connection else { return nil }
        do {
            let page = try await connection.command(DocsCommand.read, workspace: workspace, args: .object(["path": .string(path)]), as: DocPage.self)
            error = nil
            return page
        } catch {
            self.error = (error as? RemoteError)?.message ?? error.localizedDescription
            return nil
        }
    }
}

public struct DocEntry: Decodable, Equatable, Sendable, Identifiable {
    public let path: String
    public let rel: String
    public let kind: String
    public let modified: Date?
    public let agent: Bool

    public var id: String { path }

    public var isArtifact: Bool {
        kind == "artifact"
    }

    public var name: String {
        URL(filePath: rel).lastPathComponent
    }

    public var folder: String? {
        let parent = (rel as NSString).deletingLastPathComponent
        return parent.isEmpty ? nil : parent
    }

    private enum CodingKeys: String, CodingKey {
        case path, rel, kind, modified, agent
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.decode(String.self, forKey: .path)
        rel = try c.decodeIfPresent(String.self, forKey: .rel) ?? path
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "markdown"
        modified = (try? c.decodeIfPresent(String.self, forKey: .modified)).flatMap { $0.flatMap(Date.parseRFC3339) }
        agent = try c.decodeIfPresent(Bool.self, forKey: .agent) ?? false
    }
}

extension DocEntry: SyncedItem {
    public static let field = "docs"
}

public struct DocsList: Decodable, Sendable {
    public let docs: [DocEntry]
    public let truncated: Bool
    public let etag: String?
    public let unchanged: Bool

    private enum CodingKeys: String, CodingKey {
        case docs, truncated, etag, unchanged
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        docs = try c.decodeIfPresent([DocEntry].self, forKey: .docs) ?? []
        truncated = try c.decodeIfPresent(Bool.self, forKey: .truncated) ?? false
        etag = try c.decodeIfPresent(String.self, forKey: .etag)
        unchanged = try c.decodeIfPresent(Bool.self, forKey: .unchanged) ?? false
    }
}

@MainActor
@Observable
public final class DocsListStore {
    public let workspace: String
    public let session: String?
    private var list = SyncedList<DocEntry>()
    public private(set) var truncated = false
    public private(set) var loaded = false
    public private(set) var error: String?
    public private(set) var syncedAt: Date?

    @ObservationIgnored private weak var runner: (any CommandRunner)?
    @ObservationIgnored private let cache: ListCache
    @ObservationIgnored private let key: ListCache.Key
    @ObservationIgnored private var etag: String?
    @ObservationIgnored private let saves = SaveThrottle()
    @ObservationIgnored private var inflight = false
    @ObservationIgnored private var again = false

    public init(workspace: String, session: String?, runner: any CommandRunner, cache: ListCache = ListCache(dir: nil), computer: String = "") {
        self.workspace = workspace
        self.session = session
        self.runner = runner
        self.cache = cache
        key = ListCache.Key(computer: computer, workspace: workspace, collection: "docs", scope: session ?? "")
        if let saved = cache.load(key, as: Snapshot.self), let rows = saved.list["docs"]?.arrayValue, let list = try? SyncedList<DocEntry>(rows: rows, cursor: saved.list["cursor"]?.stringValue) {
            self.list = list
            truncated = saved.list["truncated"]?.boolValue ?? false
            etag = saved.list["etag"]?.stringValue
            syncedAt = saved.at
            loaded = true
        }
    }

    public var docs: [DocEntry] {
        list.items
    }

    public func ageLine(now: Date = .now) -> String? {
        ListCache.ageLine(syncedAt: syncedAt, error: error, empty: docs.isEmpty, now: now)
    }

    public func matching(_ query: String) -> [DocEntry] {
        let query = query.trimmingCharacters(in: .whitespaces)
        guard !query.isEmpty else { return docs }
        return docs.filter { $0.rel.localizedCaseInsensitiveContains(query) }
    }

    public func handle(_ signal: ConnectionSignal) {
        switch signal {
        case .online:
            Task { await refresh() }
        case .event(let event) where event.topic == Topic.docsChanged || (event.topic == Topic.agentsChanged && event.payload["kind"]?.stringValue != "resolved"):
            let target = event.payload["workspace"]?.stringValue ?? event.workspace
            guard target.isEmpty || target == workspace else { return }
            Task { await refresh() }
        case .event:
            break
        }
    }

    public func refresh() async {
        guard !inflight else {
            again = true
            return
        }
        inflight = true
        defer { inflight = false }
        repeat {
            again = false
            await load()
        } while again
    }

    private func load() async {
        guard let runner else { return }
        do {
            var args: [String: JSONValue] = ["since": .string(list.cursor ?? "")]
            if let session { args["session"] = .string(session) }
            if let etag { args["ifNoneMatch"] = .string(etag) }
            let reply = try await runner.command(DocsCommand.list, workspace: workspace, args: .object(args))
            if SyncedList<DocEntry>.synced(reply) {
                try list.apply(reply)
                truncated = reply["truncated"]?.boolValue ?? false
                etag = nil
            } else {
                let legacy = try reply.decode(DocsList.self)
                if !legacy.unchanged {
                    try list.replace(reply["docs"]?.arrayValue ?? [])
                    truncated = legacy.truncated
                    etag = legacy.etag
                }
            }
            syncedAt = .now
            error = nil
            saves.request { [weak self] in
                guard let self, let at = self.syncedAt else { return }
                var saved: [String: JSONValue] = ["docs": .array(self.list.rows), "truncated": .bool(self.truncated)]
                if let cursor = self.list.cursor { saved["cursor"] = .string(cursor) }
                if let etag = self.etag { saved["etag"] = .string(etag) }
                self.cache.save(Snapshot(list: .object(saved), at: at), self.key)
            }
        } catch {
            self.error = (error as? RemoteError)?.message ?? error.localizedDescription
        }
        loaded = true
    }

    private struct Snapshot: Codable {
        let list: JSONValue
        let at: Date
    }
}
