import Foundation
import Observation

public struct QueuedAction: Codable, Sendable, Equatable, Identifiable {
    public enum Kind: String, Codable, Sendable {
        case connect
        case archive
        case command
    }

    public var id = UUID().uuidString
    public let computer: String
    public let workspace: String
    public let session: String
    public let kind: Kind
    public let title: String
    public var request: String?
    public var seen: String?
    public var answers: [ConnectAnswer] = []
    public var choices: [ArchiveChoice] = []
    public var at = Date.now
    public var attempted = false
    public var noted = false
    public var tries = 0
    public var nextTry: Date?
    public var failure: String?
    public var name: String?
    public var args: JSONValue?
    public var place: String?
    public var text: String?
    public var deduped: Bool?

    public init(computer: String, workspace: String, session: String, kind: Kind, title: String, request: String? = nil, seen: String? = nil, answers: [ConnectAnswer] = [], choices: [ArchiveChoice] = []) {
        self.computer = computer
        self.workspace = workspace
        self.session = session
        self.kind = kind
        self.title = title
        self.request = request
        self.seen = seen
        self.answers = answers
        self.choices = choices
    }

    public init(computer: String, workspace: String, command: String, args: JSONValue, target: String, title: String, text: String? = nil, seen: String? = nil) {
        self.computer = computer
        self.workspace = workspace
        session = target.hasPrefix(Self.sessionTarget) ? String(target.dropFirst(Self.sessionTarget.count)) : ""
        kind = .command
        self.title = title
        self.seen = seen
        name = command
        self.args = args
        place = target
        self.text = text
    }

    public static let sessionTarget = "session/"
    static let checkOnMac = "Check it on the Mac"

    public var requestId: String { id }

    public var target: String {
        place ?? Self.sessionTarget + session
    }

    public var mayHaveRun: Bool {
        attempted && deduped != true
    }
}

extension QueuedAction {
    public var canUndo: Bool {
        !attempted && failure == nil
    }

    public var canDismiss: Bool {
        failure != nil
    }

    public func line(queued: String) -> String {
        if let failure { return title + (failure == QueuedAction.checkOnMac ? " may have run" : " didn't go through") }
        if mayHaveRun { return title + " may have run" }
        return nextTry == nil ? queued : title + " · trying again"
    }

    public var detail: String {
        failure ?? (mayHaveRun ? "Check it on the Mac" : title)
    }
}

public struct Landed: Sendable, Equatable {
    public let item: QueuedAction
    public let result: JSONValue
    public let at: Date
}

public struct Flushed: Sendable, Equatable {
    public var touched: Set<String> = []
    public var landed: [Landed] = []
}

public struct QueueNote: Codable, Sendable, Equatable, Identifiable {
    public var id = UUID().uuidString
    public let computer: String
    public let session: String
    public let text: String
    public var at = Date.now
}

@MainActor
@Observable
public final class ActionQueue {
    public private(set) var items: [QueuedAction] = []
    public private(set) var notes: [QueueNote] = []

    public static let maxTries = 8
    public static let firstWait: TimeInterval = 30
    public static let longestWait: TimeInterval = 1800
    public static let retryDelay: Duration = .seconds(5)

    @ObservationIgnored private let file: URL?
    @ObservationIgnored private let now: @MainActor () -> Date
    @ObservationIgnored private var flushing: Set<String> = []
    @ObservationIgnored private var sending: Set<String> = []
    @ObservationIgnored public var onLanded: (@MainActor (Landed) -> Void)?

    private struct Saved: Codable {
        var items: [QueuedAction]
        var notes: [QueueNote]
    }

    public init(file: URL?, now: @escaping @MainActor () -> Date = { Date.now }) {
        self.file = file
        self.now = now
        if let saved = file.flatMap({ try? Data(contentsOf: $0) }).flatMap({ try? JSONDecoder.clubmate.decode(Saved.self, from: $0) }) {
            items = saved.items
            notes = saved.notes
        }
    }

    public func enqueue(_ item: QueuedAction) {
        guard !holdsAttempted(item) else { return }
        items.removeAll { $0.matches(item) }
        items.append(item)
        persist()
    }

    public func writes(computer: String, workspace: String) -> [QueuedAction] {
        items.filter { $0.computer == computer && $0.workspace == workspace && $0.kind == .command && $0.failure == nil }
    }

    public func submit(_ item: QueuedAction, runner: any CommandRunner, queues: Bool, keeps: Bool = true) async throws -> JSONValue? {
        guard keeps else { return try await runner.command(item.command, workspace: item.workspace, args: item.sendArgs, requestId: item.id) }
        enqueue(item)
        guard !queues, !held(item) else { return nil }
        let id = item.id
        sending.insert(id)
        defer { sending.remove(id) }
        markAttempted(id, deduped: runner.dedupes)
        do {
            let result = try await runner.command(item.command, workspace: item.workspace, args: item.sendArgs, requestId: id)
            land(id, result)
            return result
        } catch RemoteError.notSent {
            markAttempted(id, false)
            return nil
        } catch RemoteError.server(let code, let message) where runner.dedupes && !ServerReason.final(code) {
            backOff(id, message)
            return nil
        } catch let error where runner.dedupes && Self.outcomeLost(error) {
            return nil
        } catch {
            items.removeAll { $0.id == id }
            persist()
            throw error
        }
    }

    static func outcomeLost(_ error: any Error) -> Bool {
        switch error as? RemoteError {
        case .unreachable, .http: true
        default: false
        }
    }

    private func held(_ item: QueuedAction) -> Bool {
        guard let index = items.firstIndex(where: { $0.id == item.id }) else { return false }
        return items[..<index].contains { $0.computer == item.computer && $0.workspace == item.workspace && $0.target == item.target && $0.failure == nil }
    }

    public func holdsAttempted(_ item: QueuedAction) -> Bool {
        items.contains { $0.matches(item) && $0.attempted && $0.failure == nil }
    }

    public func cancel(_ item: QueuedAction) {
        guard let current = current(item.id), current.canUndo || current.canDismiss else { return }
        items.removeAll { $0.id == item.id }
        persist()
    }

    public func isDue(computer: String) -> Bool {
        items.contains { $0.computer == computer && due($0) }
    }

    public func nextWait(computer: String) -> TimeInterval? {
        items.filter { $0.computer == computer && $0.failure == nil }.compactMap(\.nextTry).min().map { max($0.timeIntervalSince(now()), 0) }
    }

    private func due(_ item: QueuedAction) -> Bool {
        item.failure == nil && (item.nextTry ?? .distantPast) <= now()
    }

    public func dismiss(_ note: QueueNote) {
        notes.removeAll { $0.id == note.id }
        persist()
    }

    public func item(_ session: String) -> QueuedAction? {
        items.first { $0.session == session }
    }

    public func notes(session: String) -> [QueueNote] {
        notes.filter { $0.session == session }
    }

    public func notes(computer: String) -> [QueueNote] {
        notes.filter { $0.computer == computer }
    }

    public func forget(computer: String) {
        items.removeAll { $0.computer == computer }
        notes.removeAll { $0.computer == computer }
        persist()
    }

    public func flush(computer: String, runner: any CommandRunner, supports: (String) -> Bool) async -> Flushed {
        guard !flushing.contains(computer) else { return Flushed() }
        flushing.insert(computer)
        defer { flushing.remove(computer) }
        var tried: Set<String> = []
        var out = Flushed()
        while let item = items.first(where: { $0.computer == computer && !tried.contains($0.id) && !sending.contains($0.id) && due($0) && !held($0) }) {
            tried.insert(item.id)
            guard supports(item.command) else {
                if supports(AgentCommand.sessions), !item.noted { noteTooOld(item) }
                continue
            }
            sending.insert(item.id)
            defer { sending.remove(item.id) }
            do {
                let sent = try await send(item.id, runner: runner)
                if sent.touched { out.touched.insert(item.workspace) }
                if let landed = sent.landed {
                    out.landed.append(landed)
                    onLanded?(landed)
                }
            } catch RemoteError.server(let code, let message) where ServerReason.drops(code) && item.kind == .command {
                refuse(item.id, code == ServerReason.unknown ? QueuedAction.checkOnMac : message)
                out.touched.insert(item.workspace)
            } catch RemoteError.server(let code, let message) where ServerReason.drops(code) {
                drop(item.id, note: Self.staleNote(item, code: code, message))
                out.touched.insert(item.workspace)
            } catch RemoteError.server(let code, let message) where ServerReason.final(code) {
                refuse(item.id, message)
                out.touched.insert(item.workspace)
            } catch RemoteError.server(_, let message) {
                backOff(item.id, message)
            } catch {
                return out
            }
        }
        return out
    }

    private func current(_ id: String) -> QueuedAction? {
        items.first { $0.id == id }
    }

    private func send(_ id: String, runner: any CommandRunner) async throws -> (touched: Bool, landed: Landed?) {
        guard let item = current(id) else { return (false, nil) }
        do {
            return try await deliver(item, runner: runner)
        } catch RemoteError.notSent(let message) {
            if !item.attempted { markAttempted(id, false) }
            throw RemoteError.notSent(message)
        }
    }

    private func deliver(_ item: QueuedAction, runner: any CommandRunner) async throws -> (touched: Bool, landed: Landed?) {
        let id = item.id
        switch item.kind {
        case .command:
            if item.mayHaveRun {
                refuse(id, QueuedAction.checkOnMac)
                return (true, nil)
            }
            markAttempted(id, deduped: runner.dedupes)
            let result = try await runner.command(item.command, workspace: item.workspace, args: item.sendArgs, requestId: id)
            return (true, land(id, result))
        case .connect:
            if !item.attempted {
                let sessions = try await runner.command(AgentCommand.sessions, workspace: item.workspace, args: .object(["session": .string(item.session)]), as: [AgentSession].self)
                guard current(id) != nil else { return (false, nil) }
                guard let request = item.request, sessions.first?.requests.contains(request) == true else {
                    drop(id, note: Self.answeredNote(item))
                    return (true, nil)
                }
            }
            markAttempted(id, deduped: runner.dedupes)
            _ = try await runner.command(TaskCommand.connectAnswer, workspace: item.workspace, args: item.connectArgs, requestId: id)
            drop(id, note: nil)
        case .archive:
            markAttempted(id, deduped: runner.dedupes)
            let result = try await runner.command(TaskCommand.archive, workspace: item.workspace, args: item.archiveArgs, requestId: id)
            let kept = result["kept"]?.arrayValue?.compactMap(\.stringValue) ?? []
            drop(id, note: kept.isEmpty ? nil : ([item.title + " ran; some of it stayed:"] + kept).joined(separator: "\n"))
        }
        return (true, nil)
    }

    @discardableResult
    private func land(_ id: String, _ result: JSONValue) -> Landed? {
        guard let item = current(id) else { return nil }
        items.removeAll { $0.id == id }
        persist()
        return Landed(item: item, result: result, at: now())
    }

    private func refuse(_ id: String, _ message: String) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        items[index].failure = message
        items[index].nextTry = nil
        persist()
    }

    private func backOff(_ id: String, _ message: String) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        items[index].tries += 1
        let tries = items[index].tries
        if tries >= Self.maxTries {
            items[index].failure = message
            items[index].nextTry = nil
        } else {
            items[index].nextTry = now().addingTimeInterval(min(Self.firstWait * pow(2, Double(tries - 1)), Self.longestWait))
        }
        persist()
    }

    private func markAttempted(_ id: String, _ attempted: Bool = true, deduped: Bool? = nil) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        items[index].attempted = attempted
        if let deduped { items[index].deduped = deduped }
        persist()
    }

    private func noteTooOld(_ item: QueuedAction) {
        guard let index = items.firstIndex(where: { $0.id == item.id }) else { return }
        items[index].noted = true
        notes.append(QueueNote(computer: item.computer, session: item.session, text: "\(item.title) waits: this Mac's Clubmate is too old for it. Update Clubmate, or Undo."))
        persist()
    }

    private func drop(_ id: String, note: String?) {
        guard let item = current(id) else { return }
        items.removeAll { $0.id == id }
        if let note { notes.append(QueueNote(computer: item.computer, session: item.session, text: note)) }
        persist()
    }

    static func answeredNote(_ item: QueuedAction) -> String {
        "\(item.title): already answered, so yours wasn't sent."
    }

    static func staleNote(_ item: QueuedAction, code: String, _ message: String) -> String {
        if item.mayHaveRun || code == ServerReason.unknown { return "\(item.title): the Mac never confirmed it, so it may have run. Check it on the Mac." }
        return code == ServerReason.answered ? answeredNote(item) : "\(item.title) wasn't sent: \(message)."
    }

    private func persist() {
        guard let file else { return }
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        try? FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? encoder.encode(Saved(items: items, notes: notes)).write(to: file, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }
}

extension JSONValue {
    func overlaid(_ changes: JSONValue?) -> JSONValue {
        guard case .object(var fields) = self, let changes = changes?.objectValue else { return self }
        fields.merge(changes) { $1 }
        return .object(fields)
    }
}

public enum ServerReason {
    public static let stale = "stale"
    public static let answered = "answered"
    public static let unknown = "unknown"
    public static let refused = "refused"
    public static let invalidParams = "invalid_params"
    public static let notAllowed = "not_allowed"

    static func drops(_ code: String) -> Bool {
        [stale, answered, unknown].contains(code)
    }

    static func final(_ code: String) -> Bool {
        drops(code) || [refused, invalidParams, notAllowed].contains(code)
    }
}

extension QueuedAction {
    func matches(_ other: QueuedAction) -> Bool {
        kind != .command && session == other.session && kind == other.kind && request == other.request
    }

    var command: String {
        switch kind {
        case .connect: TaskCommand.connectAnswer
        case .archive: TaskCommand.archive
        case .command: name ?? ""
        }
    }

    var sendArgs: JSONValue {
        guard let seen, case .object(var fields) = args ?? .object([:]) else { return args ?? .object([:]) }
        fields["seen"] = .string(seen)
        return .object(fields)
    }

    var connectArgs: JSONValue {
        var args: [String: JSONValue] = ["request": .string(request ?? ""), "key": .string(id), "answers": .array(answers.map(\.json))]
        if let seen { args["seen"] = .string(seen) }
        return .object(args)
    }

    var archiveArgs: JSONValue {
        var args: [String: JSONValue] = ["session": .string(session), "key": .string(id), "choices": .array(choices.map(\.json))]
        if let seen { args["seen"] = .string(seen) }
        return .object(args)
    }
}

extension String {
    var nilIfEmpty: String? {
        isEmpty ? nil : self
    }
}
