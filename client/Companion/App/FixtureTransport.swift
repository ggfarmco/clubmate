#if DEBUG
import Foundation
import Synchronization

import CompanionKit

final class FixtureTransport: HTTPTransport {
    private let servers: [String: FixtureServer]
    private let fallback: FixtureServer
    private let offline: ContinuousClock.Instant?
    private let offlineHosts = Mutex<[String: ContinuousClock.Instant]>([:])

    init(directory: URL, offlineAfter: Duration? = nil) {
        fallback = FixtureServer(directory: directory, mac: .studio)
        servers = [FixtureMac.pro.host: FixtureServer(directory: directory, mac: .pro), FixtureMac.air.host: FixtureServer(directory: directory, mac: .air)]
        offline = offlineAfter.map { .now + $0 }
    }

    private func server(_ request: URLRequest) -> FixtureServer {
        servers[request.url?.host() ?? ""] ?? fallback
    }

    private func deadline(_ request: URLRequest) -> ContinuousClock.Instant? {
        let host = request.url?.host() ?? ""
        guard host == FixtureMac.air.host else { return offline }
        return offlineHosts.withLock { $0[host] }
    }

    private func reachable(_ request: URLRequest) throws {
        if let deadline = deadline(request), ContinuousClock.now >= deadline { throw URLError(.cannotConnectToHost, userInfo: [NSLocalizedDescriptionKey: "Could not connect to the server."]) }
    }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        try await Task.sleep(for: .milliseconds(120))
        try reachable(request)
        let (status, body) = await server(request).handle(path: request.url?.path() ?? "", body: request.httpBody ?? Data())
        return (body, HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: nil)!)
    }

    func stream(_ request: URLRequest) async throws -> (HTTPURLResponse, AsyncThrowingStream<Data, any Error>) {
        try reachable(request)
        if request.url?.host() == FixtureMac.air.host {
            offlineHosts.withLock { $0[FixtureMac.air.host] = $0[FixtureMac.air.host] ?? .now + .seconds(3) }
        }
        let server = server(request)
        let stream = await server.subscribe()
        if let deadline = deadline(request) {
            Task {
                try? await Task.sleep(until: deadline)
                await server.drop()
            }
        }
        return (HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: "HTTP/1.1", headerFields: nil)!, stream)
    }
}

struct FixtureMac: Sendable {
    static let studio = FixtureMac(id: "mac-mock", name: "Mac Studio", colour: "blue", host: "100.101.1.2", workspaces: [("prj_00000000000000f1", "shop"), ("prj_00000000000000f2", "clubmate")])
    static let pro = FixtureMac(id: "mac-pro", name: "MacBook Pro", colour: "magenta", host: "100.101.1.3", workspaces: [("prj_00000000000000f3", "infra")])
    static let air = FixtureMac(id: "mac-air", name: "MacBook Air", colour: "graphite", host: "100.101.1.4", workspaces: [("prj_00000000000000f4", "shop-api")])

    let id: String
    let name: String
    let colour: String
    let host: String
    let workspaces: [(id: String, name: String)]

    var link: String {
        "clubmate://pair?v=1&c=ABCDEF&h=\(host)&p=47622&id=\(id)&n=\(name.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? name)&e=4000000000"
    }
}

actor FixtureServer {
    private let mac: FixtureMac
    private let workspace: String
    private var colour: String

    private var sessions: [[String: JSONValue]] = []
    private var others: [[String: JSONValue]] = []
    private var transcripts: [String: [String: JSONValue]] = [:]
    private var tasks: [[String: JSONValue]] = []
    private var docs: [String: JSONValue] = [:]
    private var docsList: [JSONValue] = []
    private var sessionDocs: [String: [String]] = [:]
    private var docRevs: [String: Int] = [:]
    private var turns: [String: Task<Void, Never>] = [:]
    private var subscribers: [AsyncThrowingStream<Data, any Error>.Continuation] = []
    private var uiSections: JSONValue = .array([])
    private var uiViews: [String: (rev: Int, tree: JSONValue)] = [:]
    private var openedDoc: JSONValue = .null
    private var streams = 0
    private var seq = 100
    private var notified = false
    private var voice: JSONValue = .null
    private var voicePhase = "listening"
    private var voiceRev = 0
    private var voiceChunks = 0

    init(directory: URL, mac: FixtureMac) {
        self.mac = mac
        workspace = mac.workspaces[0].id
        colour = mac.colour
        let decode = { (name: String) in
            (try? JSONDecoder().decode(JSONValue.self, from: Data(contentsOf: directory.appending(path: name)))) ?? .null
        }
        let list = decode("agents-sessions.json").arrayValue ?? []
        var sessions: [[String: JSONValue]] = []
        for (index, value) in list.enumerated() {
            guard case .object(var fields) = value else { continue }
            let stamp = Self.stamp(ago: [90, 1_500, 86_400][min(index, 2)])
            fields["createdAt"] = stamp
            fields["updatedAt"] = stamp
            if index == 2 { fields["title"] = .string("Bump dependencies") }
            if index == 1 { fields["afk"] = .bool(true) }
            if index == 1, let parent = list.first?["id"] { fields["parentSession"] = parent }
            if case .object(var summary) = fields["pendingSummary"] ?? .null {
                summary["at"] = stamp
                fields["pendingSummary"] = .object(summary)
            }
            if fields["needsYouSince"] != nil { fields["needsYouSince"] = stamp }
            sessions.append(fields)
        }
        if let template = sessions.first {
            let extra: [(String, String, String, Double, String?)] = [
                ("agt_0000000000000004", "Add stock reservation expiry", "running", 240, "Editing reserve.go"),
                ("agt_0000000000000005", "Migrate cart storage", "error", 2_400, nil),
                ("agt_0000000000000006", "Bump Go to 1.25", "done", 3_700, nil),
            ]
            for (id, title, status, ago, activity) in extra {
                sessions.append(Self.session(template, id: id, title: title, status: status, ago: ago, activity: activity))
            }
            others = [Self.session(template, id: "agt_0000000000000007", title: "Draft the companion brief", status: "running", ago: 600, activity: "Reading prompt.md")]
        }
        var transcripts: [String: [String: JSONValue]] = [:]
        if case .object(var first) = decode("agents-transcript.json"), let id = sessions.first?["id"]?.stringValue {
            let entries = first["entries"]?.arrayValue ?? []
            first["entries"] = .array(entries.enumerated().map { index, entry in
                guard case .object(var fields) = entry else { return entry }
                fields["at"] = Self.stamp(ago: Double(entries.count - index) * 3)
                return .object(fields)
            })
            transcripts[id] = first
        }
        if sessions.count > 1, let id = sessions[1]["id"]?.stringValue {
            transcripts[id] = ["entries": .array([
                Self.entry("user", "prompt-1", text: "Explain the checkout flow"),
                Self.entry("agent", "agent-1", text: "Checkout runs in three steps: *validate*, *reserve*, *charge*.\n\n- **validate** checks the cart\n- **reserve** holds stock for 10 minutes\n- **charge** calls the payment provider\n\nShould I add retries to the charge step?"),
            ])]
        }
        transcripts["agt_00000000000000a1"] = ["session": .object(Self.session(sessions.first ?? [:], id: "agt_00000000000000a1", title: "Voice", status: "idle", ago: 60)), "entries": .array([
            Self.voiceEntry("voice-1", heard: "Ответь агенту в checkout: да, добавь ретраи к шагу оплаты и покрой это тестом"),
            Self.entry("agent", "agent-v1", text: "Sent to «Explain the checkout flow»."),
        ])]
        self.sessions = sessions
        self.transcripts = transcripts
        tasks = (decode("backlog-tasks.json")["tasks"]?.arrayValue ?? []).enumerated().compactMap { index, value in
            guard case .object(var fields) = value else { return nil }
            fields["createdAt"] = Self.stamp(ago: Double(index + 1) * 5_400)
            fields["updatedAt"] = Self.stamp(ago: Double(index + 1) * 1_800)
            if fields["claimedBy"] != nil, let id = sessions.first?["id"] { fields["claimedBy"] = id }
            return fields
        }
        for page in decode("docs-read.json")["docs"]?.arrayValue ?? [] {
            if let path = page["path"]?.stringValue { docs[path] = page }
        }
        let listed = decode("docs-list.json")
        docsList = (listed["docs"]?.arrayValue ?? []).enumerated().map { index, value in
            guard case .object(var fields) = value, index < 3 else { return value }
            fields["modified"] = Self.stamp(ago: [120, 1_500, 7_200][index])
            return .object(fields)
        }
        if case .object(let map) = listed["sessions"] ?? .null {
            sessionDocs = map.mapValues { $0.arrayValue?.compactMap(\.stringValue) ?? [] }
        }
        let sdui = decode("sdui.json")
        uiSections = sdui["sections"] ?? .array([])
        for (slot, tree) in sdui["views"]?.objectValue ?? [:] {
            uiViews[slot] = (1, slot == "docs.popup" ? .object(["type": .string("empty")]) : tree)
        }
        openedDoc = sdui["views"]?["docs.popup"] ?? .null
        voice = decode("voice-states.json")
        guard mac.id != FixtureMac.studio.id else { return }
        uiViews = [:]
        docs = [:]
        docsList = []
        tasks = []
        others = []
        self.transcripts = [:]
        self.sessions = mac.id == FixtureMac.pro.id ? sessions.prefix(1).map { first in
            var fields = first
            fields["id"] = .string("agt_0000000000000011")
            fields["workspace"] = .string(mac.workspaces[0].id)
            fields["worktree"] = .string("/Users/me/src/infra")
            fields["title"] = .string("Move the state bucket to the new account")
            fields["pendingSummary"] = .object(["kind": .string("permission"), "text": .string("terraform plan"), "tool": .string("execute"), "at": Self.stamp(ago: 240)])
            fields["needsYouSince"] = Self.stamp(ago: 240)
            return fields
        } : []
    }

    func subscribe() -> AsyncThrowingStream<Data, any Error> {
        let (stream, continuation) = AsyncThrowingStream<Data, any Error>.makeStream()
        streams += 1
        continuation.yield(Data(": connected\n\nevent: stream\ndata: {\"id\":\"fx\(streams)\"}\n\n".utf8))
        subscribers.append(continuation)
        if !notified {
            notified = true
            Task {
                try? await Task.sleep(for: .seconds(1))
                notifyAll()
            }
        }
        return stream
    }

    func drop() {
        for subscriber in subscribers {
            subscriber.finish(throwing: URLError(.networkConnectionLost, userInfo: [NSLocalizedDescriptionKey: "The network connection was lost."]))
        }
        subscribers = []
    }

    func handle(path: String, body: Data) -> (Int, Data) {
        let request = (try? JSONDecoder().decode(JSONValue.self, from: body)) ?? .null
        if path == "/v1/pair" {
            return (200, Self.json(["token": .string("cmt_fixture"), "computer": computer]))
        }
        let params = request["params"] ?? .null
        switch request["method"]?.stringValue {
        case "hello":
            let commands = ["agents.sessions", "agents.transcript", "agents.prompt", "agents.cancel", "agents.stop-subagent", "agents.permission", "agents.elicitation", "agents.new", "agents.close", "agents.resume", "agents.handoff", "agents.mode", "agents.config", "agents.notify", "backlog.list", "backlog.get", "backlog.add", "backlog.update", "backlog.claim", "backlog.release", "backlog.done", "docs.read", "docs.list", "voice.start", "voice.audio", "voice.end", "voice.cancel", "voice.state", "voice.confirm", "voice.settings"].map(JSONValue.string)
            return (200, Self.json(["result": .object(["computer": computer, "version": .string("0.0.13"), "commands": .array(commands), "topics": .array([.string("agents.changed"), .string("backlog.changed"), .string("docs.changed"), .string("docs.opened"), .string("notify.posted"), .string("voice.changed"), .string("voice.settings-changed")]), "ui": .bool(!UserDefaults.standard.bool(forKey: "noUIBridge"))])]))
        case "uiSections":
            return (200, Self.json(["result": uiSections]))
        case "uiGet":
            let slots = params["slots"]?.arrayValue?.compactMap(\.stringValue) ?? []
            let found = params["workspace"]?.stringValue == workspace ? slots.compactMap { slot in uiViews[slot].map { slotView(slot, rev: $0.rev, tree: $0.tree) } } : []
            return (200, Self.json(["result": .array(found)]))
        case "uiWatch":
            return (200, Self.json(["result": .null]))
        case "uiEvent":
            let slot = params["slot"]?.stringValue ?? ""
            if slot.hasSuffix(".popup"), params["action"]?.stringValue == "close" { showUI(slot, .object(["type": .string("empty")])) }
            return (200, Self.json(["result": .null]))
        case "workspaces":
            return (200, Self.json(["result": .array(mac.workspaces.map { item in
                .object(["id": .string(item.id), "name": .string(item.name), "folders": .array([.object(["path": .string("/Users/me/src/" + item.name)])])])
            })]))
        case "macs":
            return (200, Self.json(["result": .null]))
        case "setIdentityColour":
            colour = params["colour"]?.stringValue ?? colour
            publish("remote.identity", ["computer": computer])
            return (200, Self.json(["result": .object(["computer": computer])]))
        case "command" where params["id"]?.stringValue == "docs.read":
            return (200, Self.json(read(params["args"]?["path"]?.stringValue ?? "")))
        case "command":
            return (200, Self.json(["result": command(params["id"]?.stringValue ?? "", workspace: params["workspace"]?.stringValue ?? "", args: params["args"] ?? .null)]))
        default:
            return (200, Self.json(["error": .object(["code": .string("invalid_params"), "message": .string("unknown method")])]))
        }
    }

    private var computer: JSONValue {
        .object(["id": .string(mac.id), "name": .string(mac.name), "colour": .string(colour)])
    }

    private func command(_ id: String, workspace: String, args: JSONValue) -> JSONValue {
        let session = args["session"]?.stringValue ?? ""
        switch id {
        case "agents.sessions":
            return .array((workspace == self.workspace ? sessions : others).map(JSONValue.object))
        case "agents.transcript":
            var transcript = transcripts[session] ?? [:]
            if args["compact"] == .bool(true) {
                transcript["entries"] = .array((transcript["entries"]?.arrayValue ?? []).map(Self.compact))
            }
            if case .number(let from) = args["fromTurn"] ?? .null {
                transcript["entries"] = .array((transcript["entries"]?.arrayValue ?? []).filter { entry in
                    if case .number(let turn) = entry["turn"] ?? .null { return turn >= from }
                    return true
                })
            }
            if var record = sessions.first(where: { $0["id"]?.stringValue == session }) {
                record["pending"] = nil
                transcript["session"] = .object(record)
            }
            return .object(transcript)
        case "agents.prompt":
            let text = args["text"]?.stringValue ?? ""
            var prompt = Self.entry("user", "prompt-\(UUID().uuidString)", text: text)
            if case .object(var fields) = prompt, let attachments = args["attachments"]?.arrayValue, !attachments.isEmpty {
                fields["content"] = .array(attachments.map { attachment in
                    if attachment["kind"] == .string("file") {
                        let name = attachment["name"] ?? .null
                        return .object(["type": .string("resource_link"), "uri": .string("file:///uploads/\(name.stringValue ?? "")"), "name": name])
                    }
                    return .object(["type": .string("image"), "mimeType": attachment["mimeType"] ?? .null, "data": attachment["data"] ?? .null])
                })
                prompt = .object(fields)
            }
            append(session, prompt)
            update(session) { record in
                record["status"] = .string("running")
                record["asks"] = .bool(false)
                if record["title"]?.stringValue?.isEmpty ?? true { record["title"] = .string(text) }
            }
            turns[session]?.cancel()
            turns[session] = Task {
                do {
                    try await Task.sleep(for: .seconds(8))
                } catch {
                    return
                }
                finishPrompt(session, text: text)
            }
            return .null
        case "agents.permission":
            answer(session, request: args["request"]?.stringValue ?? "", option: args["option"]?.stringValue ?? "")
            return .null
        case "agents.cancel":
            turns.removeValue(forKey: session)?.cancel()
            update(session) { $0["status"] = .string("done"); $0["pending"] = nil }
            return .null
        case "agents.stop-subagent":
            stopSubagent(session, args["subagent"]?.stringValue ?? "")
            return .null
        case "agents.close":
            update(session) { $0["status"] = .string("closed") }
            return .null
        case "agents.resume":
            update(session) { $0["status"] = .string("idle"); $0["error"] = nil; $0["failure"] = nil }
            return .null
        case "agents.handoff":
            guard let old = sessions.first(where: { $0["id"]?.stringValue == session }) else { return .null }
            let next = command("agents.new", workspace: workspace, args: .object(["title": old["title"] ?? .null]))
            update(session) { $0["status"] = .string("closed"); $0["handoffTo"] = next["session"]; $0["error"] = nil; $0["failure"] = nil }
            return .object(["session": next["session"] ?? .null])
        case "agents.mode":
            update(session) { $0["mode"] = args["mode"] ?? .null }
            return .null
        case "agents.notify":
            update(session) { $0["notify"] = args["policy"] == .string("all") ? nil : args["policy"] }
            return .null
        case "agents.config":
            configure(session, id: args["id"]?.stringValue ?? "", value: args["value"]?.stringValue ?? "")
            return .null
        case "agents.new":
            let id = String(format: "agt_%016x", sessions.count + 1)
            var record = sessions.first ?? [:]
            record["id"] = .string(id)
            record["title"] = .string(args["title"]?.stringValue ?? args["prompt"]?.stringValue ?? "")
            record["status"] = .string("idle")
            record["pending"] = nil
            record["createdAt"] = Self.stamp(ago: 0)
            record["updatedAt"] = Self.stamp(ago: 0)
            sessions.append(record)
            transcripts[id] = ["entries": .array([])]
            if let prompt = args["prompt"]?.stringValue {
                _ = command("agents.prompt", workspace: workspace, args: .object(["session": .string(id), "text": .string(prompt)]))
            }
            return .object(["session": .string(id), "worktree": record["worktree"] ?? .null])
        case "docs.list":
            guard workspace == self.workspace else { return .object(["docs": .array([]), "truncated": .bool(false)]) }
            guard let session = args["session"]?.stringValue else { return .object(["docs": .array(docsList), "truncated": .bool(false)]) }
            let paths = Set(sessionDocs[session] ?? [])
            return .object(["docs": .array(docsList.filter { paths.contains($0["path"]?.stringValue ?? "") }), "truncated": .bool(false)])
        case "backlog.list":
            guard workspace == self.workspace else { return .object(["tasks": .array([])]) }
            let status = args["status"]?.stringValue ?? "open"
            var list = tasks.filter { task in
                let value = task["status"]?.stringValue ?? ""
                return status == "open" ? ["idea", "todo", "in_progress"].contains(value) : value == status
            }
            if case .number(let limit) = args["limit"] ?? .null { list = Array(list.prefix(Int(limit))) }
            return .object(["tasks": .array(list.map { task in
                var task = task
                task["descriptionHtml"] = nil
                return .object(task)
            })])
        case "backlog.get":
            return tasks.first { $0["id"] == args["id"] }.map(JSONValue.object) ?? .null
        case "backlog.add":
            let id = (tasks.compactMap { $0["id"]?.numberValue }.max() ?? 0) + 1
            var task: [String: JSONValue] = ["id": .number(id), "title": args["title"] ?? .string(""), "status": args["status"] ?? .string("todo"), "priority": args["priority"] ?? .string("normal"), "createdBy": .string("user"), "createdAt": Self.stamp(ago: 0), "updatedAt": Self.stamp(ago: 0)]
            if let description = args["description"] { task["description"] = description }
            tasks.insert(task, at: 0)
            publish("backlog.changed", ["workspace": .string(self.workspace), "id": .number(id), "kind": .string("added")])
            return .object(task)
        case "backlog.update", "backlog.claim", "backlog.release", "backlog.done":
            return updateTask(id, args)
        case "voice.settings":
            return .object(["enabled": .bool(!UserDefaults.standard.bool(forKey: "voiceOff")), "model": .string("nemotron"), "models": .array([])])
        case "voice.start":
            voicePhase = UserDefaults.standard.string(forKey: "voicePhase") ?? "listening"
            voiceRev = 1
            voiceChunks = 0
            return .object(["utterance": .string(Self.utterance)])
        case "voice.audio":
            voiceChunks += 1
            return .object(["partial": .string(voicePartial)])
        case "voice.end":
            if UserDefaults.standard.string(forKey: "voicePhase") == nil {
                setVoice("sending")
                Task {
                    try? await Task.sleep(for: .seconds(1))
                    guard voicePhase == "sending" else { return }
                    setVoice("thinking")
                    try? await Task.sleep(for: .seconds(2))
                    setVoice("confirm")
                }
            }
            return .object(["heard": .string(voicePartial)])
        case "voice.cancel":
            setVoice("cancelled")
            return .null
        case "voice.confirm":
            setVoice(args["approve"] == .bool(true) ? "done" : "cancelled")
            return .null
        case "voice.state":
            guard case .object(var state) = voice["states"]?[voicePhase] ?? .null else { return .null }
            state["utterance"] = .string(Self.utterance)
            state["rev"] = .number(Double(voiceRev))
            if voiceChunks > 0, state["heard"] == nil { state["partial"] = .string(voicePartial) }
            if voicePhase == "sending" { state["sendAt"] = Self.stamp(ago: -1) }
            return .object(state)
        default:
            return .null
        }
    }

    private static let utterance = "vu_fixture"

    private var voicePartial: String {
        let words = (voice["partial"]?.stringValue ?? "").split(separator: " ")
        return words.prefix(voiceChunks).joined(separator: " ")
    }

    private func setVoice(_ phase: String) {
        voicePhase = phase
        voiceRev += 1
        publish("voice.changed", ["workspace": .string(workspace), "utterance": .string(Self.utterance), "rev": .number(Double(voiceRev))])
    }

    private func read(_ path: String) -> [String: JSONValue] {
        guard case .object(var page) = docs[path] ?? listed(path) else {
            return ["error": .object(["code": .string("internal"), "message": .string("not in workspace")])]
        }
        if docRevs[path] == nil {
            docRevs[path] = 1
            Task {
                try? await Task.sleep(for: .seconds(4))
                change(path)
            }
        }
        let rev = docRevs[path] ?? 1
        page["rev"] = .number(Double(rev))
        if rev > 1, let html = page["html"]?.stringValue {
            page["html"] = .string(html + "<p><em>Updated while you were reading.</em></p>\n")
        }
        return ["result": .object(page)]
    }

    private func listed(_ path: String) -> JSONValue {
        guard let entry = docsList.first(where: { $0["path"]?.stringValue == path }) else { return .null }
        let name = URL(filePath: path).deletingPathExtension().lastPathComponent
        return .object(["path": .string(path), "title": .string(name), "kind": entry["kind"] ?? .string("markdown"), "html": .string("<h1>\(name)</h1>\n<p>A doc from the workspace.</p>\n")])
    }

    private func change(_ path: String) {
        let rev = (docRevs[path] ?? 1) + 1
        docRevs[path] = rev
        publish("docs.changed", ["workspace": .string(workspace), "path": .string(path), "rev": .number(Double(rev))])
    }

    private func configure(_ session: String, id: String, value: String) {
        switch id {
        case "afk":
            update(session) { $0["afk"] = .bool(value == "on") }
        case "finish":
            update(session) { $0["finish"] = .string(value) }
        case "advisor":
            transcripts[session]?["advisor"] = .string(value)
            update(session) { _ in }
        default:
            guard case .array(var options) = transcripts[session]?["configOptions"] else { return }
            for index in options.indices where options[index]["id"]?.stringValue == id {
                guard case .object(var fields) = options[index] else { continue }
                fields["currentValue"] = .string(value)
                options[index] = .object(fields)
            }
            transcripts[session]?["configOptions"] = .array(options)
            update(session) { _ in }
        }
    }

    private func updateTask(_ command: String, _ args: JSONValue) -> JSONValue {
        guard let index = tasks.firstIndex(where: { $0["id"] == args["id"] }) else { return .null }
        var task = tasks[index]
        switch command {
        case "backlog.claim":
            task["claimedFrom"] = task["status"]
            task["status"] = .string("in_progress")
            task["claimedBy"] = .string("user")
        case "backlog.release":
            task["status"] = task["claimedFrom"] ?? .string("idea")
            task["claimedBy"] = nil
        case "backlog.done":
            task["status"] = .string("done")
            task["claimedBy"] = nil
        default:
            for key in ["title", "description", "priority", "status"] {
                if let value = args[key] { task[key] = value }
            }
            if args["status"] != nil { task["claimedBy"] = nil }
            if args["description"] != nil { task["descriptionHtml"] = nil }
        }
        task["updatedAt"] = Self.stamp(ago: 0)
        tasks[index] = task
        publish("backlog.changed", ["workspace": .string(workspace), "id": task["id"] ?? .null, "kind": .string("updated")])
        return .object(task)
    }

    private func finishPrompt(_ session: String, text: String) {
        append(session, Self.entry("agent", "agent-\(UUID().uuidString)", text: "Got it — **\(text)**. I'll take care of it and report back."))
        update(session) { $0["status"] = .string("done") }
    }

    private func answer(_ session: String, request: String, option: String) {
        guard case .array(var entries) = transcripts[session]?["entries"] else { return }
        for index in entries.indices where entries[index]["id"]?.stringValue == request {
            guard case .object(var fields) = entries[index] else { continue }
            fields["outcome"] = .object(["outcome": .string("selected"), "optionId": .string(option)])
            fields["answeredAt"] = Self.stamp(ago: 0)
            entries[index] = .object(fields)
        }
        transcripts[session]?["entries"] = .array(entries)
        update(session) { $0["status"] = .string("running"); $0["pending"] = nil }
        publish("agents.changed", ["workspace": .string(workspace), "session": .string(session), "kind": .string("resolved"), "request": .string(request)])
        Task {
            try? await Task.sleep(for: .milliseconds(1500))
            append(session, .object(["kind": .string("tool"), "id": .string("run1"), "turn": .number(1), "tool": .object([
                "toolCallId": .string("run1"), "title": .string("go test ./..."), "kind": .string("execute"), "status": .string("completed"),
                "content": .array([.object(["type": .string("content"), "content": .object(["type": .string("text"), "text": .string("ok  \tshop/cart\t0.412s")])])]),
            ])]))
            append(session, Self.entry("agent", "agent-9", text: "Tests pass. `cartTotal` is extracted and covered by `TestCartTotal`."))
            update(session) { $0["status"] = .string("done") }
        }
    }

    private func stopSubagent(_ session: String, _ subagent: String) {
        guard case .array(var entries) = transcripts[session]?["entries"] else { return }
        for index in entries.indices where entries[index]["kind"] == .string("tool") && entries[index]["id"]?.stringValue == subagent {
            guard case .object(var fields) = entries[index], case .object(var tool) = fields["tool"] ?? .null else { continue }
            tool["status"] = .string("cancelled")
            tool["title"] = .string(tool["title"]?.stringValue?.components(separatedBy: " · ").first ?? "")
            fields["tool"] = .object(tool)
            fields["stoppable"] = nil
            entries[index] = .object(fields)
        }
        transcripts[session]?["entries"] = .array(entries)
        update(session) { _ in }
    }

    private func append(_ session: String, _ entry: JSONValue) {
        var transcript = transcripts[session] ?? [:]
        transcript["entries"] = .array((transcript["entries"]?.arrayValue ?? []) + [entry])
        transcripts[session] = transcript
    }

    private func update(_ session: String, _ change: (inout [String: JSONValue]) -> Void) {
        guard let index = sessions.firstIndex(where: { $0["id"]?.stringValue == session }) else { return }
        change(&sessions[index])
        if sessions[index]["pending"] == nil && sessions[index]["asks"] != .bool(true) {
            sessions[index]["pendingSummary"] = nil
            sessions[index]["needsYou"] = nil
            sessions[index]["needsYouSince"] = nil
        }
        if sessions[index]["status"] != .string("running") { sessions[index]["activity"] = nil }
        sessions[index]["updatedAt"] = Self.stamp(ago: 0)
        publish("agents.changed", ["workspace": .string(workspace), "session": .string(session), "kind": .string("status")])
    }

    private func notifyAll() {
        if mac.id == FixtureMac.pro.id {
            publish("notify.posted", ["id": .string("ntf_p1"), "workspace": .string(workspace), "session": sessions.first?["id"] ?? .null, "title": .string("Permission needed"), "message": .string("Claude wants to run terraform plan in infra."), "kind": .string("permission"), "agent": .bool(true), "ts": Self.stamp(ago: 240)])
        }
        guard mac.id == FixtureMac.studio.id else { return }
        if UserDefaults.standard.bool(forKey: "docOpened"), let session = sessions.first?["id"] {
            Task {
                try? await Task.sleep(for: .seconds(3))
                publish("docs.opened", ["workspace": .string(workspace), "session": session, "path": .string("/Users/me/src/shop/docs/checkout-retries.md")])
                showUI("docs.popup", openedDoc)
            }
        }
        let asking = sessions.count > 1 ? sessions[1]["id"] ?? .null : .null
        publish("notify.posted", ["id": .string("ntf_0"), "workspace": .string(workspace), "title": .string("Stuck"), "message": .string("No output for 15 minutes"), "kind": .string("stuck"), "ts": Self.stamp(ago: 3600)])
        publish("notify.posted", ["id": .string("ntf_1"), "workspace": .string(workspace), "title": .string("Build finished"), "message": .string("make test passed in 42 s."), "kind": .string("done"), "ts": Self.stamp(ago: 600)])
        publish("notify.posted", ["id": .string("ntf_2"), "workspace": .string(workspace), "session": sessions.first?["id"] ?? .null, "title": .string("Permission needed"), "message": .string("Claude wants to run go test ./... in shop."), "kind": .string("permission"), "agent": .bool(true), "ts": Self.stamp(ago: 60)])
        publish("notify.posted", ["id": .string("ntf_3"), "workspace": .string(workspace), "session": asking, "title": .string("Checkout question"), "message": .string("Should I add retries to the charge step?"), "kind": .string("needs-input"), "agent": .bool(true), "ts": Self.stamp(ago: 30)])
    }

    private func slotView(_ slot: String, rev: Int, tree: JSONValue) -> JSONValue {
        .object(["workspaceId": .string(workspace), "slot": .string(slot), "owner": .string(String(slot.prefix { $0 != "." })), "rev": .number(Double(rev)), "tree": tree])
    }

    private func showUI(_ slot: String, _ tree: JSONValue) {
        let rev = (uiViews[slot]?.rev ?? 0) + 1
        uiViews[slot] = (rev, tree)
        let frame = Self.json(["workspace": .string(workspace), "slot": .string(slot), "rev": .number(Double(rev)), "tree": tree])
        let wire = Data("event: ui\ndata: ".utf8) + frame + Data("\n\n".utf8)
        for subscriber in subscribers {
            subscriber.yield(wire)
        }
    }

    private func publish(_ topic: String, _ payload: [String: JSONValue]) {
        seq += 1
        let event = Self.json(["seq": .number(Double(seq)), "topic": .string(topic), "workspace": .string(workspace), "payload": .object(payload)])
        let wire = Data("id: \(seq)\nevent: bus\ndata: ".utf8) + event + Data("\n\n".utf8)
        for subscriber in subscribers {
            subscriber.yield(wire)
        }
    }

    private static func compact(_ entry: JSONValue) -> JSONValue {
        guard case .object(var fields) = entry, let blocks = fields["content"]?.arrayValue else { return entry }
        fields["content"] = .array(blocks.map { block in
            guard case .object(var image) = block, image["type"] == .string("image") else { return block }
            image["data"] = .string("")
            return .object(image)
        })
        return .object(fields)
    }

    private static func voiceEntry(_ id: String, heard: String) -> JSONValue {
        let prompt: JSONValue = .object([
            "utterance": .string("vu_fixture"),
            "device": .string("phone"),
            "heard": .string(heard),
            "context": .object([
                "pointer": .object(["device": .string("phone"), "section": .string("agents"), "selection": .object(["session": .string("agt_0000000000000002")])]),
                "session": .object(["id": .string("agt_0000000000000002"), "title": .string("Explain the checkout flow"), "status": .string("waiting"), "question": .object(["text": .string("Should I add retries to the charge step?")])]),
                "workspace": .object(["name": .string("shop"), "branches": .array([.string("main"), .string("feat/checkout-retries")])]),
            ]),
        ])
        let text = (try? JSONEncoder().encode(prompt)).map { String(decoding: $0, as: UTF8.self) } ?? ""
        return .object(["kind": .string("user"), "id": .string(id), "turn": .number(1), "from": .string("voice"), "text": .string(text), "voice": .object(["heard": .string(heard), "utterance": .string("vu_fixture")])])
    }

    private static func entry(_ kind: String, _ id: String, text: String) -> JSONValue {
        .object(["kind": .string(kind), "id": .string(id), "turn": .number(1), "text": .string(text)])
    }

    private static func session(_ template: [String: JSONValue], id: String, title: String, status: String, ago: TimeInterval, activity: String? = nil) -> [String: JSONValue] {
        var fields = template
        for key in ["pending", "pendingSummary", "question", "parentSession", "needsYou", "needsYouSince"] { fields[key] = nil }
        if let activity { fields["activity"] = .object(["title": .string(activity), "at": stamp(ago: ago)]) }
        fields["id"] = .string(id)
        fields["title"] = .string(title)
        fields["status"] = .string(status)
        fields["asks"] = .bool(false)
        fields["createdAt"] = stamp(ago: ago)
        fields["updatedAt"] = stamp(ago: status == "running" ? 5 : ago)
        if status == "running" { fields["turnStartedAt"] = stamp(ago: ago) }
        if status == "error" {
            fields["error"] = .string("acp error -32603: exit status 1")
            fields["failure"] = .object(["word": .string("exit 1"), "badge": .string("Stopped · exit 1"), "line": .string("Claude exited with status 1."), "actions": .array([failureAction(id)])])
        }
        return fields
    }

    private static func failureAction(_ session: String) -> JSONValue {
        let args = JSONValue.object(["session": .string(session)])
        return switch UserDefaults.standard.string(forKey: "failureAction") {
        case "signin": .object(["id": .string("signin"), "label": .string("Sign in"), "icon": .string("person.badge.key")])
        case "continue": .object(["id": .string("continue"), "label": .string("Hand off to a new session"), "icon": .string("arrow.turn.down.right"), "command": .string("agents.handoff"), "args": args])
        default: .object(["id": .string("retry"), "label": .string("Retry"), "icon": .string("arrow.clockwise"), "command": .string("agents.resume"), "args": args])
        }
    }

    private static func stamp(ago seconds: TimeInterval) -> JSONValue {
        .string(Date.now.addingTimeInterval(-seconds).formatted(.iso8601))
    }

    private static func json(_ value: [String: JSONValue]) -> Data {
        (try? JSONEncoder().encode(JSONValue.object(value))) ?? Data()
    }
}
#endif
