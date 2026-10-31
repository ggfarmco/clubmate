import Foundation
import WebKit

import ClubmateProtocol

@MainActor
public protocol BrowserHost: AnyObject {
    func attach(_ webView: WKWebView)
    func detach(_ webView: WKWebView)
    func handoff(_ webView: WKWebView, reason: String, owner: String) async -> Bool
    func cancelHandoff()
}

@MainActor
public final class AgentBrowser {
    private typealias Call = Pending<Result<JSONValue, Error>>

    private let host: BrowserHost
    private let dataStore: WKWebsiteDataStore
    private let userAgent: String
    private let screenshots: URL
    private let navigationTimeout: Duration
    private var tabs: [String: BrowserTab] = [:]
    private var chains: [String: (id: Int, task: Task<Void, Never>)] = [:]
    private var calls: [Int: Call] = [:]
    private var nextID = 0
    private var activeHandoff: HandoffTicket?
    private var handoffQueue: [HandoffTicket] = []

    public init(host: BrowserHost, dataStore: WKWebsiteDataStore, userAgent: String, screenshots: URL, navigationTimeout: Duration = .seconds(30)) {
        self.host = host
        self.dataStore = dataStore
        self.userAgent = userAgent
        self.screenshots = screenshots
        self.navigationTimeout = navigationTimeout
    }

    public func handle(_ call: BrowserCall) async throws -> JSONValue {
        let deadline = ContinuousClock.now + .seconds(call.timeout ?? 300)
        let step = try Step(call)
        let key = call.key
        nextID += 1
        let id = nextID
        let pending = Call(key: key)
        calls[id] = pending
        defer { calls[id] = nil }
        let previous = chains[key]?.task
        let task = Task {
            await previous?.value
            if !pending.resolved {
                do {
                    pending.resolve(.success(try await self.perform(step, key: key, call: pending, deadline: deadline)))
                } catch {
                    pending.resolve(.failure(error))
                }
            }
            if self.chains[key]?.id == id { self.chains[key] = nil }
        }
        chains[key] = (id, task)
        return try await withTaskCancellationHandler {
            try await pending.wait().get()
        } onCancel: {
            Task { @MainActor in
                pending.resolve(.failure(CancellationError()))
                self.dropHandoffs { $0.call === pending }
            }
        }
    }

    public func close(owner: String) {
        let prefix = owner + "/"
        for (key, tab) in tabs where key.hasPrefix(prefix) {
            tabs[key] = nil
            tab.webView.stopLoading()
            host.detach(tab.webView)
        }
        chains = chains.filter { !$0.key.hasPrefix(prefix) }
        for call in calls.values where call.key.hasPrefix(prefix) {
            call.resolve(.failure(BrowserError(message: "tab closed")))
        }
        dropHandoffs { $0.call.key.hasPrefix(prefix) }
    }

    public static func owner(closedBy event: BusEvent) -> String? {
        switch event.topic {
        case Topic.agentsChanged where event.payload["kind"]?.stringValue == "deleted",
             Topic.agentsChanged where event.payload["kind"]?.stringValue == "status" && event.payload["status"]?.stringValue == "closed":
            event.payload["session"]?.stringValue
        case Topic.sessionRemoved:
            event.payload["id"]?.stringValue
        default:
            nil
        }
    }

    public static func safariUserAgent(version: String) -> String {
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/\(version) Safari/605.1.15"
    }

    private func perform(_ step: Step, key: String, call: Call, deadline: ContinuousClock.Instant) async throws -> JSONValue {
        let tab = tab(for: key)
        switch step {
        case .open(let url):
            return try JSONValue.encoding(await tab.open(url))
        case .snapshot:
            let snapshot = try await tab.snapshot()
            return .object(["url": .string(snapshot.url), "title": .string(snapshot.title), "text": .string(snapshot.text)])
        case .click(let ref):
            return try JSONValue.encoding(try await tab.click(ref: ref))
        case .type(let ref, let text, let submit):
            return try JSONValue.encoding(try await tab.type(ref: ref, text: text, submit: submit))
        case .select(let ref, let option):
            return try JSONValue.encoding(try await tab.select(ref: ref, option: option))
        case .press(let key):
            return try JSONValue.encoding(try await tab.press(key: key))
        case .scroll(let direction, let ref):
            return try JSONValue.encoding(try await tab.scroll(direction: direction, ref: ref))
        case .back:
            return try JSONValue.encoding(await tab.back())
        case .screenshot:
            return .object(["path": .string(try await tab.screenshot(to: screenshots).path)])
        case .handoff(let reason, let owner):
            return await handoff(tab, reason: reason, owner: owner, call: call, deadline: deadline)
        }
    }

    private func handoff(_ tab: BrowserTab, reason: String, owner: String, call: Call, deadline: ContinuousClock.Instant) async -> JSONValue {
        let ticket = HandoffTicket(call: call)
        guard await acquireHandoff(ticket, until: deadline) else { return Self.handoffResult(false) }
        guard !ticket.outcome.resolved, !call.resolved, ContinuousClock.now < deadline else {
            endHandoff(ticket, done: false)
            return Self.handoffResult(false)
        }
        ticket.shown = true
        Task.immediate { [host] in
            let done = await host.handoff(tab.webView, reason: reason, owner: owner)
            self.endHandoff(ticket, done: done, cancel: false)
        }
        let timer = Task {
            guard (try? await Task.sleep(until: deadline)) != nil else { return }
            self.endHandoff(ticket, done: false)
        }
        let done = await ticket.outcome.wait()
        timer.cancel()
        return Self.handoffResult(done)
    }

    private func acquireHandoff(_ ticket: HandoffTicket, until deadline: ContinuousClock.Instant) async -> Bool {
        guard activeHandoff != nil else {
            activeHandoff = ticket
            return true
        }
        handoffQueue.append(ticket)
        let timer = Task {
            guard (try? await Task.sleep(until: deadline)) != nil else { return }
            self.handoffQueue.removeAll { $0 === ticket }
            ticket.granted.resolve(false)
        }
        let granted = await ticket.granted.wait()
        timer.cancel()
        return granted
    }

    private func endHandoff(_ ticket: HandoffTicket, done: Bool, cancel: Bool = true) {
        guard !ticket.outcome.resolved else { return }
        ticket.outcome.resolve(done)
        if cancel, ticket.shown { host.cancelHandoff() }
        guard activeHandoff === ticket else { return }
        if handoffQueue.isEmpty {
            activeHandoff = nil
        } else {
            let next = handoffQueue.removeFirst()
            activeHandoff = next
            next.granted.resolve(true)
        }
    }

    private func dropHandoffs(where matches: (HandoffTicket) -> Bool) {
        let dropped = handoffQueue.filter(matches)
        handoffQueue.removeAll(where: matches)
        dropped.forEach { $0.granted.resolve(false) }
        if let active = activeHandoff, matches(active) { endHandoff(active, done: false) }
    }

    private func tab(for key: String) -> BrowserTab {
        if let tab = tabs[key] { return tab }
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = dataStore
        let tab = BrowserTab(configuration: configuration, userAgent: userAgent, navigationTimeout: navigationTimeout)
        tabs[key] = tab
        host.attach(tab.webView)
        return tab
    }

    private static func handoffResult(_ done: Bool) -> JSONValue {
        .object(["result": .string(done ? "done" : "no-human")])
    }
}

private enum Step {
    case open(URL)
    case snapshot
    case click(String)
    case type(String, String, Bool)
    case select(String, String)
    case press(String)
    case scroll(String?, String?)
    case back
    case screenshot
    case handoff(String, String)

    init(_ call: BrowserCall) throws {
        let args = call.args
        func required(_ name: String) throws -> String {
            guard let value = args[name]?.stringValue else { throw BrowserError(message: "\(name) is required") }
            return value
        }
        switch call.action {
        case .open:
            guard let text = args["url"]?.stringValue,
                  let url = URL(string: text),
                  ["http", "https"].contains(url.scheme?.lowercased()),
                  url.host() != nil
            else { throw BrowserError(message: "url must be http or https") }
            self = .open(url)
        case .snapshot: self = .snapshot
        case .click: self = .click(try required("ref"))
        case .type: self = .type(try required("ref"), try required("text"), args["submit"]?.boolValue ?? false)
        case .select: self = .select(try required("ref"), try required("option"))
        case .press: self = .press(try required("key"))
        case .scroll: self = .scroll(args["direction"]?.stringValue, args["ref"]?.stringValue)
        case .back: self = .back
        case .screenshot: self = .screenshot
        case .handoff: self = .handoff(args["reason"]?.stringValue ?? "", call.owner)
        }
    }
}

@MainActor
private final class HandoffTicket {
    let call: Pending<Result<JSONValue, Error>>
    let granted = Pending<Bool>()
    let outcome = Pending<Bool>()
    var shown = false

    init(call: Pending<Result<JSONValue, Error>>) {
        self.call = call
    }
}

@MainActor
private final class Pending<Value: Sendable> {
    let key: String
    private(set) var resolved = false
    private var value: Value?
    private var continuation: CheckedContinuation<Value, Never>?

    init(key: String = "") {
        self.key = key
    }

    func resolve(_ outcome: Value) {
        guard !resolved else { return }
        resolved = true
        if let continuation {
            self.continuation = nil
            continuation.resume(returning: outcome)
        } else {
            value = outcome
        }
    }

    func wait() async -> Value {
        if let value { return value }
        return await withCheckedContinuation { continuation = $0 }
    }
}
