import XCTest

struct MacError: Error, CustomStringConvertible {
    let description: String
}

struct Mac: Sendable {
    let base: URL

    static let shared = ProcessInfo.processInfo.environment["E2E_HELPER"].flatMap(URL.init(string:)).map(Mac.init)
    static let out = ProcessInfo.processInfo.environment["E2E_OUT"].map { URL(filePath: $0, directoryHint: .isDirectory) }

    func run(_ id: String, _ args: [String: Any] = [:], caller: String? = nil, agent: Bool = false) throws -> Any {
        var body: [String: Any] = ["id": id, "args": args, "agent": agent]
        if let caller { body["caller"] = caller }
        return try request("POST", "run", body)
    }

    func signal(_ name: String) throws {
        _ = try request("POST", "signal/\(name)", nil)
    }

    func agentLog() throws -> [[String: Any]] {
        try request("GET", "agent-log", nil) as? [[String: Any]] ?? []
    }

    func pushes() throws -> [[String: Any]] {
        try request("GET", "pushes", nil) as? [[String: Any]] ?? []
    }

    func repo(_ name: String) throws -> String {
        guard let path = (try request("POST", "repo/\(name)", nil) as? [String: Any])?["path"] as? String else {
            throw MacError(description: "no repo path")
        }
        return path
    }

    func commit(in dir: String, _ file: String) throws {
        _ = try request("POST", "commit", ["dir": dir, "file": file])
    }

    func rpc(_ method: String, _ params: [String: Any]) throws -> Any {
        try request("POST", "rpc", ["method": method, "params": params])
    }

    func write(_ path: String, _ text: String) throws {
        _ = try request("POST", "write", ["path": path, "text": text])
    }

    func addFolder(_ path: String) throws {
        _ = try request("POST", "rpc", ["method": "workspace.folder.add", "params": ["id": try workspace(), "path": path]])
    }

    func setRemote(_ enabled: Bool) throws {
        _ = try run("remote.enable", ["enabled": enabled])
    }

    func folders() throws -> [String] {
        let ws = try request("POST", "rpc", ["method": "workspace.get", "params": ["id": try workspace()]]) as? [String: Any]
        return (ws?["folders"] as? [[String: Any]] ?? []).compactMap { $0["path"] as? String }
    }

    func workspace() throws -> String {
        (try request("GET", "config", nil) as? [String: Any])?["workspace"] as? String ?? ""
    }

    func uiEvent(_ slot: String, node: String, action: String, value: Any) throws {
        _ = try request("POST", "rpc", ["method": "ui.event", "params": ["workspaceId": try workspace(), "slot": slot, "nodeId": node, "action": action, "value": value]])
    }

    func ui(_ slot: String) throws -> Any? {
        let slots = try request("POST", "rpc", ["method": "ui.get", "params": ["workspaceId": try workspace()]]) as? [[String: Any]] ?? []
        return slots.first { $0["slot"] as? String == slot }?["tree"]
    }

    func openOnTheMac(_ session: String) throws {
        _ = try request("PUT", "mac-open", nil)
        try uiEvent("agents.sidebar", node: session, action: "select", value: session)
    }

    func closeTheMac() throws {
        _ = try request("DELETE", "mac-open", nil)
    }

    func answerOnTheMac(_ session: String, _ value: String) throws {
        try openOnTheMac(session)
        defer { try? closeTheMac() }
        let deadline = Date.now.addingTimeInterval(15)
        while Date.now < deadline {
            if let tree = try ui("agents.main"), let choice = Self.node(in: tree, where: { $0["type"] as? String == "choice" && ($0["props"] as? [String: Any])?["action"] as? String == "elicitationPick" }),
               let id = choice["id"] as? String {
                try uiEvent("agents.main", node: id, action: "elicitationPick", value: [id: value])
                return
            }
            Thread.sleep(forTimeInterval: 0.3)
        }
        throw MacError(description: "no question card in agents.main for \(session)")
    }

    static func node(in tree: Any, where match: ([String: Any]) -> Bool) -> [String: Any]? {
        if let dict = tree as? [String: Any] {
            if match(dict) { return dict }
            for value in dict.values {
                if let found = node(in: value, where: match) { return found }
            }
        } else if let list = tree as? [Any] {
            for value in list {
                if let found = node(in: value, where: match) { return found }
            }
        }
        return nil
    }

    func sessions() throws -> [[String: Any]] {
        try run("agents.sessions") as? [[String: Any]] ?? []
    }

    func session(_ id: String) throws -> [String: Any]? {
        try sessions().first { $0["id"] as? String == id }
    }

    func newSession(_ prompt: String) throws -> String {
        guard let session = (try run("agents.new", ["prompt": prompt]) as? [String: Any])?["session"] as? String else {
            throw MacError(description: "agents.new returned no session")
        }
        return session
    }

    private func request(_ method: String, _ path: String, _ body: [String: Any]?) throws -> Any {
        var request = URLRequest(url: base.appending(path: path), timeoutInterval: 40)
        request.httpMethod = method
        if let body {
            request.httpBody = try JSONSerialization.data(withJSONObject: body)
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        nonisolated(unsafe) var result: Result<Data, Error> = .failure(MacError(description: "no reply"))
        let done = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: request) { data, _, error in
            result = error.map { .failure($0) } ?? .success(data ?? Data())
            done.signal()
        }.resume()
        done.wait()
        let reply = try JSONSerialization.jsonObject(with: try result.get()) as? [String: Any] ?? [:]
        if let error = reply["error"] as? String {
            throw MacError(description: "\(path) \(body?["id"] ?? ""): \(error)")
        }
        return reply["result"] ?? NSNull()
    }
}

@MainActor
class E2ETestCase: XCTestCase {
    var mac: Mac!

    override func setUp() async throws {
        continueAfterFailure = false
        guard let mac = Mac.shared else {
            throw XCTSkip("needs a Clubmate core: run make companion-e2e")
        }
        self.mac = mac
    }

    var deliveredDump: URL? {
        Mac.out?.appending(path: "delivered.json")
    }

    func launch(open target: String? = nil, _ extra: [String] = []) throws -> XCUIApplication {
        let app = XCUIApplication()
        var arguments = extra
        if let target { arguments += ["-open", target] }
        if let dump = deliveredDump { arguments += ["-deliveredDump", dump.path()] }
        let devices = try mac.run("remote.devices") as? [[String: Any]] ?? []
        if devices.isEmpty {
            guard let link = (try mac.run("remote.pair") as? [String: Any])?["link"] as? String else {
                throw MacError(description: "remote.pair returned no link")
            }
            arguments += ["-autoConfirmPairing", "YES", "-pair", link]
        }
        app.launchArguments = arguments
        app.launch()
        allowNotifications(timeout: devices.isEmpty ? 15 : 2)
        return app
    }

    func waitForPushToken() throws {
        let deadline = Date.now.addingTimeInterval(120)
        while try !(mac.run("remote.devices") as? [[String: Any]] ?? []).contains(where: { $0["push"] as? Bool == true }) {
            guard Date.now < deadline else {
                throw XCTSkip("the simulator got no APNs token in 2 min: it needs a network path to sandbox.push.apple.com")
            }
            RunLoop.current.run(until: .now.addingTimeInterval(1))
        }
    }

    func allowNotifications(timeout: TimeInterval) {
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.buttons["Allow"]
        if allow.waitForExistence(timeout: timeout) { allow.tap() }
    }

    func eventually(_ what: String, timeout: TimeInterval = 15, file: StaticString = #filePath, line: UInt = #line, _ check: () throws -> Bool) {
        let deadline = Date.now.addingTimeInterval(timeout)
        var last: Error?
        while Date.now < deadline {
            do {
                if try check() { return }
                last = nil
            } catch {
                last = error
            }
            RunLoop.current.run(until: .now.addingTimeInterval(0.3))
        }
        XCTFail("timed out waiting for \(what)\(last.map { ": \($0)" } ?? "")", file: file, line: line)
    }

    func status(_ session: String) throws -> String? {
        try mac.session(session)?["status"] as? String
    }

    func agentCalls(_ method: String) throws -> [[String: Any]] {
        try mac.agentLog().filter { $0["method"] as? String == method }
    }

    func text(_ app: XCUIApplication, containing label: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
    }

    func step(_ app: XCUIApplication, _ title: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label ==[c] %@ OR label BEGINSWITH[c] %@ OR label CONTAINS[c] %@", title, title + " · ", " · " + title)).firstMatch
    }

    func option(_ title: String, in card: XCUIElement) -> XCUIElement {
        card.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
    }

    func wait(for element: XCUIElement, _ predicate: String, timeout: TimeInterval = 10) -> Bool {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: predicate), object: element)
        return XCTWaiter().wait(for: [expectation], timeout: timeout) == .completed
    }

    func screenshot(_ app: XCUIApplication, _ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }
}
