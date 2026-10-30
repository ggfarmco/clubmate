import Foundation
import Observation

public struct GoToFileMatch: Equatable, Sendable, Identifiable {
    public let path: String
    public let absPath: String
    public let positions: [Int]
    public let line: Int?

    public init(path: String, absPath: String, positions: [Int], line: Int? = nil) {
        self.path = path
        self.absPath = absPath
        self.positions = positions
        self.line = line
    }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let path = fields["path"]?.stringValue,
              let absPath = fields["absPath"]?.stringValue else { return nil }
        self.path = path
        self.absPath = absPath
        positions = fields["positions"]?.arrayValue?.compactMap { $0.numberValue.map(Int.init) } ?? []
        line = fields["line"]?.numberValue.map(Int.init)
    }

    public var id: String { absPath }

    public var nameStart: Int {
        let scalars = Array(path.unicodeScalars)
        return (scalars.lastIndex(of: "/").map { $0 + 1 }) ?? 0
    }

    public func runs(in range: Range<Int>) -> [HighlightRun] {
        let matched = Set(positions)
        return HighlightRun.split(path, in: range) { matched.contains($0) }
    }
}

public struct HighlightRun: Equatable, Sendable {
    public let text: String
    public let matched: Bool

    public init(text: String, matched: Bool) {
        self.text = text
        self.matched = matched
    }

    public static func split(_ text: String, in range: Range<Int>? = nil, matched: (Int) -> Bool) -> [HighlightRun] {
        let scalars = Array(text.unicodeScalars)
        let wanted = range ?? 0..<scalars.count
        let bounded = max(wanted.lowerBound, 0)..<max(min(wanted.upperBound, scalars.count), max(wanted.lowerBound, 0))
        var runs: [HighlightRun] = []
        var current = String.UnicodeScalarView()
        var hit = false
        for index in bounded {
            let next = matched(index)
            if next != hit, !current.isEmpty {
                runs.append(HighlightRun(text: String(current), matched: hit))
                current = String.UnicodeScalarView()
            }
            hit = next
            current.append(scalars[index])
        }
        if !current.isEmpty { runs.append(HighlightRun(text: String(current), matched: hit)) }
        return runs
    }
}

public enum GoToFileStatus: Equatable, Sendable {
    case ready
    case indexing
    case unavailable
    case failed(String)
}

@MainActor
@Observable
public final class GoToFileModel {
    public static let searchCommand = "search.files"
    public static let openCommand = "editor.open"
    public static let homeError = "search unavailable in the home workspace"
    public static let homeMessage = "Search isn't available in the home workspace"

    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public private(set) var query = ""
    public private(set) var results: [GoToFileMatch] = []
    public private(set) var selection = 0
    public private(set) var status = GoToFileStatus.ready
    public private(set) var truncated = false
    public private(set) var capped = false

    @ObservationIgnored private let run: Run
    @ObservationIgnored private let dismiss: @MainActor () -> Void
    @ObservationIgnored private let report: @MainActor (String) -> Void
    @ObservationIgnored private let opened: @MainActor () -> Void
    @ObservationIgnored private let pick: (@MainActor (String) -> Void)?
    @ObservationIgnored private let debounce: Duration
    @ObservationIgnored private let retry: Duration
    @ObservationIgnored private var pending: Task<Void, Never>?
    @ObservationIgnored private var serial = 0
    @ObservationIgnored public private(set) var choosing = false
    @ObservationIgnored private var epoch = 0

    public init(
        debounce: Duration = .milliseconds(40),
        retry: Duration = .milliseconds(500),
        run: @escaping Run,
        dismiss: @escaping @MainActor () -> Void,
        report: @escaping @MainActor (String) -> Void,
        opened: @escaping @MainActor () -> Void = {},
        pick: (@MainActor (String) -> Void)? = nil
    ) {
        self.debounce = debounce
        self.retry = retry
        self.run = run
        self.dismiss = dismiss
        self.report = report
        self.opened = opened
        self.pick = pick
    }

    public var picking: Bool { pick != nil }

    public func update(_ query: String) {
        self.query = query
        serial += 1
        let serial = serial
        pending?.cancel()
        pending = Task { [weak self, debounce] in
            if debounce > .zero {
                do { try await Task.sleep(for: debounce) } catch { return }
            }
            await self?.fetch(query, serial)
        }
    }

    public func move(_ delta: Int) {
        guard !results.isEmpty else { return }
        selection = (selection + delta + results.count) % results.count
    }

    public func select(_ index: Int) {
        if results.indices.contains(index) { selection = index }
    }

    public func choose() async {
        guard !choosing, status != .indexing else { return }
        choosing = true
        defer { choosing = false }
        let epoch = epoch
        while let task = pending {
            await task.value
            guard epoch == self.epoch, status != .indexing else { return }
            if pending == task { break }
        }
        guard results.indices.contains(selection) else { return }
        let match = results[selection]
        cancel()
        dismiss()
        if let pick { return pick(match.absPath) }
        var args: [String: JSONValue] = ["path": .string(match.absPath)]
        if let line = match.line { args["line"] = .number(Double(line)) }
        switch await run(CommandInvocation(id: Self.openCommand, args: .object(args))) {
        case .success: opened()
        case .failure(let error): report(error.message)
        }
    }

    public func cancel() {
        epoch += 1
        serial += 1
        pending?.cancel()
        pending = nil
    }

    private func fetch(_ query: String, _ serial: Int) async {
        guard serial == self.serial, !Task.isCancelled else { return }
        let reply = await run(CommandInvocation(id: Self.searchCommand, args: .object(["query": .string(query)])))
        guard serial == self.serial, !Task.isCancelled else { return }
        switch reply {
        case .success(let value):
            let fields = value.objectValue ?? [:]
            if fields["indexing"]?.boolValue == true {
                status = .indexing
                results = []
                pending = Task { [weak self, retry] in
                    do { try await Task.sleep(for: retry) } catch { return }
                    await self?.fetch(query, serial)
                }
                return
            }
            results = fields["matches"]?.arrayValue?.compactMap(GoToFileMatch.init) ?? []
            truncated = fields["truncated"]?.boolValue == true
            capped = fields["capped"]?.boolValue == true
            status = .ready
        case .failure(let error):
            results = []
            truncated = false
            capped = false
            status = Self.isHome(error) ? .unavailable : .failed(error.message)
        }
        selection = 0
    }

    private static func isHome(_ error: ClubmateError) -> Bool {
        if case .rpc(_, let message) = error { return message.hasSuffix(homeError) }
        return false
    }
}
