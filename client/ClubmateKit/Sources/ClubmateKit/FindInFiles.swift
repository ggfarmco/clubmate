import Foundation
import Observation

public struct FindOptions: Equatable, Sendable {
    public var caseSensitive: Bool
    public var word: Bool
    public var regex: Bool
    public var glob: String

    public init(caseSensitive: Bool = false, word: Bool = false, regex: Bool = false, glob: String = "") {
        self.caseSensitive = caseSensitive
        self.word = word
        self.regex = regex
        self.glob = glob
    }
}

public struct FindMatch: Equatable, Sendable, Identifiable {
    public let path: String
    public let absPath: String
    public let line: Int
    public let col: Int
    public let text: String
    public let ranges: [Range<Int>]

    public var id: String { "\(absPath):\(line)" }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let path = fields["path"]?.stringValue,
              let absPath = fields["absPath"]?.stringValue, let line = fields["line"]?.numberValue else { return nil }
        self.path = path
        self.absPath = absPath
        self.line = Int(line)
        col = fields["col"]?.numberValue.map(Int.init) ?? 1
        text = fields["text"]?.stringValue ?? ""
        ranges = (fields["ranges"]?.arrayValue ?? []).compactMap { pair in
            guard let bounds = pair.arrayValue?.compactMap(\.numberValue), bounds.count == 2, bounds[0] <= bounds[1] else { return nil }
            return Int(bounds[0])..<Int(bounds[1])
        }
    }

    public var runs: [HighlightRun] {
        HighlightRun.split(text) { index in ranges.contains { $0.contains(index) } }
    }
}

public struct FileGroup: Equatable, Sendable, Identifiable {
    public let path: String
    public let absPath: String
    public internal(set) var matches: [FindMatch]

    public var id: String { absPath }
}

public enum FindState: Equatable, Sendable {
    case idle
    case searching
    case done
    case truncated
    case unavailable
    case error(String)
}

public struct PreviewLine: Equatable, Sendable {
    public let number: Int
    public let text: String
}

public struct FindPreview: Equatable, Sendable {
    public let matchID: String
    public let line: Int
    public let lines: [PreviewLine]
}

@MainActor
@Observable
public final class FindInFilesModel {
    public static let maxQuickfixItems = 10_000
    public static let quickfixTitleLimit = 512

    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public private(set) var query = ""
    public private(set) var options = FindOptions()
    public private(set) var groups: [FileGroup] = []
    public private(set) var flat: [FindMatch] = []
    public private(set) var selection = 0
    public private(set) var state = FindState.idle
    public private(set) var preview: FindPreview?

    @ObservationIgnored private let run: Run
    @ObservationIgnored private let dismiss: @MainActor () -> Void
    @ObservationIgnored private let report: @MainActor (String) -> Void
    @ObservationIgnored private let opened: @MainActor () -> Void
    @ObservationIgnored private let debounce: Duration
    @ObservationIgnored private let previewDebounce: Duration
    @ObservationIgnored private var searching: Task<Void, Never>?
    @ObservationIgnored private var previewing: Task<Void, Never>?
    @ObservationIgnored private var serial = 0
    @ObservationIgnored private var currentID: String?
    @ObservationIgnored private var groupIndex: [String: Int] = [:]

    public init(
        debounce: Duration = .milliseconds(150),
        previewDebounce: Duration = .milliseconds(60),
        run: @escaping Run,
        dismiss: @escaping @MainActor () -> Void,
        report: @escaping @MainActor (String) -> Void,
        opened: @escaping @MainActor () -> Void
    ) {
        self.debounce = debounce
        self.previewDebounce = previewDebounce
        self.run = run
        self.dismiss = dismiss
        self.report = report
        self.opened = opened
    }

    public var selected: FindMatch? {
        flat.indices.contains(selection) ? flat[selection] : nil
    }

    public func update(query: String) {
        guard query != self.query else { return }
        self.query = query
        start()
    }

    public func update(options: FindOptions) {
        guard options != self.options else { return }
        self.options = options
        start()
    }

    public func start() {
        serial += 1
        let serial = serial
        searching?.cancel()
        searching = Task { [weak self, debounce] in
            if debounce > .zero {
                do { try await Task.sleep(for: debounce) } catch { return }
            }
            await self?.search(serial)
        }
    }

    public func move(_ delta: Int) {
        guard !flat.isEmpty else { return }
        select(min(max(selection + delta, 0), flat.count - 1))
    }

    public func select(_ index: Int) {
        guard flat.indices.contains(index) else { return }
        selection = index
        loadPreview()
    }

    public func open() async {
        guard let match = selected else { return }
        dismiss()
        let args: [String: JSONValue] = ["path": .string(match.absPath), "line": .number(Double(match.line)), "col": .number(Double(match.col))]
        switch await run(CommandInvocation(id: GoToFileModel.openCommand, args: .object(args))) {
        case .success: opened()
        case .failure(let error): report(error.message)
        }
    }

    public func quickfix() async {
        guard !flat.isEmpty else { return }
        let items: [JSONValue] = flat.prefix(Self.maxQuickfixItems).map { match in
            .object([
                "path": .string(match.absPath),
                "line": .number(Double(match.line)),
                "col": .number(Double(match.col)),
                "text": .string(match.text.replacingOccurrences(of: "\u{0}", with: "")),
            ])
        }
        let title = String("Search: \(query)".map { $0.isNewline ? " " : $0 }.prefix(Self.quickfixTitleLimit))
        dismiss()
        if case .failure(let error) = await run(CommandInvocation(id: "editor.quickfix", args: .object(["title": .string(title), "items": .array(items)]))) {
            report(error.message)
        }
    }

    public func stop() {
        serial += 1
        searching?.cancel()
        previewing?.cancel()
        if let id = currentID {
            currentID = nil
            Task { [run] in _ = await run(Self.cancelInvocation(id)) }
        }
    }

    private func search(_ serial: Int) async {
        if let old = currentID {
            currentID = nil
            _ = await run(Self.cancelInvocation(old))
        }
        guard serial == self.serial else { return }
        reset()
        guard !query.isEmpty else {
            state = .idle
            return
        }
        state = .searching
        var args: [String: JSONValue] = [
            "query": .string(query), "case": .bool(options.caseSensitive), "word": .bool(options.word), "regex": .bool(options.regex),
        ]
        if !options.glob.isEmpty { args["glob"] = .string(options.glob) }
        let started = await run(CommandInvocation(id: "search.start", args: .object(args)))
        guard serial == self.serial else {
            if case .success(let value) = started, let id = value.objectValue?["id"]?.stringValue {
                _ = await run(Self.cancelInvocation(id))
            }
            return
        }
        switch started {
        case .success(let value):
            guard let id = value.objectValue?["id"]?.stringValue else {
                state = .error("search did not return an id")
                return
            }
            currentID = id
            await poll(id, serial)
        case .failure(let error):
            state = Self.isHome(error) ? .unavailable : .error(error.message)
        }
    }

    private func poll(_ id: String, _ serial: Int) async {
        var after = 0
        while true {
            let reply = await run(CommandInvocation(id: "search.poll", args: .object(["id": .string(id), "after": .number(Double(after))])))
            guard serial == self.serial, currentID == id else { return }
            switch reply {
            case .failure(let error):
                state = .error(error.message)
                currentID = nil
                return
            case .success(let value):
                let fields = value.objectValue ?? [:]
                guard fields["id"]?.stringValue == currentID else { return }
                append(fields["matches"]?.arrayValue?.compactMap(FindMatch.init) ?? [])
                after = fields["next"]?.numberValue.map(Int.init) ?? after
                guard fields["done"]?.boolValue == true else { continue }
                currentID = nil
                if let error = fields["error"]?.stringValue, !error.isEmpty {
                    state = .error(error)
                } else {
                    state = fields["truncated"]?.boolValue == true ? .truncated : .done
                }
                return
            }
        }
    }

    private func reset() {
        groups = []
        flat = []
        groupIndex = [:]
        selection = 0
        preview = nil
        previewing?.cancel()
    }

    private func append(_ matches: [FindMatch]) {
        guard !matches.isEmpty else { return }
        let wasEmpty = flat.isEmpty
        for match in matches {
            if let index = groupIndex[match.absPath] {
                groups[index].matches.append(match)
            } else {
                groupIndex[match.absPath] = groups.count
                groups.append(FileGroup(path: match.path, absPath: match.absPath, matches: [match]))
            }
        }
        let current = selected?.id
        flat = groups.flatMap(\.matches)
        if let current, let index = flat.firstIndex(where: { $0.id == current }) { selection = index }
        if wasEmpty { select(0) }
    }

    private func loadPreview() {
        guard let match = selected else { return }
        previewing?.cancel()
        previewing = Task { [weak self, previewDebounce] in
            if previewDebounce > .zero {
                do { try await Task.sleep(for: previewDebounce) } catch { return }
            }
            await self?.fetchPreview(match)
        }
    }

    private func fetchPreview(_ match: FindMatch) async {
        let args: JSONValue = .object(["path": .string(match.absPath), "line": .number(Double(match.line)), "context": .number(8)])
        guard case .success(let value) = await run(CommandInvocation(id: "search.preview", args: args)),
              !Task.isCancelled, selected?.id == match.id else { return }
        let lines = (value.objectValue?["lines"]?.arrayValue ?? []).compactMap { line -> PreviewLine? in
            guard let fields = line.objectValue, let number = fields["n"]?.numberValue else { return nil }
            return PreviewLine(number: Int(number), text: fields["text"]?.stringValue ?? "")
        }
        preview = FindPreview(matchID: match.id, line: match.line, lines: lines)
    }

    private static func cancelInvocation(_ id: String) -> CommandInvocation {
        CommandInvocation(id: "search.cancel", args: .object(["id": .string(id)]))
    }

    private static func isHome(_ error: ClubmateError) -> Bool {
        if case .rpc(_, let message) = error { return message.hasSuffix(GoToFileModel.homeError) }
        return false
    }
}
