#if DEBUG
import Foundation

import CompanionKit

final class FixtureTransport: HTTPTransport {
    private let server: FixtureServer

    init(directory: URL) {
        server = FixtureServer(directory: directory)
    }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        try await Task.sleep(for: .milliseconds(120))
        let (status, body) = await server.handle(path: request.url?.path() ?? "", body: request.httpBody ?? Data())
        return (body, HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: nil)!)
    }

    func stream(_ request: URLRequest) async throws -> (HTTPURLResponse, AsyncThrowingStream<Data, any Error>) {
        let stream = await server.subscribe()
        return (HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: "HTTP/1.1", headerFields: nil)!, stream)
    }
}

actor FixtureServer {
    private static let workspace = "prj_00000000000000f1"

    private var sessions: [[String: JSONValue]] = []
    private var others: [[String: JSONValue]] = []
    private var transcripts: [String: [String: JSONValue]] = [:]
    private var tasks: [[String: JSONValue]] = []
    private var turns: [String: Task<Void, Never>] = [:]
    private var subscribers: [AsyncThrowingStream<Data, any Error>.Continuation] = []
    private var seq = 100
    private var notified = false

    init(directory: URL) {
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
            sessions.append(fields)
        }
        if let template = sessions.first {
            let extra: [(String, String, String, Double)] = [
                ("agt_0000000000000004", "Add stock reservation expiry", "running", 240),
                ("agt_0000000000000005", "Migrate cart storage", "error", 2_400),
                ("agt_0000000000000006", "Bump Go to 1.25", "done", 3_700),
            ]
            for (id, title, status, ago) in extra {
                sessions.append(Self.session(template, id: id, title: title, status: status, ago: ago))
            }
            others = [Self.session(template, id: "agt_0000000000000007", title: "Draft the companion brief", status: "running", ago: 600)]
        }
        var transcripts: [String: [String: JSONValue]] = [:]
        if case .object(let first) = decode("agents-transcript.json"), let id = sessions.first?["id"]?.stringValue {
            transcripts[id] = first
        }
        if sessions.count > 1, let id = sessions[1]["id"]?.stringValue {
            transcripts[id] = ["entries": .array([
                Self.entry("user", "prompt-1", text: "Explain the checkout flow"),
                Self.entry("agent", "agent-1", text: "Checkout runs in three steps: *validate*, *reserve*, *charge*.\n\n- **validate** checks the cart\n- **reserve** holds stock for 10 minutes\n- **charge** calls the payment provider\n\nShould I add retries to the charge step?"),
            ])]
        }
        self.sessions = sessions
        self.transcripts = transcripts
        tasks = (decode("backlog-tasks.json")["tasks"]?.arrayValue ?? []).enumerated().compactMap { index, value in
            guard case .object(var fields) = value else { return nil }
            fields["createdAt"] = Self.stamp(ago: Double(index + 1) * 5_400)
            fields["updatedAt"] = Self.stamp(ago: Double(index + 1) * 1_800)
            if fields["claimedBy"] != nil, let id = sessions.first?["id"] { fields["claimedBy"] = id }
            return fields
        }
    }

    func subscribe() -> AsyncThrowingStream<Data, any Error> {
        let (stream, continuation) = AsyncThrowingStream<Data, any Error>.makeStream()
        continuation.yield(Data(": connected\n\n".utf8))
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

    func handle(path: String, body: Data) -> (Int, Data) {
        let request = (try? JSONDecoder().decode(JSONValue.self, from: body)) ?? .null
        if path == "/v1/pair" {
            return (200, Self.json(["token": .string("cmt_fixture"), "computer": .object(["id": .string("mac-mock"), "name": .string("Mac Studio")])]))
        }
        let params = request["params"] ?? .null
        switch request["method"]?.stringValue {
        case "hello":
            let commands = ["agents.sessions", "agents.transcript", "agents.prompt", "agents.cancel", "agents.permission", "agents.elicitation", "agents.new", "agents.close", "agents.mode", "agents.config", "agents.notify", "backlog.list", "backlog.get", "backlog.add", "backlog.update", "backlog.claim", "backlog.release", "backlog.done"].map(JSONValue.string)
            return (200, Self.json(["result": .object(["computer": .object(["id": .string("mac-mock"), "name": .string("Mac Studio")]), "version": .string("0.0.13"), "commands": .array(commands), "topics": .array([.string("agents.changed"), .string("backlog.changed"), .string("notify.posted")])])]))
        case "workspaces":
            return (200, Self.json(["result": .array([
                .object(["id": .string(Self.workspace), "name": .string("shop"), "folders": .array([.object(["path": .string("/Users/me/src/shop")])])]),
                .object(["id": .string("prj_00000000000000f2"), "name": .string("clubmate"), "folders": .array([.object(["path": .string("/Users/me/src/clubmate")])])]),
            ])]))
        case "command":
            return (200, Self.json(["result": command(params["id"]?.stringValue ?? "", workspace: params["workspace"]?.stringValue ?? "", args: params["args"] ?? .null)]))
        default:
            return (200, Self.json(["error": .object(["code": .string("invalid_params"), "message": .string("unknown method")])]))
        }
    }

    private func command(_ id: String, workspace: String, args: JSONValue) -> JSONValue {
        let session = args["session"]?.stringValue ?? ""
        switch id {
        case "agents.sessions":
            return .array((workspace == Self.workspace ? sessions : others).map(JSONValue.object))
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
                fields["content"] = .array(attachments.map { .object(["type": .string("image"), "mimeType": $0["mimeType"] ?? .null, "data": $0["data"] ?? .null]) })
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
        case "agents.close":
            update(session) { $0["status"] = .string("closed") }
            return .null
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
        case "backlog.list":
            guard workspace == Self.workspace else { return .object(["tasks": .array([])]) }
            let status = args["status"]?.stringValue ?? "open"
            var list = tasks.filter { task in
                let value = task["status"]?.stringValue ?? ""
                return status == "open" ? ["idea", "todo", "in_progress"].contains(value) : value == status
            }
            if case .number(let limit) = args["limit"] ?? .null { list = Array(list.prefix(Int(limit))) }
            return .object(["tasks": .array(list.map(JSONValue.object))])
        case "backlog.get":
            return tasks.first { $0["id"] == args["id"] }.map(JSONValue.object) ?? .null
        case "backlog.add":
            let id = (tasks.compactMap { $0["id"]?.numberValue }.max() ?? 0) + 1
            var task: [String: JSONValue] = ["id": .number(id), "title": args["title"] ?? .string(""), "status": args["status"] ?? .string("todo"), "priority": args["priority"] ?? .string("normal"), "createdBy": .string("user"), "createdAt": Self.stamp(ago: 0), "updatedAt": Self.stamp(ago: 0)]
            if let description = args["description"] { task["description"] = description }
            tasks.insert(task, at: 0)
            publish("backlog.changed", ["workspace": .string(Self.workspace), "id": .number(id), "kind": .string("added")])
            return .object(task)
        case "backlog.update", "backlog.claim", "backlog.release", "backlog.done":
            return updateTask(id, args)
        default:
            return .null
        }
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
        }
        task["updatedAt"] = Self.stamp(ago: 0)
        tasks[index] = task
        publish("backlog.changed", ["workspace": .string(Self.workspace), "id": task["id"] ?? .null, "kind": .string("updated")])
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
            entries[index] = .object(fields)
        }
        transcripts[session]?["entries"] = .array(entries)
        update(session) { $0["status"] = .string("running"); $0["pending"] = nil }
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

    private func append(_ session: String, _ entry: JSONValue) {
        var transcript = transcripts[session] ?? [:]
        transcript["entries"] = .array((transcript["entries"]?.arrayValue ?? []) + [entry])
        transcripts[session] = transcript
    }

    private func update(_ session: String, _ change: (inout [String: JSONValue]) -> Void) {
        guard let index = sessions.firstIndex(where: { $0["id"]?.stringValue == session }) else { return }
        change(&sessions[index])
        if sessions[index]["pending"] == nil && sessions[index]["asks"] != .bool(true) { sessions[index]["pendingSummary"] = nil }
        sessions[index]["updatedAt"] = Self.stamp(ago: 0)
        publish("agents.changed", ["workspace": .string(Self.workspace), "session": .string(session), "kind": .string("status")])
    }

    private func notifyAll() {
        let asking = sessions.count > 1 ? sessions[1]["id"] ?? .null : .null
        publish("notify.posted", ["id": .string("ntf_0"), "workspace": .string(Self.workspace), "title": .string("Stuck"), "message": .string("No output for 15 minutes"), "kind": .string("stuck"), "ts": Self.stamp(ago: 3600)])
        publish("notify.posted", ["id": .string("ntf_1"), "workspace": .string(Self.workspace), "title": .string("Build finished"), "message": .string("make test passed in 42 s."), "kind": .string("done"), "ts": Self.stamp(ago: 600)])
        publish("notify.posted", ["id": .string("ntf_2"), "workspace": .string(Self.workspace), "session": sessions.first?["id"] ?? .null, "title": .string("Permission needed"), "message": .string("Claude wants to run go test ./... in shop."), "kind": .string("permission"), "agent": .bool(true), "ts": Self.stamp(ago: 60)])
        publish("notify.posted", ["id": .string("ntf_3"), "workspace": .string(Self.workspace), "session": asking, "title": .string("Checkout question"), "message": .string("Should I add retries to the charge step?"), "kind": .string("needs-input"), "agent": .bool(true), "ts": Self.stamp(ago: 30)])
    }

    private func publish(_ topic: String, _ payload: [String: JSONValue]) {
        seq += 1
        let event = Self.json(["seq": .number(Double(seq)), "topic": .string(topic), "workspace": .string(Self.workspace), "payload": .object(payload)])
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

    private static func entry(_ kind: String, _ id: String, text: String) -> JSONValue {
        .object(["kind": .string(kind), "id": .string(id), "turn": .number(1), "text": .string(text)])
    }

    private static func session(_ template: [String: JSONValue], id: String, title: String, status: String, ago: TimeInterval) -> [String: JSONValue] {
        var fields = template
        for key in ["pending", "pendingSummary", "question", "parentSession"] { fields[key] = nil }
        fields["id"] = .string(id)
        fields["title"] = .string(title)
        fields["status"] = .string(status)
        fields["asks"] = .bool(false)
        fields["createdAt"] = stamp(ago: ago)
        fields["updatedAt"] = stamp(ago: status == "running" ? 5 : ago)
        if status == "running" { fields["turnStartedAt"] = stamp(ago: ago) }
        if status == "error" { fields["error"] = .string("exit status 1") }
        return fields
    }

    private static func stamp(ago seconds: TimeInterval) -> JSONValue {
        .string(Date.now.addingTimeInterval(-seconds).formatted(.iso8601))
    }

    private static func json(_ value: [String: JSONValue]) -> Data {
        (try? JSONEncoder().encode(JSONValue.object(value))) ?? Data()
    }
}
#endif
