import Darwin
import Foundation
import Synchronization
import XCTest

@testable import ClubmateKit

@MainActor
final class HealTests: XCTestCase {
    func testAStoreKeepsRetryingWhileTheCoreIsAliveButFailing() async throws {
        let base = try TestCore.makeTempDir()
        defer { try? FileManager.default.removeItem(at: base) }
        let paths = ClubmatePaths(state: base.appending(path: "state"), runtime: base.appending(path: "run"))
        for dir in [paths.runtime, paths.sessionsSocketDir] {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        }
        let core = try ScriptedCore(path: paths.coreSocket.path(percentEncoded: false), failingSections: 2)
        let client = CoreClient(connection: try await RPCConnection.connect(paths: paths))
        let store = WorkspaceUIStore(client: client, workspaceID: "")

        await store.attach(client)

        XCTAssertFalse(store.connected)
        try await waitUntil { store.connected }
        XCTAssertNil(store.failure)
        XCTAssertEqual(core.sectionCalls, 3)
        await store.stop()
        await client.close()
    }
}

private final class ScriptedCore: Sendable {
    private let listener: Int32
    private let failing: Int
    private let sections = Mutex(0)

    init(path: String, failingSections: Int) throws {
        failing = failingSections
        listener = socket(AF_UNIX, SOCK_STREAM, 0)
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        withUnsafeMutableBytes(of: &addr.sun_path) { $0.copyBytes(from: Array(path.utf8)) }
        let bound = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.bind(listener, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard listener >= 0, bound == 0, Darwin.listen(listener, 1) == 0 else {
            throw TestFailure(description: "listen \(path): \(String(cString: strerror(errno)))")
        }
        let thread = Thread { [self] in serve() }
        thread.start()
    }

    deinit {
        Darwin.close(listener)
    }

    var sectionCalls: Int {
        sections.withLock { $0 }
    }

    private func serve() {
        let fd = accept(listener, nil, nil)
        guard fd >= 0 else { return }
        defer { Darwin.close(fd) }
        var lines = LineBuffer()
        var chunk = [UInt8](repeating: 0, count: 64 * 1024)
        while true {
            let n = chunk.withUnsafeMutableBytes { Darwin.read(fd, $0.baseAddress, $0.count) }
            if n <= 0 { return }
            chunk.withUnsafeBytes { lines.feed(UnsafeRawBufferPointer(rebasing: $0[0..<n])) { answer($0, on: fd) } }
        }
    }

    private func answer(_ line: Data, on fd: Int32) {
        guard let request = try? JSONSerialization.jsonObject(with: line) as? [String: Any], let id = request["id"] as? Int else { return }
        let reply: String
        switch request["method"] as? String {
        case "ui.sections":
            let call = sections.withLock { $0 += 1; return $0 }
            reply = call <= failing ? #""error":{"code":-32000,"message":"plugins are restarting"}"# : #""result":[]"#
        case "ui.get":
            reply = #""result":[]"#
        case "ui.subscribe":
            reply = #""result":{"subscription":"sub_1"}"#
        case "ui.unsubscribe":
            reply = #""result":{}"#
        default:
            reply = #""error":{"code":-32601,"message":"method not found"}"#
        }
        let bytes = Array("{\"jsonrpc\":\"2.0\",\"id\":\(id),\(reply)}\n".utf8)
        _ = bytes.withUnsafeBytes { Darwin.write(fd, $0.baseAddress, $0.count) }
    }
}
