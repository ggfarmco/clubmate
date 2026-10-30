import Foundation
import Synchronization

import CompanionKit

final class FakeTransport: HTTPTransport {
    typealias Reply = @Sendable (URLRequest) async throws -> (Data, Int)
    typealias StreamReply = @Sendable (URLRequest) async throws -> (Int, AsyncThrowingStream<Data, any Error>)

    private let reply: Reply
    private let streamReply: StreamReply
    private let log = Mutex<[URLRequest]>([])

    init(reply: @escaping Reply = { _ in (Data(), 404) }, stream: @escaping StreamReply = { _ in (404, AsyncThrowingStream { $0.finish() }) }) {
        self.reply = reply
        streamReply = stream
    }

    var requests: [URLRequest] {
        log.withLock { $0 }
    }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        log.withLock { $0.append(request) }
        let (data, status) = try await reply(request)
        return (data, Self.response(request, status))
    }

    func stream(_ request: URLRequest) async throws -> (HTTPURLResponse, AsyncThrowingStream<Data, any Error>) {
        log.withLock { $0.append(request) }
        let (status, stream) = try await streamReply(request)
        return (Self.response(request, status), stream)
    }

    static func response(_ request: URLRequest, _ status: Int) -> HTTPURLResponse {
        HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: nil)!
    }

    static func rpc(_ request: URLRequest) -> String {
        guard let body = request.httpBody, let value = try? JSONDecoder().decode(JSONValue.self, from: body) else { return "" }
        let method = value["method"]?.stringValue ?? ""
        return method == "command" ? value["params"]?["id"]?.stringValue ?? "" : method
    }

    static func result(_ json: String) -> Data {
        Data("{\"result\":\(json)}".utf8)
    }
}

final class Recorder<Value: Sendable>: Sendable {
    private let storage = Mutex<[Value]>([])

    func add(_ value: Value) {
        storage.withLock { $0.append(value) }
    }

    var values: [Value] {
        storage.withLock { $0 }
    }
}

let helloJSON = #"{"computer":{"id":"mac1","name":"Mac Studio"},"version":"0.0.13","commands":["agents.sessions","agents.transcript","agents.prompt"],"topics":["agents.changed","notify.posted"]}"#
let workspacesJSON = #"[{"id":"prj_00000000000000f1","name":"shop","roots":["/Users/me/src/shop"]}]"#

func fixture(_ name: String) throws -> Data {
    try Data(contentsOf: Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures")!)
}

@MainActor
func waitUntil(timeout: Duration = .seconds(5), _ condition: @MainActor () -> Bool) async throws {
    let deadline = ContinuousClock.now + timeout
    while !condition() {
        guard ContinuousClock.now < deadline else { throw CancellationError() }
        try await Task.sleep(for: .milliseconds(5))
    }
}
