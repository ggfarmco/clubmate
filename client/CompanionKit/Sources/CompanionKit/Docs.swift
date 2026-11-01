import Foundation
import Observation

public struct DocPage: Decodable, Equatable, Sendable {
    public let path: String
    public let title: String
    public let html: String
    public let rev: Int
    public var anchor: String?

    public init(path: String, title: String, html: String, rev: Int, anchor: String? = nil) {
        self.path = path
        self.title = title
        self.html = html
        self.rev = rev
        self.anchor = anchor
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
        guard let hash = link.lastIndex(of: "#"), [".md", ".markdown", ".mmd"].contains(where: { link[..<hash].lowercased().hasSuffix($0) }) else { return (link, nil) }
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
