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
    private var transcripts: [String: [String: JSONValue]] = [:]
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
            sessions.append(fields)
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
            let commands = ["agents.sessions", "agents.transcript", "agents.prompt", "agents.cancel", "agents.permission", "agents.new", "agents.close"].map(JSONValue.string)
            return (200, Self.json(["result": .object(["computer": .object(["id": .string("mac-mock"), "name": .string("Mac Studio")]), "version": .string("0.0.13"), "commands": .array(commands), "topics": .array([.string("agents.changed"), .string("notify.posted")])])]))
        case "workspaces":
            return (200, Self.json(["result": .array([
                .object(["id": .string(Self.workspace), "name": .string("shop"), "roots": .array([.string("/Users/me/src/shop")])]),
                .object(["id": .string("prj_00000000000000f2"), "name": .string("clubmate"), "roots": .array([.string("/Users/me/src/clubmate")])]),
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
            return workspace == Self.workspace ? .array(sessions.map(JSONValue.object)) : .array([])
        case "agents.transcript":
            var transcript = transcripts[session] ?? [:]
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
            append(session, Self.entry("user", "prompt-\(UUID().uuidString)", text: text))
            update(session) { $0["status"] = .string("running"); $0["asks"] = .bool(false) }
            Task {
                try? await Task.sleep(for: .seconds(2))
                finishPrompt(session, text: text)
            }
            return .null
        case "agents.permission":
            answer(session, request: args["request"]?.stringValue ?? "", option: args["option"]?.stringValue ?? "")
            return .null
        case "agents.cancel":
            update(session) { $0["status"] = .string("done"); $0["pending"] = nil }
            return .null
        case "agents.close":
            update(session) { $0["status"] = .string("closed"); $0["closed"] = .bool(true) }
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
        default:
            return .null
        }
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
        sessions[index]["updatedAt"] = Self.stamp(ago: 0)
        publish("agents.changed", ["workspace": .string(Self.workspace), "session": .string(session), "kind": .string("status")])
    }

    private func notifyAll() {
        let asking = sessions.count > 1 ? sessions[1]["id"] ?? .null : .null
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

    private static func entry(_ kind: String, _ id: String, text: String) -> JSONValue {
        .object(["kind": .string(kind), "id": .string(id), "turn": .number(1), "text": .string(text)])
    }

    private static func stamp(ago seconds: TimeInterval) -> JSONValue {
        .string(Date.now.addingTimeInterval(-seconds).formatted(.iso8601))
    }

    private static func json(_ value: [String: JSONValue]) -> Data {
        (try? JSONEncoder().encode(JSONValue.object(value))) ?? Data()
    }
}
#endif
